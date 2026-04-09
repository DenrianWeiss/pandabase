package retriever

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"pandabase/internal/db/models"
	"pandabase/pkg/plugin"
)

// SearchMode defines the type of search to perform
type SearchMode string

const (
	// SearchModeVector performs vector similarity search only
	SearchModeVector SearchMode = "vector"
	// SearchModeFullText performs full-text search only
	SearchModeFullText SearchMode = "fulltext"
	// SearchModeHybrid performs both vector and full-text search with reranking
	SearchModeHybrid SearchMode = "hybrid"
)

// SearchRequest represents a search request
type SearchRequest struct {
	Query          string                 `json:"query" binding:"required"`
	NamespaceIDs   []string               `json:"namespace_ids,omitempty"`
	TopK           int                    `json:"top_k" binding:"required,min=1,max=100"`
	MinScore       float64                `json:"min_score,omitempty"`
	Mode           SearchMode             `json:"mode,omitempty"`
	HybridWeight   float64                `json:"hybrid_weight,omitempty"` // 0.0-1.0, weight for vector search (1-weight for fulltext)
	Filters        map[string]interface{} `json:"filters,omitempty"`
	IncludeContent bool                   `json:"include_content,omitempty"` // Whether to include full chunk content in response
}

// Validate validates the search request
func (r *SearchRequest) Validate() error {
	if r.Query == "" {
		return fmt.Errorf("query is required")
	}
	if r.TopK < 1 || r.TopK > 100 {
		return fmt.Errorf("top_k must be between 1 and 100")
	}
	if r.Mode == "" {
		r.Mode = SearchModeHybrid
	}
	if r.HybridWeight == 0 {
		r.HybridWeight = 0.7 // Default: 70% vector, 30% full-text
	}
	if r.HybridWeight < 0 || r.HybridWeight > 1 {
		return fmt.Errorf("hybrid_weight must be between 0 and 1")
	}
	return nil
}

// DocumentInfo represents document information in search results
type DocumentInfo struct {
	ID         uuid.UUID      `json:"id"`
	SourceType string         `json:"source_type"`
	SourceURI  string         `json:"source_uri"`
	Metadata   map[string]any `gorm:"serializer:json" json:"metadata,omitempty"`
}

// ChunkInfo represents chunk information in search results
type ChunkInfo struct {
	ID         uuid.UUID           `json:"id"`
	ChunkIndex int                 `json:"chunk_index"`
	Content    string              `json:"content,omitempty"`
	Location   models.LocationInfo `gorm:"serializer:json" json:"location"`
	TokenCount int                 `json:"token_count"`
	Metadata   map[string]any      `gorm:"serializer:json" json:"metadata,omitempty"`
}

// SearchResult represents a single search result
type SearchResult struct {
	// IDs for reference
	ChunkID     uuid.UUID `json:"chunk_id"`
	DocumentID  uuid.UUID `json:"document_id"`
	NamespaceID uuid.UUID `json:"namespace_id"`

	// Detailed information
	Chunk    ChunkInfo    `gorm:"embedded;embeddedPrefix:chunk_" json:"chunk"`
	Document DocumentInfo `gorm:"embedded;embeddedPrefix:document_" json:"document"`

	// Scores
	VectorScore   float64 `json:"vector_score,omitempty"`   // Cosine similarity (0-1)
	FullTextScore float64 `json:"fulltext_score,omitempty"` // Normalized full-text score (0-1)
	FinalScore    float64 `json:"final_score"`              // Combined/Reranked score (0-1)

	// Distance metrics (for debugging/advanced use)
	VectorDistance float64 `json:"vector_distance,omitempty"` // Raw pgvector distance

	// Rank information
	Rank int `json:"rank"`
}

// SearchResponse represents the response from a search
type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	TotalCount int            `json:"total_count"`
	Query      string         `json:"query"`
	Mode       SearchMode     `json:"mode"`
	TookMs     int64          `json:"took_ms"`
}

// Retriever performs document retrieval using various search strategies
type Retriever struct {
	db         *gorm.DB
	embedder   plugin.Embedder
	ftsDict    string
	vectorType string
}

// NewRetriever creates a new retriever instance
func NewRetriever(db *gorm.DB, embedder plugin.Embedder, ftsDict string, vectorType string) *Retriever {
	if ftsDict == "" {
		ftsDict = "simple"
	}
	if vectorType == "" {
		vectorType = "vector"
	}
	return &Retriever{
		db:         db,
		embedder:   embedder,
		ftsDict:    ftsDict,
		vectorType: vectorType,
	}
}

