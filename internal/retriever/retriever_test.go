package retriever

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"pandabase/internal/db/models"
	"pandabase/pkg/plugin"
)

// Test configuration
const (
	testDBHost     = "localhost"
	testDBPort     = "5433"
	testDBUser     = "pandabase"
	testDBPassword = "pandabase"
	testDBName     = "pandabase_test"
)

// MockEmbedder is a mock embedder for testing
type MockEmbedder struct {
	dimensions int
}

func (m *MockEmbedder) Name() string {
	return "mock-embedder"
}

func (m *MockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		// Generate deterministic mock embedding based on text content
		vector := make([]float32, m.dimensions)
		for j := 0; j < m.dimensions; j++ {
			// Simple hash-based embedding for testing
			vector[j] = float32((len(texts[i]) + j) % 100) / 100.0
		}
		results[i] = vector
	}
	return results, nil
}

func (m *MockEmbedder) Dimensions() int {
	return m.dimensions
}

func (m *MockEmbedder) Model() string {
	return "mock-model"
}

// setupTestDB creates a test database connection
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

	// Enable pgvector extension
	err = db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error
	if err != nil {
		t.Skipf("Failed to create pgvector extension: %v. Make sure pgvector is installed.", err)
	}

	return db
}

// setupTestTables creates test tables with the specified vector dimension
func setupTestTables(t *testing.T, db *gorm.DB, dimensions int) {
	// Drop existing tables
	db.Exec("DROP TABLE IF EXISTS embeddings")
	db.Exec("DROP TABLE IF EXISTS chunks")
	db.Exec("DROP TABLE IF EXISTS documents")
	db.Exec("DROP TABLE IF EXISTS namespaces")

	// Create tables
	err := db.AutoMigrate(&models.Namespace{}, &models.Document{}, &models.Chunk{})
	require.NoError(t, err)

	// Create embeddings table with dynamic dimension
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

	// Create indexes
	db.Exec("CREATE INDEX idx_documents_namespace_id ON documents(namespace_id)")
	db.Exec("CREATE INDEX idx_chunks_document_id ON chunks(document_id)")
	db.Exec("CREATE INDEX idx_embeddings_chunk_id ON embeddings(chunk_id)")
	db.Exec("CREATE INDEX idx_embeddings_embedding ON embeddings USING hnsw (embedding vector_cosine_ops)")
	db.Exec("CREATE INDEX idx_chunks_content_fts ON chunks USING GIN (to_tsvector('english', content))")
}

// createTestData creates test data in the database
func createTestData(t *testing.T, db *gorm.DB, embedder plugin.Embedder) (namespaceID, documentID uuid.UUID, chunkIDs []uuid.UUID) {
	ctx := context.Background()

	// Create namespace
	namespace := models.Namespace{
		ID:          uuid.New(),
		Name:        "test-namespace",
		Description: "Test namespace",
		AccessLevel: "private",
		OwnerID:     uuid.New(),
	}
	err := db.WithContext(ctx).Create(&namespace).Error
	require.NoError(t, err)

	// Create document
	document := models.Document{
		ID:          uuid.New(),
		NamespaceID: namespace.ID,
		SourceType:  "text",
		SourceURI:   "test://document.txt",
		ContentHash: "abc123",
		Status:      models.DocumentStatusCompleted,
		Metadata:    map[string]any{"author": "test"},
	}
	err = db.WithContext(ctx).Create(&document).Error
	require.NoError(t, err)

	// Create chunks
	chunks := []models.Chunk{
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 0,
			Content:    "Machine learning is a subset of artificial intelligence that enables computers to learn from data.",
			Location: models.LocationInfo{
				Type:      "file",
				URI:       "test://document.txt",
				Section:   "Introduction",
				LineStart: 1,
				LineEnd:   5,
			},
			TokenCount: 15,
			Metadata:   map[string]any{"topic": "AI"},
		},
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 1,
			Content:    "Natural language processing allows computers to understand and generate human language using deep learning.",
			Location: models.LocationInfo{
				Type:      "file",
				URI:       "test://document.txt",
				Section:   "NLP",
				LineStart: 6,
				LineEnd:   10,
			},
			TokenCount: 14,
			Metadata:   map[string]any{"topic": "NLP"},
		},
		{
			ID:         uuid.New(),
			DocumentID: document.ID,
			ChunkIndex: 2,
			Content:    "Computer vision uses neural networks to analyze and understand images and videos.",
			Location: models.LocationInfo{
				Type:      "file",
				URI:       "test://document.txt",
				Section:   "CV",
				LineStart: 11,
				LineEnd:   15,
			},
			TokenCount: 12,
			Metadata:   map[string]any{"topic": "CV"},
		},
	}

	for _, chunk := range chunks {
		err := db.WithContext(ctx).Create(&chunk).Error
		require.NoError(t, err)
		chunkIDs = append(chunkIDs, chunk.ID)

		// Generate and insert embedding
		embeddings, err := embedder.Embed(ctx, []string{chunk.Content})
		require.NoError(t, err)

		vectorStr := vectorToPostgresArray(embeddings[0])
		sql := `
			INSERT INTO embeddings (chunk_id, embedding, model, created_at)
			VALUES (?, ?::vector, ?, NOW())
		`
		err = db.Exec(sql, chunk.ID, vectorStr, embedder.Model()).Error
		require.NoError(t, err)
	}

	return namespace.ID, document.ID, chunkIDs
}

