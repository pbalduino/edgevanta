package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pbalduino/ev_assignment/internal/config"
	"github.com/pbalduino/ev_assignment/internal/domain"
	"github.com/pbalduino/ev_assignment/internal/ingest"
	"github.com/pbalduino/ev_assignment/internal/provider"
	"github.com/pbalduino/ev_assignment/internal/storage"
)

type EstimatorService struct {
	cfg    config.Config
	store  *storage.SQLiteStore
	openai *provider.OpenAIClient
}

func NewEstimatorService(cfg config.Config, store *storage.SQLiteStore, openai *provider.OpenAIClient) *EstimatorService {
	return &EstimatorService{
		cfg:    cfg,
		store:  store,
		openai: openai,
	}
}

func (s *EstimatorService) Bootstrap(ctx context.Context) error {
	if err := os.MkdirAll(s.cfg.UploadDir, 0o755); err != nil {
		return err
	}

	docs, err := s.store.ListDocuments(ctx)
	if err != nil {
		return err
	}
	existing := make(map[string]domain.Document, len(docs))
	for _, doc := range docs {
		existing[doc.Name] = doc
	}

	defaults := []string{
		"docs/sample_bid_tabulation.csv",
		"docs/specifications-vol-1.pdf",
		"docs/specifications-vol-2.pdf",
		"docs/plans.pdf",
	}
	for _, path := range defaults {
		name := filepath.Base(path)
		if doc, ok := existing[name]; ok {
			if doc.Type == domain.DocumentTypePDF && doc.PageCount == 0 {
				log.Printf("bootstrap repair: re-ingesting %s because it has page_count=0", name)
				if err := s.store.DeleteDocument(ctx, doc.ID); err != nil {
					log.Printf("bootstrap warning: failed to delete stale %s: %v", name, err)
					continue
				}
			} else {
				continue
			}
		}
		if err := s.IngestFile(ctx, path); err != nil {
			log.Printf("bootstrap warning: failed to ingest %s: %v", path, err)
		}
	}
	return nil
}

func (s *EstimatorService) IngestFile(ctx context.Context, path string) error {
	name := filepath.Base(path)
	source := detectSource(name)
	ext := strings.ToLower(filepath.Ext(name))

	doc := domain.Document{
		Name:       name,
		Path:       path,
		Source:     source,
		UploadedAt: time.Now().UTC(),
	}

	switch ext {
	case ".csv":
		doc.Type = domain.DocumentTypeCSV
		docID, err := s.store.CreateDocument(ctx, doc)
		if err != nil {
			return err
		}
		rows, err := ingest.ParseBidCSV(ctx, path, docID)
		if err != nil {
			_ = s.store.DeleteDocument(ctx, docID)
			return err
		}
		if err := s.store.InsertBidRows(ctx, rows); err != nil {
			_ = s.store.DeleteDocument(ctx, docID)
			return err
		}
		return nil
	case ".pdf":
		doc.Type = domain.DocumentTypePDF
		log.Printf("ingest start: pdf %s", name)
		pages, totalPages, err := ingest.ExtractPDFPages(path)
		if err != nil {
			return err
		}
		doc.PageCount = totalPages
		docID, err := s.store.CreateDocument(ctx, doc)
		if err != nil {
			return err
		}
		chunks := ingest.ChunkPages(pages, s.cfg.ChunkSize, s.cfg.ChunkOverlap)
		log.Printf("ingest progress: pdf %s produced %d extracted pages and %d chunks", name, len(pages), len(chunks))
		inputs := make([]string, 0, len(chunks))
		docChunks := make([]domain.DocumentChunk, 0, len(chunks))
		for i, chunk := range chunks {
			meta := map[string]any{
				"source":     source,
				"page_start": chunk.PageStart,
				"page_end":   chunk.PageEnd,
			}
			rawMeta, _ := json.Marshal(meta)
			docChunks = append(docChunks, domain.DocumentChunk{
				DocumentID: docID,
				ChunkIndex: i,
				PageStart:  chunk.PageStart,
				PageEnd:    chunk.PageEnd,
				Section:    chunk.Section,
				Source:     source,
				Text:       chunk.Text,
				Metadata:   string(rawMeta),
			})
			inputs = append(inputs, chunk.Text)
		}
		vectors, err := s.openai.Embeddings(ctx, inputs)
		if err != nil {
			_ = s.store.DeleteDocument(ctx, docID)
			return err
		}
		if err := s.store.InsertChunks(ctx, docChunks, vectors); err != nil {
			_ = s.store.DeleteDocument(ctx, docID)
			return err
		}
		log.Printf("ingest done: pdf %s stored with %d chunks", name, len(docChunks))
		return nil
	default:
		return fmt.Errorf("unsupported file type: %s", ext)
	}
}

func (s *EstimatorService) ListDocuments(ctx context.Context) ([]domain.Document, error) {
	return s.store.ListDocuments(ctx)
}

func (s *EstimatorService) SearchDocuments(ctx context.Context, query string, source domain.KnowledgeSource) ([]domain.DocumentChunk, error) {
	vector, err := s.openai.EmbedOne(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("document search unavailable: %w", err)
	}
	return s.store.SearchChunks(ctx, vector, s.cfg.MaxRetrievedChunks, source)
}