// Search performs a search based on the request parameters
func (r *Retriever) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid search request: %w", err)
	}

	switch req.Mode {
	case SearchModeVector:
		return r.vectorSearch(ctx, req)
	case SearchModeFullText:
		return r.fullTextSearch(ctx, req)
	case SearchModeHybrid:
		return r.hybridSearch(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported search mode: %s", req.Mode)
	}
}

// vectorSearch performs vector similarity search using pgvector
func (r *Retriever) vectorSearch(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	// Generate embedding for the query
	embeddings, err := r.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding generated")
	}

	queryVector := embeddings[0]

	// Build the SQL query
	query := r.buildVectorSearchQuery(req, queryVector)

	// Execute search
	var results []SearchResult
	if err := query.Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Post-process results
	r.postProcessResults(&results, req.MinScore, req.IncludeContent)

	return &SearchResponse{
		Results:    results,
		TotalCount: len(results),
		Query:      req.Query,
		Mode:       SearchModeVector,
	}, nil
}

// buildVectorSearchQuery builds the SQL query for vector search
func (r *Retriever) buildVectorSearchQuery(req SearchRequest, queryVector []float32) *gorm.DB {
	// Convert vector to PostgreSQL array format
	vectorStr := vectorToPostgresArray(queryVector)

	// Base query with all necessary joins
	query := r.db.Table("embeddings AS e").
		Select(fmt.Sprintf(`
			e.chunk_id as chunk_id,
			c.document_id as document_id,
			d.namespace_id as namespace_id,
			c.chunk_index as chunk_index,
			c.content as content,
			c.location::text as location,
			c.token_count as token_count,
			c.metadata::text as chunk_metadata,
			d.source_type as source_type,
			d.source_uri as source_uri,
			d.metadata::text as document_metadata,
			1 - (e.embedding <=> ?::%[1]s) as vector_score,
			e.embedding <=> ?::%[1]s as vector_distance
		`, r.vectorType), vectorStr, vectorStr).
		Joins("JOIN chunks c ON c.id = e.chunk_id").
		Joins("JOIN documents d ON d.id = c.document_id").
		Where("d.status = ?", models.DocumentStatusCompleted).
		Order(fmt.Sprintf("e.embedding <=> '%s'::%s", vectorStr, r.vectorType)).
		Limit(req.TopK * 2) // Fetch more for filtering

	// Apply namespace filter
	if len(req.NamespaceIDs) > 0 {
		query = query.Where("d.namespace_id IN ?", req.NamespaceIDs)
	}

	// Apply custom filters
	query = r.applyFilters(query, req.Filters)

	return query
}

// fullTextSearch performs full-text search using PostgreSQL tsvector
func (r *Retriever) fullTextSearch(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	// Build the SQL query
	query := r.buildFullTextSearchQuery(req)

	// Execute search
	var results []SearchResult
	if err := query.Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("full-text search failed: %w", err)
	}

	// Post-process results
	r.postProcessResults(&results, req.MinScore, req.IncludeContent)

	return &SearchResponse{
		Results:    results,
		TotalCount: len(results),
		Query:      req.Query,
		Mode:       SearchModeFullText,
	}, nil
}

// buildFullTextSearchQuery builds the SQL query for full-text search
func (r *Retriever) buildFullTextSearchQuery(req SearchRequest) *gorm.DB {
	selectSQL := fmt.Sprintf(`
			c.id as chunk_id,
			c.document_id as document_id,
			d.namespace_id as namespace_id,
			c.chunk_index as chunk_index,
			c.content as content,
			c.location::text as location,
			c.token_count as token_count,
			c.metadata::text as chunk_metadata,
			d.source_type as source_type,
			d.source_uri as source_uri,
			d.metadata::text as document_metadata,
			ts_rank(to_tsvector('%[1]s', c.content), plainto_tsquery('%[1]s', ?)) as fulltext_score
		`, r.ftsDict)

	whereSQL := fmt.Sprintf("to_tsvector('%[1]s', c.content) @@ plainto_tsquery('%[1]s', ?)", r.ftsDict)

	query := r.db.Table("chunks AS c").
		Select(selectSQL, req.Query).
		Joins("JOIN documents d ON d.id = c.document_id").
		Where("d.status = ?", models.DocumentStatusCompleted).
		Where(whereSQL, req.Query).
		Order("fulltext_score DESC").
		Limit(req.TopK * 2)

	// Apply namespace filter
	if len(req.NamespaceIDs) > 0 {
		query = query.Where("d.namespace_id IN ?", req.NamespaceIDs)
	}

	// Apply custom filters
	query = r.applyFilters(query, req.Filters)

	return query
}