// skipIfNoDocker skips the test if SKIP_DOCKER_TESTS is set
func skipIfNoDocker(t *testing.T) {
	if os.Getenv("SKIP_DOCKER_TESTS") == "1" {
		t.Skip("Skipping Docker-dependent tests")
	}
}

func TestSearchRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		request SearchRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request with defaults",
			request: SearchRequest{
				Query: "test query",
				TopK:  10,
			},
			wantErr: false,
		},
		{
			name: "empty query",
			request: SearchRequest{
				Query: "",
				TopK:  10,
			},
			wantErr: true,
			errMsg:  "query is required",
		},
		{
			name: "top_k too small",
			request: SearchRequest{
				Query: "test",
				TopK:  0,
			},
			wantErr: true,
			errMsg:  "top_k must be between 1 and 100",
		},
		{
			name: "top_k too large",
			request: SearchRequest{
				Query: "test",
				TopK:  101,
			},
			wantErr: true,
			errMsg:  "top_k must be between 1 and 100",
		},
		{
			name: "hybrid weight out of range",
			request: SearchRequest{
				Query:        "test",
				TopK:         10,
				HybridWeight: 1.5,
			},
			wantErr: true,
			errMsg:  "hybrid_weight must be between 0 and 1",
		},
		{
			name: "valid hybrid request",
			request: SearchRequest{
				Query:        "test",
				TopK:         5,
				Mode:         SearchModeHybrid,
				HybridWeight: 0.7,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewRetriever(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")
	assert.NotNil(t, retriever)
	assert.NotNil(t, retriever.db)
	assert.NotNil(t, retriever.embedder)
}

func TestRetriever_VectorSearch(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, _, _ := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	tests := []struct {
		name     string
		request  SearchRequest
		wantErr  bool
		minCount int
	}{
		{
			name: "basic vector search",
			request: SearchRequest{
				Query:        "artificial intelligence and machine learning",
				TopK:         5,
				Mode:         SearchModeVector,
				NamespaceIDs: []string{namespaceID.String()},
			},
			wantErr:  false,
			minCount: 1,
		},
		{
			name: "vector search with min score",
			request: SearchRequest{
				Query:        "neural networks and deep learning",
				TopK:         5,
				Mode:         SearchModeVector,
				MinScore:     0.5,
				NamespaceIDs: []string{namespaceID.String()},
			},
			wantErr: false,
		},
		{
			name: "vector search without content",
			request: SearchRequest{
				Query:          "computer vision",
				TopK:           5,
				Mode:           SearchModeVector,
				NamespaceIDs:   []string{namespaceID.String()},
				IncludeContent: false,
			},
			wantErr:  false,
			minCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := retriever.Search(context.Background(), tt.request)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.GreaterOrEqual(t, len(resp.Results), tt.minCount)
				assert.Equal(t, tt.request.Query, resp.Query)
				assert.Equal(t, SearchModeVector, resp.Mode)

				// Verify result structure
				for _, result := range resp.Results {
					assert.NotEqual(t, uuid.Nil, result.ChunkID)
					assert.NotEqual(t, uuid.Nil, result.DocumentID)
					assert.GreaterOrEqual(t, result.VectorScore, 0.0)
					assert.LessOrEqual(t, result.VectorScore, 1.0)

					if !tt.request.IncludeContent {
						assert.Empty(t, result.Chunk.Content)
					}
				}
			}
		})
	}
}

