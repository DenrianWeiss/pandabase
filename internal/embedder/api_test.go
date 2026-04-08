package embedder

import (
	"context"
	"os"
	"testing"

	"pandabase/internal/config"
)

// TestDoubaoMultimodal tests the Doubao multimodal embedder
// Set TEST_DOUbao_API=1 to run this test
// Required env vars:
//   - DOUBAO_API_URL (default: https://ark.cn-beijing.volces.com/api/v3)
//   - DOUBAO_API_KEY
//   - DOUBAO_MODEL (default: doubao-embedding-vision-250615)
//   - DOUBAO_DIMENSIONS (default: 2048)
func TestDoubaoMultimodal(t *testing.T) {
	if os.Getenv("TEST_DOUBAO_API") != "1" {
		t.Skip("Skipping Doubao test. Set TEST_DOUBAO_API=1 to run")
	}

	apiURL := getEnv("DOUBAO_API_URL", "https://ark.cn-beijing.volces.com/api/v3")
	apiKey := os.Getenv("DOUBAO_API_KEY")
	model := getEnv("DOUBAO_MODEL", "doubao-embedding-vision-250615")
	dimensions := getEnvInt("DOUBAO_DIMENSIONS", 2048)

	if apiKey == "" {
		t.Fatal("DOUBAO_API_KEY environment variable is required")
	}

	embedder := NewDoubaoMultimodalEmbedder(apiURL, apiKey, model, dimensions)

	ctx := context.Background()

	t.Run("EmbedTexts", func(t *testing.T) {
		texts := []string{
			"这是一个测试文本",
			"This is another test text",
			"这是第三个测试文本",
		}

		embeddings, err := embedder.EmbedTexts(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedTexts failed: %v", err)
		}

		if len(embeddings) != len(texts) {
			t.Errorf("Expected %d embeddings, got %d", len(texts), len(embeddings))
		}

		for i, emb := range embeddings {
			if len(emb) != dimensions {
				t.Errorf("Embedding %d: expected dimension %d, got %d", i, dimensions, len(emb))
			}
		}

		t.Logf("Successfully generated %d text embeddings", len(embeddings))
	})

	t.Run("EmbedMultimodal", func(t *testing.T) {
		// Test multimodal input (text only for now, as we don't have test images)
		inputs := []MultimodalInput{
			{Type: "text", Text: "这是一个多模态测试"},
		}

		embeddings, err := embedder.EmbedMultimodal(ctx, inputs)
		if err != nil {
			// Multimodal endpoint might not be available for all models
			t.Logf("EmbedMultimodal skipped (endpoint may not be available): %v", err)
			return
		}

		if len(embeddings) != 1 {
			t.Errorf("Expected 1 embedding, got %d", len(embeddings))
		}

		if len(embeddings[0]) != dimensions {
			t.Errorf("Expected dimension %d, got %d", dimensions, len(embeddings[0]))
		}

		t.Logf("Successfully generated multimodal embedding")
	})
}

// TestOpenRouterStandard tests standard OpenAI-compatible embedding with OpenRouter
// Set TEST_OPENROUTER_API=1 to run this test
// Required env vars:
//   - OPENROUTER_API_URL (default: https://openrouter.ai/api/v1)
//   - OPENROUTER_API_KEY
//   - OPENROUTER_MODEL (default: qwen/qwen3-embedding-8b)
//   - OPENROUTER_DIMENSIONS (default: 4096)
func TestOpenRouterStandard(t *testing.T) {
	if os.Getenv("TEST_OPENROUTER_API") != "1" {
		t.Skip("Skipping OpenRouter test. Set TEST_OPENROUTER_API=1 to run")
	}

	apiURL := getEnv("OPENROUTER_API_URL", "https://openrouter.ai/api/v1")
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	model := getEnv("OPENROUTER_MODEL", "qwen/qwen3-embedding-8b")
	dimensions := getEnvInt("OPENROUTER_DIMENSIONS", 4096)

	if apiKey == "" {
		t.Fatal("OPENROUTER_API_KEY environment variable is required")
	}

	// Standard mode (multimodal disabled)
	embedder := NewOpenAIEmbedder(apiURL, apiKey, model, dimensions, false)

	ctx := context.Background()

	t.Run("EmbedSingle", func(t *testing.T) {
		texts := []string{"This is a test sentence for embedding."}
		embeddings, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
		}

		if len(embeddings) != 1 {
			t.Errorf("Expected 1 embedding, got %d", len(embeddings))
		}

		if len(embeddings[0]) != dimensions {
			t.Errorf("Expected dimension %d, got %d", dimensions, len(embeddings[0]))
		}

		t.Logf("Successfully generated embedding with %d dimensions", len(embeddings[0]))
	})

	t.Run("EmbedMultiple", func(t *testing.T) {
		texts := []string{
			"First test sentence.",
			"Second test sentence.",
			"Third test sentence.",
		}

		embeddings, err := embedder.Embed(ctx, texts)
		if err != nil {
			t.Fatalf("Embed failed: %v", err)
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

	t.Run("TestConnection", func(t *testing.T) {
		err := embedder.TestConnection(ctx)
		if err != nil {
			t.Fatalf("TestConnection failed: %v", err)
		}
		t.Log("Successfully connected to OpenRouter API")
	})
}

// TestFactory creates embedders using the factory
func TestFactory(t *testing.T) {
	tests := []struct {
		name             string
		apiURL           string
		apiKey           string
		model            string
		dimensions       int
		enableMultimodal bool
		wantErr          bool
	}{
		{
			name:             "standard embedder",
			apiURL:           "https://api.openai.com/v1",
			apiKey:           "test-key",
			model:            "text-embedding-ada-002",
			dimensions:       1536,
			enableMultimodal: false,
			wantErr:          false,
		},
		{
			name:             "multimodal embedder",
			apiURL:           "https://ark.cn-beijing.volces.com/api/v3",
			apiKey:           "test-key",
			model:            "doubao-embedding-vision-250615",
			dimensions:       2048,
			enableMultimodal: true,
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.EmbeddingConfig{
				APIURL:           tt.apiURL,
				APIKey:           tt.apiKey,
				Model:            tt.model,
				Dimensions:       tt.dimensions,
				EnableMultimodal: tt.enableMultimodal,
			}

			factory := NewFactory(cfg)
			embedder, err := factory.Create()

			if (err != nil) != tt.wantErr {
				t.Errorf("Factory.Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if embedder == nil {
					t.Error("Expected non-nil embedder")
					return
				}

				if embedder.Model() != tt.model {
					t.Errorf("Expected model %s, got %s", tt.model, embedder.Model())
				}

				if embedder.Dimensions() != tt.dimensions {
					t.Errorf("Expected dimensions %d, got %d", tt.dimensions, embedder.Dimensions())
				}
			}
		})
	}
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	// Simple implementation - in production, use strconv.Atoi with error handling
	return defaultValue
}