// hybridSearch performs hybrid search combining vector and full-text search
func (r *Retriever) hybridSearch(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	// Generate embedding for the query
	embeddings, err := r.embedder.Embed(ctx, []string{req.Query})
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding generated")
	}

	queryVector := embeddings[0]

	// Perform both searches in parallel (vector search first as it's usually more expensive)
	vectorResults, err := r.fetchVectorResults(req, queryVector)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	fullTextResults, err := r.fetchFullTextResults(req)
	if err != nil {
		return nil, fmt.Errorf("full-text search failed: %w", err)
	}

	// Merge and rerank results
	mergedResults := r.mergeAndRerank(vectorResults, fullTextResults, req.HybridWeight, req.TopK)

	// Post-process results
	r.postProcessResults(&mergedResults, req.MinScore, req.IncludeContent)

	return &SearchResponse{
		Results:    mergedResults,
		TotalCount: len(mergedResults),
		Query:      req.Query,
		Mode:       SearchModeHybrid,
	}, nil
}

// fetchVectorResults fetches results from vector search
func (r *Retriever) fetchVectorResults(req SearchRequest, queryVector []float32) (map[uuid.UUID]SearchResult, error) {
	query := r.buildVectorSearchQuery(req, queryVector)

	var results []SearchResult
	if err := query.Scan(&results).Error; err != nil {
		return nil, err
	}

	resultMap := make(map[uuid.UUID]SearchResult)
	for _, result := range results {
		// Convert distance to similarity score (cosine distance to similarity)
		result.VectorScore = 1 - result.VectorDistance
		resultMap[result.ChunkID] = result
	}

	return resultMap, nil
}

// fetchFullTextResults fetches results from full-text search
func (r *Retriever) fetchFullTextResults(req SearchRequest) (map[uuid.UUID]SearchResult, error) {
	query := r.buildFullTextSearchQuery(req)

	var results []SearchResult
	if err := query.Scan(&results).Error; err != nil {
		return nil, err
	}

	resultMap := make(map[uuid.UUID]SearchResult)
	for _, result := range results {
		resultMap[result.ChunkID] = result
	}

	return resultMap, nil
}

// mergeAndRerank merges vector and full-text results and reranks them
func (r *Retriever) mergeAndRerank(
	vectorResults map[uuid.UUID]SearchResult,
	fullTextResults map[uuid.UUID]SearchResult,
	vectorWeight float64,
	topK int,
) []SearchResult {
	// Collect all unique chunk IDs
	allIDs := make(map[uuid.UUID]struct{})
	for id := range vectorResults {
		allIDs[id] = struct{}{}
	}
	for id := range fullTextResults {
		allIDs[id] = struct{}{}
	}

	// Calculate combined scores
	var merged []SearchResult
	for id := range allIDs {
		vecResult, hasVector := vectorResults[id]
		textResult, hasFullText := fullTextResults[id]

		var result SearchResult
		var vectorScore, fullTextScore float64

		if hasVector {
			result = vecResult
			vectorScore = vecResult.VectorScore
		}

		if hasFullText {
			if !hasVector {
				result = textResult
			}
			fullTextScore = textResult.FullTextScore
		}

		// Normalize full-text score to 0-1 range if needed
		if fullTextScore > 1.0 {
			fullTextScore = fullTextScore / 10.0 // ts_rank typically returns 0-1, but can be higher
		}
		if fullTextScore > 1.0 {
			fullTextScore = 1.0
		}

		// Calculate weighted final score
		// Use reciprocal rank fusion for better results
		result.FinalScore = r.reciprocalRankFusion(vectorScore, fullTextScore, vectorWeight)
		result.VectorScore = vectorScore
		result.FullTextScore = fullTextScore

		merged = append(merged, result)
	}

	// Sort by final score (descending)
	sortResultsByScore(merged)

	// Return top K results
	if len(merged) > topK {
		merged = merged[:topK]
	}

	return merged
}

// reciprocalRankFusion combines scores using Reciprocal Rank Fusion (RRF)
// This is a more robust method than simple weighted average
func (r *Retriever) reciprocalRankFusion(vectorScore, fullTextScore, vectorWeight float64) float64 {
	const k = 60.0 // RRF constant

	// Convert scores to ranks (higher score = lower rank number)
	// For simplicity, we use the score directly as a proxy for rank
	vectorRank := 1.0 / (vectorScore + 0.01)
	fullTextRank := 1.0 / (fullTextScore + 0.01)

	// Calculate RRF score
	vectorRRF := vectorWeight / (k + vectorRank)
	textRRF := (1 - vectorWeight) / (k + fullTextRank)

	return vectorRRF + textRRF
}

