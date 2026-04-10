package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"pandabase/internal/db/models"
	"pandabase/internal/embedder"
	"pandabase/internal/retriever"
)

// Test configuration
const (
	testDBHost     = "localhost"
	testDBPort     = "5433"
	testDBUser     = "pandabase"
	testDBPassword = "pandabase"
	testDBName     = "pandabase_test"
)

func boolPtr(v bool) *bool {
	return &v
}

func setupTestDB(t *testing.T) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		testDBHost, testDBUser, testDBPassword, testDBName, testDBPort,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("Failed to connect to test database: %v. Make sure PostgreSQL with pgvector is running on port %s", err, testDBPort)
	}

	err = db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error
	if err != nil {
		t.Skipf("Failed to create pgvector extension: %v. Make sure pgvector is installed.", err)
	}

	return db
}

func setupTestTables(t *testing.T, db *gorm.DB, dimensions int) {
	db.Exec("DROP TABLE IF EXISTS embeddings")
	db.Exec("DROP TABLE IF EXISTS chunks")
	db.Exec("DROP TABLE IF EXISTS documents")
	db.Exec("DROP TABLE IF EXISTS namespaces")

	err := db.AutoMigrate(&models.Namespace{}, &models.Document{}, &models.Chunk{})
	require.NoError(t, err)

	createEmbeddingsSQL := fmt.Sprintf(`
		CREATE TABLE embeddings (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			chunk_id UUID NOT NULL UNIQUE REFERENCES chunks(id) ON DELETE CASCADE,
			embedding VECTOR(%d) NOT NULL,
			model VARCHAR(100) NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		)
	`, dimensions)
	err = db.Exec(createEmbeddingsSQL).Error
	require.NoError(t, err)
}

func createRealTestData(t *testing.T, db *gorm.DB, emb *embedder.OpenAIEmbedder) (namespaceID, documentID uuid.UUID, chunkIDs []uuid.UUID) {
	ctx := context.Background()

	namespace := models.Namespace{
		ID:          uuid.New(),
		Name:        "api-test-namespace-" + uuid.NewString()[:8],
		Description: "Test namespace for API",
		AccessLevel: "private",
		OwnerID:     uuid.New(),
	}
	err := db.WithContext(ctx).Create(&namespace).Error
	require.NoError(t, err)

	document := models.Document{
		ID:          uuid.New(),
		NamespaceID: namespace.ID,
		SourceType:  "text",
		SourceURI:   "test://machine_learning.txt",
		ContentHash: uuid.NewString(), // random to avoid unique index conflict
		Status:      models.DocumentStatusCompleted,
		Metadata:    map[string]any{"source": "wikipedia", "year": "2023"},
	}
	err = db.WithContext(ctx).Create(&document).Error
	require.NoError(t, err)

	chunks := []models.Chunk{
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 0,
			Content:    "PostgreSQL is a powerful, open source object-relational database system.",
		},
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 1,
			Content:    "Pgvector is an open-source vector similarity search extension for PostgreSQL.",
		},
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 2,
			Content:    "RAG (Retrieval-Augmented Generation) combines LLM power with private data retrieval.",
		},
	}

	for _, chunk := range chunks {
		err := db.WithContext(ctx).Create(&chunk).Error
		require.NoError(t, err)
		chunkIDs = append(chunkIDs, chunk.ID)

		// Generate real embedding using OpenRouter
		time.Sleep(500 * time.Millisecond) // Respect rate limits
		embeddings, err := emb.Embed(ctx, []string{chunk.Content})
		require.NoError(t, err)

		vectorStrStr := "["
		for i, v := range embeddings[0] {
			if i > 0 {
				vectorStrStr += ","
			}
			vectorStrStr += fmt.Sprintf("%f", v)
		}
		vectorStrStr += "]"

		sql := `
			INSERT INTO embeddings (chunk_id, embedding, model, created_at)
			VALUES (?, ?::vector, ?, NOW())
		`
		err = db.Exec(sql, chunk.ID, vectorStrStr, emb.Model()).Error
		require.NoError(t, err)
	}

	return namespace.ID, document.ID, chunkIDs
}

func TestSearchAPI_Integration(t *testing.T) {
	if os.Getenv("SKIP_DOCKER_TESTS") == "1" {
		t.Skip("Skipping Docker-dependent tests")
	}

	// Set API Key from the prompt for testing OpenRouter
	apiKey := "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"
	model := "qwen/qwen3-embedding-8b"
	apiURL := "https://openrouter.ai/api/v1"
	dimension := 4096 // For qwen/qwen3-embedding-8b

	// Init embedder
	realEmbedder := embedder.NewOpenAIEmbedder(apiURL, apiKey, model, dimension, false)

	// Setup Database
	db := setupTestDB(t)
	setupTestTables(t, db, dimension)

	// Create Data
	namespaceID, docID, chunkIDs := createRealTestData(t, db, realEmbedder)

	// Setup Retriever
	ret := retriever.NewRetriever(db, realEmbedder, "simple", "vector")

	// Setup Router
	gin.SetMode(gin.TestMode)
	router := SetupRouter(ret)

	t.Run("POST /api/v1/search - Valid Hybrid Search", func(t *testing.T) {
		reqBody := retriever.SearchRequest{
			Query:          "Tell me about vector databases in Postgres",
			TopK:           3,
			Mode:           retriever.SearchModeHybrid,
			NamespaceIDs:   []string{namespaceID.String()},
			IncludeContent: boolPtr(true),
		}

		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequest(http.MethodPost, "/api/v1/search", bytes.NewBuffer(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp retriever.SearchResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		// Expecting at least one match related to pgvector/Postgres
		assert.Greater(t, len(resp.Results), 0)
		assert.Equal(t, "Tell me about vector databases in Postgres", resp.Query)

		// The pgvector chunk should be ranked highly
		foundPgVector := false
		for _, r := range resp.Results {
			if r.ChunkID == chunkIDs[1] { // chunk 1 is about pgvector
				foundPgVector = true
				break
			}
		}
		assert.True(t, foundPgVector, "Expected to find pgvector chunk in top results")
	})

	t.Run("GET /api/v1/chunks/:id - Get specific chunk", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/chunks/%s", chunkIDs[2].String())
		req, _ := http.NewRequest(http.MethodGet, url, nil)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var result retriever.SearchResult
		err := json.Unmarshal(w.Body.Bytes(), &result)
		require.NoError(t, err)

		assert.Equal(t, chunkIDs[2].String(), result.ChunkID.String())
		assert.Contains(t, result.Chunk.Content, "RAG (Retrieval-Augmented Generation)")
	})

	t.Run("GET /api/v1/documents/:id/chunks - Get document chunks", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/documents/%s/chunks", docID.String())
		req, _ := http.NewRequest(http.MethodGet, url, nil)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, float64(3), response["count"])
	})
}
