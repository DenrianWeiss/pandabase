package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"pandabase/internal/auth"
	"pandabase/internal/document"
)

// DocumentHandler handles document-related HTTP requests
type DocumentHandler struct {
	service *document.Service
}

// NewDocumentHandler creates a new document handler
func NewDocumentHandler(service *document.Service) *DocumentHandler {
	return &DocumentHandler{service: service}
}

// UploadRequest represents document upload request
type UploadRequest struct {
	ChunkSize      int    `form:"chunk_size"`
	ChunkOverlap   int    `form:"chunk_overlap"`
	ParserType     string `form:"parser_type"`
	SkipEmbedding  bool   `form:"skip_embedding"`
	ForceReprocess bool   `form:"force_reprocess"`
}

// ImportRequestJSON represents document import request via URL
type ImportRequestJSON struct {
	URL              string `json:"url" binding:"required"`
	Title            string `json:"title,omitempty"`
	AutoExtractTitle bool   `json:"auto_extract_title"`
	ParserType       string `json:"parser_type" binding:"required"`
	NotionAPIKey     string `json:"notion_api_key,omitempty"`
	ChunkSize        int    `json:"chunk_size"`
	ChunkOverlap     int    `json:"chunk_overlap"`
	SkipEmbedding    bool   `json:"skip_embedding"`
	RenderJavaScript bool   `json:"render_javascript"`
	RenderTimeout    int    `json:"render_timeout"`
	WaitSelector     string `json:"wait_selector,omitempty"`
	RenderFallback   bool   `json:"render_fallback"`
}

// Upload handles document upload
func (h *DocumentHandler) Upload(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	// Parse form file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// Parse options
	var req UploadRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Upload document
	result, err := h.service.Upload(c.Request.Context(), document.UploadRequest{
		NamespaceID: namespaceID,
		UserID:      userID,
		File:        file,
		FileHeader:  header,
		Options: document.UploadOptions{
			ChunkSize:      req.ChunkSize,
			ChunkOverlap:   req.ChunkOverlap,
			ParserType:     req.ParserType,
			SkipEmbedding:  req.SkipEmbedding,
			ForceReprocess: req.ForceReprocess,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// Import handles document import from URL
func (h *DocumentHandler) Import(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	var req ImportRequestJSON
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	metadata := make(map[string]any)
	if req.NotionAPIKey != "" {
		metadata["notion_api_key"] = req.NotionAPIKey
	}
	if req.Title != "" {
		metadata["title"] = req.Title
	}
	metadata["auto_extract_title"] = req.AutoExtractTitle

	result, err := h.service.ImportURL(c.Request.Context(), document.ImportRequest{
		NamespaceID: namespaceID,
		UserID:      userID,
		URL:         req.URL,
		SourceType:  req.ParserType,
		Metadata:    metadata,
		Options: document.UploadOptions{
			ChunkSize:        req.ChunkSize,
			ChunkOverlap:     req.ChunkOverlap,
			ParserType:       req.ParserType,
			SkipEmbedding:    req.SkipEmbedding,
			RenderJavaScript: req.RenderJavaScript,
			RenderTimeout:    req.RenderTimeout,
			WaitSelector:     req.WaitSelector,
			RenderFallback:   req.RenderFallback,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, result)
}

// Update handles document update
func (h *DocumentHandler) Update(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	// Parse form file
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	defer file.Close()

	// Parse options
	var req UploadRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update document
	result, err := h.service.Update(c.Request.Context(), document.UpdateRequest{
		DocumentID:  documentID,
		NamespaceID: namespaceID,
		UserID:      userID,
		File:        file,
		FileHeader:  header,
		Options: document.UploadOptions{
			ChunkSize:      req.ChunkSize,
			ChunkOverlap:   req.ChunkOverlap,
			ParserType:     req.ParserType,
			SkipEmbedding:  req.SkipEmbedding,
			ForceReprocess: true,
		},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Delete handles document deletion
func (h *DocumentHandler) Delete(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	cascade := c.Query("cascade") == "true"

	if err := h.service.Delete(c.Request.Context(), documentID, namespaceID, userID, cascade); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "document deletion queued"})
}

// Retry handles document processing retry
func (h *DocumentHandler) Retry(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	result, err := h.service.Retry(c.Request.Context(), documentID, namespaceID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// Get handles retrieving a single document
func (h *DocumentHandler) Get(c *gin.Context) {
	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	doc, err := h.service.Get(c.Request.Context(), documentID, namespaceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, doc)
}

// List handles listing documents in a namespace
func (h *DocumentHandler) List(c *gin.Context) {
	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	status := c.Query("status")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	docs, total, err := h.service.List(c.Request.Context(), namespaceID, status, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":        docs,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// UpdateTitleRequest represents a request to update document title
type UpdateTitleRequest struct {
	Title string `json:"title" binding:"required"`
}

// UpdateTitle handles updating document title
func (h *DocumentHandler) UpdateTitle(c *gin.Context) {
	userID, ok := auth.GetUserIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	var req UpdateTitleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.UpdateTitle(c.Request.Context(), documentID, namespaceID, userID, req.Title); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "title updated successfully"})
}
func (h *DocumentHandler) Download(c *gin.Context) {
	namespaceIDStr := c.Param("ns_id")
	namespaceID, err := uuid.Parse(namespaceIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid namespace ID"})
		return
	}

	documentIDStr := c.Param("document_id")
	documentID, err := uuid.Parse(documentIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid document ID"})
		return
	}

	reader, filename, err := h.service.GetFileContent(c.Request.Context(), documentID, namespaceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	defer reader.Close()

	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.DataFromReader(http.StatusOK, -1, "application/octet-stream", reader, nil)
}
