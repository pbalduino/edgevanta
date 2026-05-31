package domain

import "time"

type DocumentType string

const (
	DocumentTypeCSV  DocumentType = "csv"
	DocumentTypePDF  DocumentType = "pdf"
	DocumentTypeText DocumentType = "text"
)

type KnowledgeSource string

const (
	SourceBidTabulation KnowledgeSource = "bid_tabulation"
	SourceSpecifications KnowledgeSource = "specifications"
	SourcePlans KnowledgeSource = "plans"
	SourceUnknown KnowledgeSource = "unknown"
)

type Document struct {
	ID         int64           `json:"id"`
	Name       string          `json:"name"`
	Path       string          `json:"path"`
	Type       DocumentType    `json:"type"`
	Source     KnowledgeSource `json:"source"`
	PageCount  int             `json:"page_count"`
	UploadedAt time.Time       `json:"uploaded_at"`
}

type DocumentChunk struct {
	ID         int64           `json:"id"`
	DocumentID int64           `json:"document_id"`
	ChunkIndex int             `json:"chunk_index"`
	PageStart  int             `json:"page_start"`
	PageEnd    int             `json:"page_end"`
	Section    string          `json:"section"`
	Source     KnowledgeSource `json:"source"`
	Text       string          `json:"text"`
	Metadata   string          `json:"metadata"`
	Score      float64         `json:"score,omitempty"`
}

type BidRow struct {
	ID            int64     `json:"id"`
	ProjectID     string    `json:"project_id"`
	LetDate       time.Time `json:"let_date"`
	County        string    `json:"county"`
	ItemNumber    string    `json:"item_number"`
	ItemDesc      string    `json:"item_desc"`
	Unit          string    `json:"unit"`
	Quantity      float64   `json:"quantity"`
	EngineerUnit  float64   `json:"engineer_unit_price"`
	Bidder        string    `json:"bidder"`
	BidRank       int       `json:"bid_rank"`
	UnitPrice     float64   `json:"unit_price"`
	ExtAmount     float64   `json:"ext_amount"`
	BidTotal      float64   `json:"bid_total"`
	DocumentID    int64     `json:"document_id"`
}

type PriceOutlier struct {
	ItemNumber      string  `json:"item_number"`
	ItemDesc        string  `json:"item_desc"`
	Bidder          string  `json:"bidder"`
	Unit            string  `json:"unit"`
	UnitPrice       float64 `json:"unit_price"`
	AveragePrice    float64 `json:"average_price"`
	RatioToAverage  float64 `json:"ratio_to_average"`
	ZScore          float64 `json:"z_score"`
	DeviationClass  string  `json:"deviation_class"`
}

type ToolResult struct {
	Tool    string         `json:"tool"`
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

type ToolRoute struct {
	Tools      []string        `json:"tools"`
	SourceHint KnowledgeSource `json:"source_hint"`
	NeedSearch bool            `json:"need_search"`
}

type AskResponse struct {
	Answer  string          `json:"answer"`
	Tool    string          `json:"tool"`
	Route   ToolRoute       `json:"route"`
	Chunks  []DocumentChunk `json:"chunks,omitempty"`
	Citations []string      `json:"citations,omitempty"`
	Result  map[string]any  `json:"result,omitempty"`
}
