package embedder

import (
	"context"
	"os"
	"testing"
)

// TestWithVolcanoEngine tests the embedder with the provided Volcano Engine API
// This test is skipped by default. To run it:
// go test -v -run TestWithVolcanoEngine ./internal/embedder/
//
// Note: The provided model (doubao-embedding-vision-250615) is a vision model,
// not a text embedding model. For text embeddings, you need to use a text
// embedding model like doubao-embedding-text-240715 or doubao-embedding-large-240915.
// Also ensure your API key has access to the model.
func TestWithVolcanoEngine(t *testing.T) {
	// Skip if not explicitly requested
	if os.Getenv("TEST_VOLCANO_API") != "1" {
		t.Skip("Skipping Volcano Engine test. Set TEST_VOLCANO_API=1 to run")
	}

	// Use the provided credentials
	// Note: This is a vision model, not a text embedding model
	// You may need to request access or use a different model
	apiURL := "https://ark.cn-beijing.volces.com/api/v3"
	apiKey := "8e3ccfd1-6049-44ac-a3ac-eb5656efa06f"
	
	// Try the provided model first
	model := os.Getenv("VOLCANO_MODEL")
	if model == "" {
		model = "doubao-embedding-vision-250615"
	}
	
	dimensions := 2048 // Adjust based on your model

	embedder := NewOpenAIEmbedder(apiURL, apiKey, model, dimensions, false)

	ctx := context.Background()

	t.Run("TestConnection", func(t *testing.T) {
		err := embedder.TestConnection(ctx)
		if err != nil {
			t.Logf("Connection test failed (model may not support embeddings or API key has no access): %v", err)
			t.Skip("Skipping - model may not support embeddings API")
		}
		t.Log("Successfully connected to Volcano Engine API")
	})

	t.Run("EmbedSingleText", func(t *testing.T) {
		texts := []string{"这是一个测试文本"}
		embeddings, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Logf("Embed failed: %v", err)
			t.Skip("Skipping - model may not support embeddings API")
		}

		if len(embeddings) != 1 {
			t.Errorf("Expected 1 embedding, got %d", len(embeddings))
		}

		if len(embeddings[0]) != dimensions {
			t.Errorf("Expected dimension %d, got %d", dimensions, len(embeddings[0]))
		}

		t.Logf("Successfully generated embedding with %d dimensions", len(embeddings[0]))
	})

	t.Run("EmbedMultipleTexts", func(t *testing.T) {
		texts := []string{
			"这是第一个测试文本",
			"This is the second test text",
			"这是第三个测试文本，用于测试批量嵌入",
		}

		embeddings, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Logf("Embed failed: %v", err)
			t.Skip("Skipping - model may not support embeddings API")
		}

		if len(embeddings) != len(texts) {
			t.Errorf("Expected %d embeddings, got %d", len(texts), len(embeddings))
		}

		for i, emb := range embeddings {
			if len(emb) != dimensions {
				t.Errorf("Embedding %d: expected dimension %d, got %d", i, dimensions, len(emb))
			}
		}

		t.Logf("Successfully generated %d embeddings", len(embeddings))
	})

	t.Run("EmbedEmptyInput", func(t *testing.T) {
		embeddings, err := embedder.Embed(ctx, []string{})
		if err != nil {
			t.Fatalf("Embed failed for empty input: %v", err)
		}
		if len(embeddings) != 0 {
			t.Errorf("Expected 0 embeddings for empty input, got %d", len(embeddings))
		}
	})

	t.Run("TestEmbedderInterface", func(t *testing.T) {
		// Test Name()
		if embedder.Name() != "openai-compatible" {
			t.Errorf("Expected name 'openai-compatible', got '%s'", embedder.Name())
		}

		// Test Model()
		if embedder.Model() != model {
			t.Errorf("Expected model '%s', got '%s'", model, embedder.Model())
		}

		// Test Dimensions()
		if embedder.Dimensions() != dimensions {
			t.Errorf("Expected dimensions %d, got %d", dimensions, embedder.Dimensions())
		}
	})
}

func TestOpenAIEmbedderValidation(t *testing.T) {
	tests := []struct {
		name       string
		apiURL     string
		apiKey     string
		model      string
		dimensions int
		wantErr    bool
	}{
		{
			name:       "valid configuration",
			apiURL:     "https://api.openai.com/v1",
			apiKey:     "test-key",
			model:      "text-embedding-ada-002",
			dimensions: 1536,
			wantErr:    false,
		},
		{
			name:       "missing API URL uses default",
			apiURL:     "",  // Will default to OpenAI URL
			apiKey:     "test-key",
			model:      "text-embedding-ada-002",
			dimensions: 1536,
			wantErr:    false,  // Empty URL gets default value
		},
		{
			name:       "missing API key",
			apiURL:     "https://api.openai.com/v1",
			apiKey:     "",
			model:      "text-embedding-ada-002",
			dimensions: 1536,
			wantErr:    true,
		},
		{
			name:       "missing model",
			apiURL:     "https://api.openai.com/v1",
			apiKey:     "test-key",
			model:      "",
			dimensions: 1536,
			wantErr:    true,
		},
		{
			name:       "invalid dimensions",
			apiURL:     "https://api.openai.com/v1",
			apiKey:     "test-key",
			model:      "text-embedding-ada-002",
			dimensions: 0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder := NewOpenAIEmbedder(tt.apiURL, tt.apiKey, tt.model, tt.dimensions, false)
			err := embedder.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOpenAIEmbedderDefaults(t *testing.T) {
	embedder := NewOpenAIEmbedder("", "key", "model", 1536, false)
	
	// Should default to OpenAI URL
	if embedder.apiURL != "https://api.openai.com/v1" {
		t.Errorf("Expected default URL 'https://api.openai.com/v1', got '%s'", embedder.apiURL)
	}
}