// applyFilters applies custom filters to the query
func (r *Retriever) applyFilters(query *gorm.DB, filters map[string]interface{}) *gorm.DB {
	if filters == nil {
		return query
	}

	// Document metadata filters
	if docMeta, ok := filters["document_metadata"].(map[string]interface{}); ok {
		for key, value := range docMeta {
			query = query.Where("d.metadata->>? = ?", key, value)
		}
	}

	// Chunk metadata filters
	if chunkMeta, ok := filters["chunk_metadata"].(map[string]interface{}); ok {
		for key, value := range chunkMeta {
			query = query.Where("c.metadata->>? = ?", key, value)
		}
	}

	// Source type filter
	if sourceType, ok := filters["source_type"].(string); ok && sourceType != "" {
		query = query.Where("d.source_type = ?", sourceType)
	}

	// Date range filters
	if createdAfter, ok := filters["created_after"].(string); ok && createdAfter != "" {
		query = query.Where("d.created_at >= ?", createdAfter)
	}
	if createdBefore, ok := filters["created_before"].(string); ok && createdBefore != "" {
		query = query.Where("d.created_at <= ?", createdBefore)
	}

	return query
}

// postProcessResults post-processes search results
func (r *Retriever) postProcessResults(results *[]SearchResult, minScore float64, includeContent bool) {
	// Filter by minimum score
	if minScore > 0 {
		filtered := make([]SearchResult, 0, len(*results))
		for _, result := range *results {
			if result.FinalScore >= minScore || (result.FinalScore == 0 && result.VectorScore >= minScore) {
				filtered = append(filtered, result)
			}
		}
		*results = filtered
	}

	// Assign ranks and handle content
	for i := range *results {
		(*results)[i].Rank = i + 1
		if !includeContent {
			(*results)[i].Chunk.Content = "" // Remove content if not requested
		}
		// Ensure FinalScore is set
		if (*results)[i].FinalScore == 0 && (*results)[i].VectorScore > 0 {
			(*results)[i].FinalScore = (*results)[i].VectorScore
		}
		if (*results)[i].FinalScore == 0 && (*results)[i].FullTextScore > 0 {
			(*results)[i].FinalScore = (*results)[i].FullTextScore
		}
	}
}

// vectorToPostgresArray converts a float32 slice to PostgreSQL array format
func vectorToPostgresArray(vector []float32) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range vector {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%f", v))
	}
	sb.WriteString("]")
	return sb.String()
}

// normalizeTsQuery normalizes a query string for PostgreSQL tsquery
func normalizeTsQuery(query string) string {
	// Remove special characters that might break tsquery
	query = strings.ReplaceAll(query, "'", " ")
	query = strings.ReplaceAll(query, "\\", " ")
	query = strings.ReplaceAll(query, "&", " ")
	query = strings.ReplaceAll(query, "|", " ")
	query = strings.ReplaceAll(query, "!", " ")
	query = strings.ReplaceAll(query, "(", " ")
	query = strings.ReplaceAll(query, ")", " ")
	return strings.TrimSpace(query)
}