func (s *EstimatorService) AnalyzeBidData(ctx context.Context) (domain.ToolResult, error) {
	summary, err := s.store.BidSummary(ctx)
	if err != nil {
		return domain.ToolResult{}, err
	}
	topItems, err := s.store.TopBidItems(ctx, 5)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return domain.ToolResult{
		Tool:    "analyze_bid_data",
		Summary: "Bid tabulation summary with top extended-amount items.",
		Data: map[string]any{
			"summary":   summary,
			"top_items": topItems,
		},
	}, nil
}

func (s *EstimatorService) FindPriceOutliers(ctx context.Context) (domain.ToolResult, error) {
	outliers, err := s.store.PriceOutliers(ctx, 2.0, 1.5)
	if err != nil {
		return domain.ToolResult{}, err
	}
	limit := 10
	if len(outliers) < limit {
		limit = len(outliers)
	}
	return domain.ToolResult{
		Tool:    "find_price_outliers",
		Summary: "Items whose unit prices materially deviate from their peer group.",
		Data: map[string]any{
			"outliers": outliers[:limit],
			"count":    len(outliers),
		},
	}, nil
}

func (s *EstimatorService) GetProjectSummary(ctx context.Context) (domain.ToolResult, error) {
	project, err := s.store.ProjectSummary(ctx)
	if err != nil {
		return domain.ToolResult{}, err
	}
	chunks, err := s.SearchDocuments(ctx, "project summary runway quantities drainage phasing", domain.SourceUnknown)
	if err != nil && !errors.Is(err, provider.ErrAIUnavailable) {
		return domain.ToolResult{}, err
	}
	return domain.ToolResult{
		Tool:    "get_project_summary",
		Summary: "High-level project summary combining bid data and retrieved document context.",
		Data: map[string]any{
			"bid_summary":      project,
			"supporting_pages": chunks,
		},
	}, nil
}

func (s *EstimatorService) Ask(ctx context.Context, question string) (domain.AskResponse, error) {
	route := classifyQuestion(question)
	toolOutputs := map[string]any{}

	for _, tool := range route.Tools {
		result, err := s.runTool(ctx, tool)
		if err != nil {
			return domain.AskResponse{}, err
		}
		toolOutputs[tool] = result.Data
	}

	chunks := []domain.DocumentChunk{}
	var err error
	if route.NeedSearch || len(route.Tools) == 0 {
		chunks, err = s.SearchDocuments(ctx, question, route.SourceHint)
		if err != nil && !errors.Is(err, provider.ErrAIUnavailable) {
			return domain.AskResponse{}, err
		}
	}
	if len(chunks) == 0 && route.SourceHint != domain.SourceUnknown {
		chunks, _ = s.SearchDocuments(ctx, question, domain.SourceUnknown)
	}

	primaryTool := "search_documents"
	if len(route.Tools) > 0 {
		primaryTool = route.Tools[0]
	}

	answer := ""
	if len(chunks) == 0 && len(toolOutputs) > 0 {
		answer = summarizeStructuredOutputs(toolOutputs)
	} else {
		answer, err = s.openai.AnswerQuestion(ctx, question, chunks, toolOutputs)
		if err != nil {
			if errors.Is(err, provider.ErrAIUnavailable) && len(toolOutputs) > 0 {
				answer = summarizeStructuredOutputs(toolOutputs) + "\n\nOpenAI indisponivel no momento para sintese semantica: " + err.Error()
			} else if errors.Is(err, provider.ErrAIUnavailable) {
				answer = "OpenAI indisponivel no momento para busca semantica e sintese: " + err.Error()
			} else {
				return domain.AskResponse{}, err
			}
		}
	}
	return domain.AskResponse{
		Answer:    answer,
		Tool:      primaryTool,
		Route:     route,
		Chunks:    compactChunks(chunks),
		Citations: buildCitations(chunks),
		Result:    toolOutputs,
	}, nil
}

func (s *EstimatorService) runTool(ctx context.Context, name string) (domain.ToolResult, error) {
	switch name {
	case "analyze_bid_data":
		return s.AnalyzeBidData(ctx)
	case "find_price_outliers":
		return s.FindPriceOutliers(ctx)
	case "get_project_summary":
		return s.GetProjectSummary(ctx)
	default:
		return domain.ToolResult{}, fmt.Errorf("unsupported tool: %s", name)
	}
}

func summarizeStructuredOutputs(toolOutputs map[string]any) string {
	if data, ok := toolOutputs["analyze_bid_data"].(map[string]any); ok {
		return summarizeBidAnalysis(data)
	}
	if data, ok := toolOutputs["find_price_outliers"].(map[string]any); ok {
		return summarizeOutliers(data)
	}
	if data, ok := toolOutputs["get_project_summary"].(map[string]any); ok {
		raw, _ := json.MarshalIndent(data, "", "  ")
		return "Project summary is available locally:\n" + string(raw)
	}
	raw, _ := json.MarshalIndent(toolOutputs, "", "  ")
	return "Structured result is available locally:\n" + string(raw)
}

