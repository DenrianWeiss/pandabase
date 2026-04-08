package handlers

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"pandabase/internal/retriever"
)

// RetrieverHandler handles requests related to document search and retrieval
type RetrieverHandler struct {
	retriever *retriever.Retriever
}

// NewRetrieverHandler creates a new RetrieverHandler
func NewRetrieverHandler(r *retriever.Retriever) *RetrieverHandler {
	return &RetrieverHandler{
		retriever: r,
	}
}

// Search handles the main search endpoint
func (h *RetrieverHandler) Search(c *gin.Context) {
	var req retriever.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
		return
	}

	// Validate request
	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Validation failed", "details": err.Error()})
		return
	}

	// Default values if not provided
	if req.TopK == 0 {
		req.TopK = 10
	}
	if req.Mode == "" {
		req.Mode = retriever.SearchModeHybrid
	}

	// Execute search
	resp, err := h.retriever.Search(c.Request.Context(), req)
	if err != nil {
		log.Printf("Search failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetChunkByID retrieves a specific chunk (original material part) by ID
func (h *RetrieverHandler) GetChunkByID(c *gin.Context) {
	idParam := c.Param("id")
	chunkID, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid chunk ID format"})
		return
	}

	result, err := h.retriever.GetChunkByID(c.Request.Context(), chunkID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Chunk not found", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetDocumentChunks retrieves all chunks from a specific document
func (h *RetrieverHandler) GetDocumentChunks(c *gin.Context) {
	idParam := c.Param("id")
	documentID, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document ID format"})
		return
	}

	results, err := h.retriever.GetDocumentChunks(c.Request.Context(), documentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve document chunks", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"document_id": documentID,
		"chunks":      results,
		"count":       len(results),
	})
}
