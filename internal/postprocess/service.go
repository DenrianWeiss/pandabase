package postprocess

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultPrompt is the built-in prompt for cleaning web content
const DefaultPrompt = `You are a web content cleaner. Your task is to extract clean, useful text content from raw web page text.

Instructions:
1. Remove navigation menus, headers, footers, sidebars, advertisements, and other UI elements
2. Remove cookie notices, popups, and promotional banners
3. Remove social media widgets and sharing buttons
4. Remove comments sections and user-generated content (unless specifically requested)
5. Remove duplicate content and boilerplate text
6. Preserve the main article/content text, headings, and important information
7. Preserve code blocks, tables, and structured data if they contain useful information
8. Output ONLY the cleaned text content, no explanations or metadata
9. If the content appears to be a list of articles/posts, extract each item with its title and brief description
10. Maintain the original structure using markdown formatting where appropriate

Input is raw text extracted from a web page. Output the cleaned, valuable content as plain text with markdown formatting.`

// Service handles post-processing of web content using LLM
type Service struct {
	apiURL       string
	apiKey       string
	model        string
	customPrompt string
	enabled      bool
	httpClient   *http.Client
}

// NewService creates a new post-process service
// If apiKey is empty, the service is disabled and will bypass processing
func NewService(apiURL, apiKey, model, customPrompt string, enabled bool) *Service {
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	prompt := customPrompt
	if prompt == "" {
		prompt = DefaultPrompt
	}

	// Disable if no API key provided
	isEnabled := enabled && apiKey != ""

	return &Service{
		apiURL:       apiURL,
		apiKey:       apiKey,
		model:        model,
		customPrompt: prompt,
		enabled:      isEnabled,
		httpClient: &http.Client{
			Timeout: 125 * time.Second, // Slightly longer than the 2 min context timeout
		},
	}
}

// Process cleans web content using LLM
// If service is disabled, returns original content unchanged
func (s *Service) Process(ctx context.Context, content string) (string, error) {
	// Bypass if disabled or no API key
	if !s.enabled {
		return content, nil
	}

	if content == "" {
		return "", nil
	}

	// Limit content size to prevent timeouts (max 8000 chars ~ 2000 tokens)
	const maxContentSize = 8000
	originalSize := len(content)
	if originalSize > maxContentSize {
		content = content[:maxContentSize]
	}

	// Create a timeout context for the API call (2 minutes)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	requestBody := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{
				"role":    "system",
				"content": s.customPrompt,
			},
			{
				"role":    "user",
				"content": content,
			},
		},
		"temperature": 0.1,
		"max_tokens":  4096, // Limit response size
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	chatURL := s.apiURL + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", chatURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.Error != nil {
		return "", fmt.Errorf("API error: %s (%s)", result.Error.Message, result.Error.Type)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}

	return result.Choices[0].Message.Content, nil
}

// IsEnabled returns whether the service is enabled and properly configured
func (s *Service) IsEnabled() bool {
	return s.enabled
}

// Validate checks if the configuration is valid
func (s *Service) Validate() error {
	if !s.enabled {
		return nil
	}
	if s.apiURL == "" {
		return fmt.Errorf("API URL is required")
	}
	if s.apiKey == "" {
		return fmt.Errorf("API key is required")
	}
	if s.model == "" {
		return fmt.Errorf("model name is required")
	}
	return nil
}

// TestConnection tests the connection to the API
func (s *Service) TestConnection(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	_, err := s.Process(ctx, "test")
	return err
}