func TestRetriever_FullTextSearch(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, _, _ := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	tests := []struct {
		name     string
		request  SearchRequest
		wantErr  bool
		minCount int
	}{
		{
			name: "basic full-text search",
			request: SearchRequest{
				Query:        "machine learning artificial intelligence",
				TopK:         5,
				Mode:         SearchModeFullText,
				NamespaceIDs: []string{namespaceID.String()},
			},
			wantErr:  false,
			minCount: 1,
		},
		{
			name: "full-text search with filters",
			request: SearchRequest{
				Query: "natural language processing",
				TopK:  5,
				Mode:  SearchModeFullText,
				Filters: map[string]interface{}{
					"source_type": "text",
				},
				NamespaceIDs: []string{namespaceID.String()},
			},
			wantErr:  false,
			minCount: 0, // May or may not match depending on FTS behavior
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := retriever.Search(context.Background(), tt.request)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, resp)
				assert.GreaterOrEqual(t, len(resp.Results), tt.minCount)
				assert.Equal(t, SearchModeFullText, resp.Mode)

				// Verify result structure
				for _, result := range resp.Results {
					assert.NotEqual(t, uuid.Nil, result.ChunkID)
					assert.GreaterOrEqual(t, result.FullTextScore, 0.0)
				}
			}
		})
	}
}

func TestRetriever_HybridSearch(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, _, _ := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	request := SearchRequest{
		Query:        "deep learning neural networks",
		TopK:         5,
		Mode:         SearchModeHybrid,
		HybridWeight: 0.7,
		NamespaceIDs: []string{namespaceID.String()},
	}

	resp, err := retriever.Search(context.Background(), request)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, SearchModeHybrid, resp.Mode)

	// Verify hybrid results have both scores
	for _, result := range resp.Results {
		assert.NotEqual(t, uuid.Nil, result.ChunkID)
		// At least one of the scores should be non-zero
		assert.True(t, result.VectorScore > 0 || result.FullTextScore > 0,
			"Result should have at least one non-zero score")
		assert.GreaterOrEqual(t, result.FinalScore, 0.0)
	}
}

func TestRetriever_GetChunkByID(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, documentID, chunkIDs := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	tests := []struct {
		name      string
		chunkID   uuid.UUID
		wantErr   bool
		errMsg    string
		wantChunk bool
	}{
		{
			name:      "existing chunk",
			chunkID:   chunkIDs[0],
			wantErr:   false,
			wantChunk: true,
		},
		{
			name:    "non-existent chunk",
			chunkID: uuid.New(),
			wantErr: true,
			errMsg:  "chunk not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := retriever.GetChunkByID(context.Background(), tt.chunkID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.chunkID, result.ChunkID)
				assert.Equal(t, documentID, result.DocumentID)
				assert.Equal(t, namespaceID, result.NamespaceID)
				assert.NotEmpty(t, result.Chunk.Content)
			}
		})
	}
}

func TestRetriever_GetDocumentChunks(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, documentID, _ := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	results, err := retriever.GetDocumentChunks(context.Background(), documentID)
	assert.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify order by chunk_index
	for i, result := range results {
		assert.Equal(t, i, result.Chunk.ChunkIndex)
		assert.Equal(t, documentID, result.DocumentID)
		assert.Equal(t, namespaceID, result.NamespaceID)
	}
}

func TestCalculateCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float64
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: -1.0,
		},
		{
			name:     "different dimensions",
			a:        []float32{1, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
		{
			name:     "zero vector",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
		},
		{
			name:     "45 degree angle",
			a:        []float32{1, 0},
			b:        []float32{0.707107, 0.707107},
			expected: 0.707107,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateCosineSimilarity(tt.a, tt.b)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestVectorToPostgresArray(t *testing.T) {
	tests := []struct {
		name     string
		vector   []float32
		expected string
	}{
		{
			name:     "empty vector",
			vector:   []float32{},
			expected: "[]",
		},
		{
			name:     "single element",
			vector:   []float32{1.5},
			expected: "[1.500000]",
		},
		{
			name:     "multiple elements",
			vector:   []float32{1.0, 2.0, 3.0},
			expected: "[1.000000,2.000000,3.000000]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := vectorToPostgresArray(tt.vector)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeTsQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "simple query",
			query:    "machine learning",
			expected: "machine learning",
		},
		{
			name:     "query with special chars",
			query:    "AI & ML | 'test'",
			expected: "AI   ML    test",
		},
		{
			name:     "query with parentheses",
			query:    "(AI AND ML)",
			expected: "AI AND ML",
		},
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeTsQuery(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSortResultsByScore(t *testing.T) {
	results := []SearchResult{
		{FinalScore: 0.5},
		{FinalScore: 0.9},
		{FinalScore: 0.3},
		{FinalScore: 0.7},
	}

	sortResultsByScore(results)

	expected := []float64{0.9, 0.7, 0.5, 0.3}
	for i, result := range results {
		assert.Equal(t, expected[i], result.FinalScore)
	}
}

func TestRetriever_reciprocalRankFusion(t *testing.T) {
	retriever := &Retriever{}

	tests := []struct {
		name          string
		vectorScore   float64
		fullTextScore float64
		vectorWeight  float64
	}{
		{
			name:          "equal scores",
			vectorScore:   0.5,
			fullTextScore: 0.5,
			vectorWeight:  0.5,
		},
		{
			name:          "high vector score",
			vectorScore:   0.9,
			fullTextScore: 0.3,
			vectorWeight:  0.7,
		},
		{
			name:          "high text score",
			vectorScore:   0.3,
			fullTextScore: 0.9,
			vectorWeight:  0.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := retriever.reciprocalRankFusion(tt.vectorScore, tt.fullTextScore, tt.vectorWeight)
			assert.Greater(t, score, 0.0)
		})
	}
}

func TestRetriever_mergeAndRerank(t *testing.T) {
	retriever := &Retriever{}

	chunkID1 := uuid.New()
	chunkID2 := uuid.New()
	chunkID3 := uuid.New()

	vectorResults := map[uuid.UUID]SearchResult{
		chunkID1: {ChunkID: chunkID1, VectorScore: 0.9},
		chunkID2: {ChunkID: chunkID2, VectorScore: 0.7},
	}

	fullTextResults := map[uuid.UUID]SearchResult{
		chunkID2: {ChunkID: chunkID2, FullTextScore: 0.8},
		chunkID3: {ChunkID: chunkID3, FullTextScore: 0.6},
	}

	results := retriever.mergeAndRerank(vectorResults, fullTextResults, 0.7, 10)

	// Should have all 3 unique chunks
	assert.Len(t, results, 3)

	// chunkID2 should have both scores
	foundChunk2 := false
	for _, r := range results {
		if r.ChunkID == chunkID2 {
			foundChunk2 = true
			assert.Greater(t, r.VectorScore, 0.0)
			assert.Greater(t, r.FullTextScore, 0.0)
			break
		}
	}
	assert.True(t, foundChunk2)
}

func TestRetriever_applyFilters(t *testing.T) {
	skipIfNoDocker(t)
	db := setupTestDB(t)
	mockEmbedder := &MockEmbedder{dimensions: 128}
	setupTestTables(t, db, 128)
	namespaceID, _, _ := createTestData(t, db, mockEmbedder)

	retriever := NewRetriever(db, mockEmbedder, "simple", "vector")

	// Test with source_type filter
	request := SearchRequest{
		Query:        "machine learning",
		TopK:         10,
		Mode:         SearchModeFullText,
		NamespaceIDs: []string{namespaceID.String()},
		Filters: map[string]interface{}{
			"source_type": "text",
		},
	}

	resp, err := retriever.Search(context.Background(), request)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// Ensure MockEmbedder implements plugin.Embedder
var _ plugin.Embedder = (*MockEmbedder)(nil)
