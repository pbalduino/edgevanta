package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"

	"github.com/pbalduino/ev_assignment/internal/domain"
)

var ErrAIUnavailable = errors.New("openai service unavailable")

type OpenAIClient struct {
	client         openai.Client
	embeddingModel string
	responseModel  string
	enabled        bool
}

func NewOpenAIClient(apiKey, embeddingModel, responseModel string) (*OpenAIClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return &OpenAIClient{
			embeddingModel: embeddingModel,
			responseModel:  responseModel,
			enabled:        false,
		}, nil
	}
	return &OpenAIClient{
		client:         openai.NewClient(option.WithAPIKey(apiKey)),
		embeddingModel: embeddingModel,
		responseModel:  responseModel,
		enabled:        true,
	}, nil
}

func (c *OpenAIClient) Embeddings(ctx context.Context, inputs []string) ([][]float64, error) {
	if !c.enabled {
		return nil, fmt.Errorf("%w: OPENAI_API_KEY not configured", ErrAIUnavailable)
	}
	if len(inputs) == 0 {
		return nil, nil
	}

	resp, err := c.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Model: openai.EmbeddingModel(c.embeddingModel),
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: inputs,
		},
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return nil, classifyAPIError(err)
	}

	vectors := make([][]float64, 0, len(resp.Data))
	for _, item := range resp.Data {
		vectors = append(vectors, item.Embedding)
	}
	return vectors, nil
}

func (c *OpenAIClient) EmbedOne(ctx context.Context, input string) ([]float64, error) {
	vectors, err := c.Embeddings(ctx, []string{input})
	if err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return nil, nil
	}
	return vectors[0], nil
}

func (c *OpenAIClient) AnswerQuestion(ctx context.Context, question string, chunks []domain.DocumentChunk, toolResult map[string]any) (string, error) {
	if !c.enabled {
		return "", fmt.Errorf("%w: OPENAI_API_KEY not configured", ErrAIUnavailable)
	}
	var contextBuilder strings.Builder
	for _, chunk := range chunks {
		contextBuilder.WriteString("[")
		contextBuilder.WriteString(string(chunk.Source))
		contextBuilder.WriteString(" pages ")
		contextBuilder.WriteString(fmt.Sprintf("%d-%d", chunk.PageStart, chunk.PageEnd))
		contextBuilder.WriteString("]\n")
		contextBuilder.WriteString(chunk.Text)
		contextBuilder.WriteString("\n\n")
	}

	toolJSON := "{}"
	if toolResult != nil {
		raw, _ := json.MarshalIndent(toolResult, "", "  ")
		toolJSON = string(raw)
	}

	prompt := "You are a construction estimating assistant. Answer only from the provided data. If the answer is incomplete, say what is missing.\n\nQuestion:\n" +
		question + "\n\nStructured tool output:\n" + toolJSON + "\n\nRetrieved context:\n" + contextBuilder.String()

	resp, err := c.client.Responses.New(ctx, responses.ResponseNewParams{
		Model: shared.ResponsesModel(c.responseModel),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String(prompt),
		},
		Text: responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfText: &shared.ResponseFormatTextParam{},
			},
		},
		Temperature: openai.Float(0.2),
	})
	if err != nil {
		return "", classifyAPIError(err)
	}

	return strings.TrimSpace(resp.OutputText()), nil
}

func classifyAPIError(err error) error {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		if apiErr.Code == "insufficient_quota" || apiErr.StatusCode == 429 {
			return fmt.Errorf("%w: %s", ErrAIUnavailable, apiErr.Message)
		}
	}
	return err
}
