package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"pandabase/pkg/plugin"
)

// OpenAIEmbedder implements Embedder interface using OpenAI-compatible API
type OpenAIEmbedder struct {
	apiURL           string
	apiKey           string
	model            string
	dimensions       int
	enableMultimodal bool
	httpClient       *http.Client
}

// NewOpenAIEmbedder creates a new OpenAI-compatible embedder
func NewOpenAIEmbedder(apiURL, apiKey, model string, dimensions int, enableMultimodal bool) *OpenAIEmbedder {
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1"
	}
	return &OpenAIEmbedder{
		apiURL:           apiURL,
		apiKey:           apiKey,
		model:            model,
		dimensions:       dimensions,
		enableMultimodal: enableMultimodal,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// Name returns the embedder name
func (e *OpenAIEmbedder) Name() string {
	if e.enableMultimodal {
		return "openai-compatible-multimodal"
	}
	return "openai-compatible"
}

// Embed generates embeddings for the given texts using standard OpenAI interface
func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// OpenAI API supports batching, but we'll process in chunks to avoid rate limits
	const batchSize = 100
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("failed to embed batch %d-%d: %w", i, end, err)
		}

		allEmbeddings = append(allEmbeddings, embeddings...)
	}

	return allEmbeddings, nil
}

// embedBatch sends a single batch to the standard OpenAI API
func (e *OpenAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	requestBody := map[string]interface{}{
		"model": e.model,
		"input": texts,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Standard OpenAI embeddings endpoint
	embeddingsURL := e.apiURL + "/embeddings"

	req, err := http.NewRequestWithContext(ctx, "POST", embeddingsURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			Object    string    `json:"object"`
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Sort embeddings by index to maintain order
	embeddings := make([][]float32, len(result.Data))
	for _, item := range result.Data {
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}

// Dimensions returns the dimension of the embeddings
func (e *OpenAIEmbedder) Dimensions() int {
	return e.dimensions
}

// Model returns the model name used for embeddings
func (e *OpenAIEmbedder) Model() string {
	return e.model
}

// IsMultimodalEnabled returns whether multimodal mode is enabled
func (e *OpenAIEmbedder) IsMultimodalEnabled() bool {
	return e.enableMultimodal
}

// Validate checks if the embedder configuration is valid
func (e *OpenAIEmbedder) Validate() error {
	if e.apiURL == "" {
		return fmt.Errorf("API URL is required")
	}
	if e.apiKey == "" {
		return fmt.Errorf("API key is required")
	}
	if e.model == "" {
		return fmt.Errorf("model name is required")
	}
	if e.dimensions <= 0 {
		return fmt.Errorf("dimensions must be positive")
	}
	return nil
}

// TestConnection tests the connection to the embedding API
func (e *OpenAIEmbedder) TestConnection(ctx context.Context) error {
	// Try to embed a simple text
	_, err := e.Embed(ctx, []string{"test"})
	return err
}

// Ensure OpenAIEmbedder implements plugin.Embedder interface
var _ plugin.Embedder = (*OpenAIEmbedder)(nil)