// sortResultsByScore sorts results by final score in descending order
func sortResultsByScore(results []SearchResult) {
	// Simple bubble sort for small result sets
	// For production, consider using sort.Slice
	n := len(results)
	for i := 0; i < n; i++ {
		for j := 0; j < n-i-1; j++ {
			if results[j].FinalScore < results[j+1].FinalScore {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// GetChunkByID retrieves a specific chunk by ID with its full content
func (r *Retriever) GetChunkByID(ctx context.Context, chunkID uuid.UUID) (*SearchResult, error) {
	var result SearchResult
	// We need to fetch into a flat structure first due to gorm JSON limitations
	var flat struct {
		ChunkID         uuid.UUID
		DocumentID      uuid.UUID
		NamespaceID     uuid.UUID
		ChunkIndex      int
		Content         string
		Location        string `gorm:"column:location"`
		TokenCount      int
		ChunkMetadata   string `gorm:"column:chunk_metadata"`
		SourceType      string
		SourceURI       string
		DocumentMetadata string `gorm:"column:document_metadata"`
	}
	
	err := r.db.Table("chunks AS c").
		Select(`
			c.id as chunk_id,
			c.document_id as document_id,
			d.namespace_id as namespace_id,
			c.chunk_index as chunk_index,
			c.content as content,
			c.location::text as location,
			c.token_count as token_count,
			c.metadata::text as chunk_metadata,
			d.source_type as source_type,
			d.source_uri as source_uri,
			d.metadata::text as document_metadata
		`).
		Joins("JOIN documents d ON d.id = c.document_id").
		Where("c.id = ?", chunkID).
		Scan(&flat).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get chunk: %w", err)
	}

	if flat.ChunkID == uuid.Nil {
		return nil, fmt.Errorf("chunk not found: %s", chunkID)
	}
	
	// Parse JSON
	var location models.LocationInfo
	if flat.Location != "" {
		json.Unmarshal([]byte(flat.Location), &location)
	}
	
	var chunkMetadata map[string]any
	if flat.ChunkMetadata != "" {
		json.Unmarshal([]byte(flat.ChunkMetadata), &chunkMetadata)
	}
	
	var documentMetadata map[string]any
	if flat.DocumentMetadata != "" {
		json.Unmarshal([]byte(flat.DocumentMetadata), &documentMetadata)
	}
	
	// Map flat struct to nested SearchResult
	result = SearchResult{
		ChunkID:     flat.ChunkID,
		DocumentID:  flat.DocumentID,
		NamespaceID: flat.NamespaceID,
		Chunk: ChunkInfo{
			ID:         flat.ChunkID,
			ChunkIndex: flat.ChunkIndex,
			Content:    flat.Content,
			Location:   location,
			TokenCount: flat.TokenCount,
			Metadata:   chunkMetadata,
		},
		Document: DocumentInfo{
			ID:         flat.DocumentID,
			SourceType: flat.SourceType,
			SourceURI:  flat.SourceURI,
			Metadata:   documentMetadata,
		},
	}

	return &result, nil
}

// GetDocumentChunks retrieves all chunks for a document
func (r *Retriever) GetDocumentChunks(ctx context.Context, documentID uuid.UUID) ([]SearchResult, error) {
	// We need to fetch into a flat structure first due to gorm JSON limitations
	type flatChunk struct {
		ChunkID         uuid.UUID
		DocumentID      uuid.UUID
		NamespaceID     uuid.UUID
		ChunkIndex      int
		Content         string
		Location        string `gorm:"column:location"`
		TokenCount      int
		ChunkMetadata   string `gorm:"column:chunk_metadata"`
		SourceType      string
		SourceURI       string
		DocumentMetadata string `gorm:"column:document_metadata"`
	}
	
	var flatResults []flatChunk
	
	err := r.db.Table("chunks AS c").
		Select(`
			c.id as chunk_id,
			c.document_id as document_id,
			d.namespace_id as namespace_id,
			c.chunk_index as chunk_index,
			c.content as content,
			c.location::text as location,
			c.token_count as token_count,
			c.metadata::text as chunk_metadata,
			d.source_type as source_type,
			d.source_uri as source_uri,
			d.metadata::text as document_metadata
		`).
		Joins("JOIN documents d ON d.id = c.document_id").
		Where("c.document_id = ?", documentID).
		Order("c.chunk_index ASC").
		Scan(&flatResults).Error

	if err != nil {
		return nil, fmt.Errorf("failed to get document chunks: %w", err)
	}

	results := make([]SearchResult, len(flatResults))
	for i, flat := range flatResults {
		// Parse JSON
		var location models.LocationInfo
		if flat.Location != "" {
			json.Unmarshal([]byte(flat.Location), &location)
		}
		
		var chunkMetadata map[string]any
		if flat.ChunkMetadata != "" {
			json.Unmarshal([]byte(flat.ChunkMetadata), &chunkMetadata)
		}
		
		var documentMetadata map[string]any
		if flat.DocumentMetadata != "" {
			json.Unmarshal([]byte(flat.DocumentMetadata), &documentMetadata)
		}
		
		results[i] = SearchResult{
			ChunkID:     flat.ChunkID,
			DocumentID:  flat.DocumentID,
			NamespaceID: flat.NamespaceID,
			Chunk: ChunkInfo{
				ID:         flat.ChunkID,
				ChunkIndex: flat.ChunkIndex,
				Content:    flat.Content,
				Location:   location,
				TokenCount: flat.TokenCount,
				Metadata:   chunkMetadata,
			},
			Document: DocumentInfo{
				ID:         flat.DocumentID,
				SourceType: flat.SourceType,
				SourceURI:  flat.SourceURI,
				Metadata:   documentMetadata,
			},
		}
	}

	return results, nil
}

// CalculateCosineSimilarity calculates cosine similarity between two vectors
func CalculateCosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float64
	for i := 0; i < len(a); i++ {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}