func summarizeBidAnalysis(data map[string]any) string {
	var lines []string
	lines = append(lines, "The top 5 bid items by total extended amount are:")

	if items, ok := data["top_items"].([]map[string]any); ok {
		for i, item := range items {
			lines = append(lines, fmt.Sprintf(
				"%d. %s (%s): %.2f %s total",
				i+1,
				stringValue(item["item_desc"]),
				stringValue(item["item_number"]),
				floatValue(item["total_ext_amount"]),
				stringValue(item["unit"]),
			))
		}
	}

	if summary, ok := data["summary"].(map[string]any); ok {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf(
			"The dataset includes %.0f bid rows, %.0f unique items, and %.0f bidders. The maximum bid total is %.2f.",
			floatValue(summary["row_count"]),
			floatValue(summary["item_count"]),
			floatValue(summary["bidder_count"]),
			floatValue(summary["max_bid_total"]),
		))
	}

	return strings.Join(lines, "\n")
}

func summarizeOutliers(data map[string]any) string {
	count := int(floatValue(data["count"]))
	if count == 0 {
		return "I did not find strong unit-price outliers under the current detection thresholds."
	}

	lines := []string{
		fmt.Sprintf("I found %d strong unit-price outliers.", count),
		"",
		"Largest deviations:",
	}

	if outliers, ok := data["outliers"].([]domain.PriceOutlier); ok {
		for i, outlier := range outliers {
			if i >= 5 {
				break
			}
			lines = append(lines, fmt.Sprintf(
				"%d. %s (%s), bidder %s: %.2f vs %.2f average (%.2fx, z=%.2f)",
				i+1,
				outlier.ItemDesc,
				outlier.ItemNumber,
				outlier.Bidder,
				outlier.UnitPrice,
				outlier.AveragePrice,
				outlier.RatioToAverage,
				outlier.ZScore,
			))
		}
	}

	return strings.Join(lines, "\n")
}

func stringValue(value any) string {
	if v, ok := value.(string); ok {
		return v
	}
	return ""
}

func floatValue(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	default:
		return 0
	}
}

func buildCitations(chunks []domain.DocumentChunk) []string {
	if len(chunks) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(chunks))
	citations := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		citation := fmt.Sprintf("%s pp.%d-%d", chunk.Source, chunk.PageStart, chunk.PageEnd)
		if _, ok := seen[citation]; ok {
			continue
		}
		seen[citation] = struct{}{}
		citations = append(citations, citation)
	}
	return citations
}

func compactChunks(chunks []domain.DocumentChunk) []domain.DocumentChunk {
	if len(chunks) == 0 {
		return nil
	}
	limit := 4
	if len(chunks) < limit {
		limit = len(chunks)
	}
	compact := make([]domain.DocumentChunk, 0, limit)
	for _, chunk := range chunks[:limit] {
		chunk.Text = truncateText(chunk.Text, 1400)
		compact = append(compact, chunk)
	}
	return compact
}

func truncateText(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "\n...[truncated]"
}

func classifyQuestion(question string) domain.ToolRoute {
	lower := strings.ToLower(question)
	route := domain.ToolRoute{
		Tools:      []string{},
		SourceHint: detectSearchSource(lower),
	}

	if anyContains(lower, "outlier", "deviate", "deviation", "significantly above", "significantly below", "average unit price", "10x") {
		route.Tools = append(route.Tools, "find_price_outliers")
	}
	if anyContains(lower, "top ", "most expensive", "highest cost", "key quantities", "quantity", "quantities", "bid data", "bid items", "extended amount", "ext amount", "bidder statistics") {
		route.Tools = appendIfMissing(route.Tools, "analyze_bid_data")
	}
	if anyContains(lower, "project summary", "summarize the project", "summarize project", "runway", "phasing", "airport layout", "project metadata") {
		route.Tools = appendIfMissing(route.Tools, "get_project_summary")
		route.NeedSearch = true
	}
	if anyContains(lower, "plan set", "plans", "specification", "specifications", "spec", "drainage", "underdrain", "pavement", "marking", "geotechnical", "soil", "groundwater", "section") {
		route.NeedSearch = true
	}
	if len(route.Tools) == 0 {
		route.NeedSearch = true
	}
	return route
}

func detectSearchSource(lower string) domain.KnowledgeSource {
	switch {
	case anyContains(lower, "plan set", "plans", "drawing", "drainage layout", "runway", "phasing"):
		return domain.SourcePlans
	case anyContains(lower, "specification", "specifications", "spec ", "section", "underdrain", "geotechnical", "groundwater", "soil", "remediation"):
		return domain.SourceSpecifications
	default:
		return domain.SourceUnknown
	}
}

func anyContains(text string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func detectSource(name string) domain.KnowledgeSource {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "bid"):
		return domain.SourceBidTabulation
	case strings.Contains(lower, "spec"):
		return domain.SourceSpecifications
	case strings.Contains(lower, "plan"):
		return domain.SourcePlans
	default:
		return domain.SourceUnknown
	}
}
