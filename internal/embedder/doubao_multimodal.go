package embedder

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// MultimodalInput represents a single multimodal input item
type MultimodalInput struct {
	Type     string `json:"type"`      // "text" or "image"
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"` // base64 data URI
}

// DoubaoMultimodalEmbedder implements multimodal embedding using Doubao API
type DoubaoMultimodalEmbedder struct {
	*OpenAIEmbedder
}

// NewDoubaoMultimodalEmbedder creates a new Doubao multimodal embedder
func NewDoubaoMultimodalEmbedder(apiURL, apiKey, model string, dimensions int) *DoubaoMultimodalEmbedder {
	// Doubao multimodal uses the standard embedder but with multimodal endpoint
	base := NewOpenAIEmbedder(apiURL, apiKey, model, dimensions, true)
	return &DoubaoMultimodalEmbedder{OpenAIEmbedder: base}
}

// Name returns the embedder name
func (e *DoubaoMultimodalEmbedder) Name() string {
	return "doubao-multimodal"
}

// EmbedTexts embeds text-only inputs using standard endpoint
func (e *DoubaoMultimodalEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	// Use standard OpenAI endpoint for text-only
	return e.OpenAIEmbedder.Embed(ctx, texts)
}

// EmbedMultimodal embeds multimodal inputs (text + images)
func (e *DoubaoMultimodalEmbedder) EmbedMultimodal(ctx context.Context, inputs []MultimodalInput) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	// Build request for Doubao multimodal endpoint
	requestBody := map[string]interface{}{
		"model": e.model,
		"input": inputs,
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Doubao multimodal endpoint
	multimodalURL := e.apiURL + "/embeddings/multimodal"

	req, err := http.NewRequestWithContext(ctx, "POST", multimodalURL, bytes.NewBuffer(jsonBody))
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
		// Try alternative response format (some APIs return single object instead of array)
		var altResult struct {
			Object    string    `json:"object"`
			Embedding []float32 `json:"embedding"`
			Model     string    `json:"model"`
		}
		if altErr := json.Unmarshal(body, &altResult); altErr == nil && len(altResult.Embedding) > 0 {
			return [][]float32{altResult.Embedding}, nil
		}
		// Debug: print actual response
		return nil, fmt.Errorf("failed to unmarshal response: %w, body: %s", err, string(body))
	}

	// Sort embeddings by index
	embeddings := make([][]float32, len(result.Data))
	for _, item := range result.Data {
		embeddings[item.Index] = item.Embedding
	}

	return embeddings, nil
}

// EmbedImage embeds a single image
func (e *DoubaoMultimodalEmbedder) EmbedImage(ctx context.Context, imageData []byte, mimeType string) ([]float32, error) {
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Image)

	inputs := []MultimodalInput{
		{Type: "image", ImageURL: dataURI},
	}

	embeddings, err := e.EmbedMultimodal(ctx, inputs)
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return embeddings[0], nil
}

// EmbedTextAndImage embeds text and image together
func (e *DoubaoMultimodalEmbedder) EmbedTextAndImage(ctx context.Context, text string, imageData []byte, mimeType string) ([]float32, error) {
	base64Image := base64.StdEncoding.EncodeToString(imageData)
	dataURI := fmt.Sprintf("data:%s;base64,%s", mimeType, base64Image)

	inputs := []MultimodalInput{
		{Type: "text", Text: text},
		{Type: "image", ImageURL: dataURI},
	}

	embeddings, err := e.EmbedMultimodal(ctx, inputs)
	if err != nil {
		return nil, err
	}

	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}

	return embeddings[0], nil
}

// Embed implements plugin.Embedder interface (text-only mode)
func (e *DoubaoMultimodalEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.EmbedTexts(ctx, texts)
}
