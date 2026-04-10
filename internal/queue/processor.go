package queue

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"pandabase/internal/chunker"
	"pandabase/internal/db/models"
	"pandabase/internal/parser"
	"pandabase/internal/postprocess"
	"pandabase/internal/storage"
	"pandabase/pkg/plugin"
)

// Processor handles task processing
type Processor struct {
	db              *gorm.DB
	storage         storage.Storage
	embedder        plugin.Embedder
	postProcessor   *postprocess.Service
	logger          *logrus.Logger
	httpClient      *http.Client
	staticFetcher   URLFetcher
	renderedFetcher URLFetcher
}

// NewProcessor creates a new task processor
func NewProcessor(db *gorm.DB, storage storage.Storage, embedder plugin.Embedder, postProcessor *postprocess.Service, logger *logrus.Logger) *Processor {
	return &Processor{
		db:       db,
		storage:  storage,
		embedder: embedder,
		postProcessor: postProcessor,
		logger:   logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
		staticFetcher: NewStaticURLFetcher((&http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		})),
		renderedFetcher: NewChromedpURLFetcher(),
	}
}

// RegisterHandlers registers task handlers with the mux
func (p *Processor) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeDocumentProcess, p.handleDocumentProcess)
	mux.HandleFunc(TypeDocumentDelete, p.handleDocumentDelete)
	mux.HandleFunc(TypeDocumentUpdate, p.handleDocumentUpdate)
}

// handleDocumentProcess processes a document
func (p *Processor) handleDocumentProcess(ctx context.Context, task *asynq.Task) error {
	var payload DocumentProcessPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger := p.logger.WithFields(logrus.Fields{
		"document_id":  payload.DocumentID,
		"namespace_id": payload.NamespaceID,
		"task_id":      task.ResultWriter().TaskID(),
	})

	logger.Info("Processing document")

	// Update document status
	if err := p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusProcessing, ""); err != nil {
		logger.WithError(err).Error("Failed to update document status")
		return err
	}

	// Get content
	var content []byte
	var err error
	var finalContentType = payload.ContentType
	var finalFileName = payload.FileName
	var storagePath string

	if payload.SourceURL != "" {
		logger.WithField("url", payload.SourceURL).Info("Fetching content from URL")
		if payload.Options.ParserType == "notion" {
			// Notion parser handles fetching via API using the URL in metadata
			content = []byte{}
			finalContentType = "application/x-notion"
			if finalFileName == "" {
				finalFileName = "notion-page"
			}
		} else {
			content, err = p.fetchURLContent(ctx, payload.SourceURL, payload.Options)
			if err != nil {
				p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, "fetch failed: "+err.Error())
				return err
			}
			if payload.Options.RenderJavaScript {
				finalContentType = "text/plain"
			} else {
				finalContentType = "text/html"
			}
			if finalFileName == "" {
				if payload.Options.RenderJavaScript {
					finalFileName = "webpage_rendered.txt"
				} else {
					finalFileName = "webpage.html"
				}
			}
		}

		// Apply post-processing for web content if enabled
		if p.postProcessor != nil && p.postProcessor.IsEnabled() && payload.SourceURL != "" {
			logger.Info("Applying post-processing to web content")
			startTime := time.Now()
			processedContent, err := p.postProcessor.Process(ctx, string(content))
			if err != nil {
				logger.WithError(err).Warn("Post-processing failed, using original content")
			} else {
				content = []byte(processedContent)
				logger.WithField("duration", time.Since(startTime)).Info("Post-processing completed successfully")
			}
		}

		// Save fetched content to storage for download support
		storagePath, err = p.storage.Save(ctx, finalFileName, strings.NewReader(string(content)))
		if err != nil {
			logger.WithError(err).Warn("Failed to save fetched content to storage")
			// Continue processing even if storage save fails
		} else {
			// Update document's SourceURI to point to the stored file
			if err := p.db.WithContext(ctx).Model(&models.Document{}).
				Where("id = ?", payload.DocumentID).
				Update("source_uri", fmt.Sprintf("file://%s", storagePath)).Error; err != nil {
				logger.WithError(err).Warn("Failed to update document source_uri")
			}
			logger.WithField("path", storagePath).Info("Saved fetched content to storage")
		}
	} else {
		// Get file from storage
		file, err := p.storage.Get(ctx, payload.FilePath)
		if err != nil {
			p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
			logger.WithError(err).Error("Failed to get file from storage")
			return err
		}
		defer file.Close()

		// Read content
		content, err = io.ReadAll(file)
		if err != nil {
			p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
			logger.WithError(err).Error("Failed to read file content")
			return err
		}
	}

	// Calculate content hash
	hash := calculateHash(content)

	// Check for existing document with same hash (unless force reprocess)
	if !payload.Options.ForceReprocess {
		var existingDoc models.Document
		if err := p.db.WithContext(ctx).Where("namespace_id = ? AND content_hash = ? AND status = ? AND id != ?",
			payload.NamespaceID, hash, models.DocumentStatusCompleted, payload.DocumentID).First(&existingDoc).Error; err == nil {
			// Same content already exists
			p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, "duplicate content")
			logger.Warn("Duplicate content detected")
			return fmt.Errorf("document with same content already exists: %s", existingDoc.ID)
		}
	}

	// Select parser based on content type
	contentType := finalContentType
	if payload.Options.ParserType != "auto" && payload.Options.ParserType != "" {
		forceExtractedTextParser := payload.SourceURL != "" &&
			payload.Options.ParserType == "web" &&
			payload.Options.RenderJavaScript &&
			strings.Contains(finalContentType, "text/plain")
		if !forceExtractedTextParser {
			contentType = payload.Options.ParserType
		}
	}

	var docParser plugin.DocumentParser
	ext := strings.ToLower(filepath.Ext(finalFileName))

	switch {
	case strings.Contains(contentType, "pdf") || ext == ".pdf":
		docParser = parser.NewPDFParser()
	case strings.Contains(contentType, "notion") || ext == ".notion":
		docParser = parser.NewNotionParser()
	case strings.Contains(contentType, "html") || strings.Contains(contentType, "web") || ext == ".html" || ext == ".htm":
		docParser = parser.NewWebParser()
	case strings.Contains(contentType, "markdown") || ext == ".md" || ext == ".markdown":
		docParser = parser.NewMarkdownParser()
	case strings.Contains(contentType, "text") || ext == ".txt" || ext == ".text":
		docParser = parser.NewTextParser()
	default:
		// Default to text parser
		docParser = parser.NewTextParser()
	}

	// Parse document
	parseOpts := plugin.ParseOptions{
		Filename:         finalFileName,
		ContentType:      finalContentType,
		RenderJavaScript: payload.Options.RenderJavaScript,
		RenderTimeout:    payload.Options.RenderTimeout,
		WaitSelector:     payload.Options.WaitSelector,
		Metadata: map[string]any{
			"file_name":         finalFileName,
			"content_type":      finalContentType,
			"render_javascript": payload.Options.RenderJavaScript,
			"render_timeout":    payload.Options.RenderTimeout,
			"wait_selector":     payload.Options.WaitSelector,
		},
	}

	// Add source URL and metadata if available
	if payload.SourceURL != "" {
		parseOpts.Metadata["source_url"] = payload.SourceURL
		if payload.Options.ParserType == "notion" {
			parseOpts.Metadata["notion_url"] = payload.SourceURL
		}
	}
	if payload.SourceMetadata != nil {
		for k, v := range payload.SourceMetadata {
			parseOpts.Metadata[k] = v
		}
	}

	doc, err := docParser.Parse(ctx, strings.NewReader(string(content)), parseOpts)
	if err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to parse document")
		return err
	}

	// Handle title extraction/setting for web pages
	// For web content, always try to use the extracted title from the parser
	if payload.SourceURL != "" {
		// Web parser already extracts title into doc.Metadata["title"]
		if extractedTitle, ok := doc.Metadata["title"].(string); !ok || extractedTitle == "" {
			// If no title extracted, use the filename as fallback
			doc.Metadata["title"] = finalFileName
		}
		// If user provided a manual title via SourceMetadata, override the extracted one
		if payload.SourceMetadata != nil {
			if manualTitle, hasManualTitle := payload.SourceMetadata["title"].(string); hasManualTitle && manualTitle != "" {
				doc.Metadata["title"] = manualTitle
			}
		}
	}

	// Select chunker
	var docChunker plugin.Chunker
	if _, ok := docParser.(*parser.MarkdownParser); ok {
		docChunker = chunker.NewMarkdownChunker(payload.Options.ChunkSize)
	} else {
		docChunker = chunker.NewLineBasedChunker(payload.Options.ChunkSize, payload.Options.ChunkOverlap)
	}

	// Chunk document
	chunks, err := docChunker.Split(ctx, doc)
	if err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to chunk document")
		return err
	}

	logger.WithField("chunk_count", len(chunks)).Info("Document chunked")

	// Find existing chunks to perform diff analysis
	var oldChunks []models.Chunk
	if err := p.db.WithContext(ctx).Where("document_id = ?", payload.DocumentID).Find(&oldChunks).Error; err != nil {
		logger.WithError(err).Warn("Failed to fetch existing chunks for diff")
	}

	// Map content to old chunks for fast lookup
	oldMap := make(map[string][]models.Chunk)
	for _, c := range oldChunks {
		oldMap[c.Content] = append(oldMap[c.Content], c)
	}

	var chunksToCreate []models.Chunk
	var chunkContentsToEmbed []string
	var chunksToUpdate []models.Chunk
	var oldChunkIDsToDelete []uuid.UUID

	for i, chunk := range chunks {
		var matchedOldChunk *models.Chunk
		if list, ok := oldMap[chunk.Content]; ok && len(list) > 0 {
			matchedOldChunk = &list[0]
			oldMap[chunk.Content] = list[1:]
		}

		locationInfo := models.LocationInfo{
			Type:      chunk.Location.Type,
			URI:       chunk.Location.URI,
			Section:   chunk.Location.Section,
			LineStart: chunk.Location.LineStart,
			LineEnd:   chunk.Location.LineEnd,
			Offset:    chunk.Location.Offset,
		}

		metadataInfo := map[string]any{
			"file_name":    finalFileName,
			"content_type": finalContentType,
		}

		if matchedOldChunk != nil {
			// Update chunk in place if metadata/index changed
			matchedOldChunk.ChunkIndex = i
			matchedOldChunk.Location = locationInfo
			matchedOldChunk.TokenCount = chunker.TokenCount(chunk.Content)
			matchedOldChunk.Metadata = metadataInfo
			chunksToUpdate = append(chunksToUpdate, *matchedOldChunk)
		} else {
			// New chunk
			dbChunk := models.Chunk{
				DocumentID: payload.DocumentID,
				ChunkIndex: i,
				Content:    chunk.Content,
				Location:   locationInfo,
				TokenCount: chunker.TokenCount(chunk.Content),
				Metadata:   metadataInfo,
			}
			chunksToCreate = append(chunksToCreate, dbChunk)
			chunkContentsToEmbed = append(chunkContentsToEmbed, chunk.Content)
		}
	}

	// Any remaining in oldMap are to be deleted
	for _, list := range oldMap {
		for _, c := range list {
			oldChunkIDsToDelete = append(oldChunkIDsToDelete, c.ID)
		}
	}

	if len(oldChunkIDsToDelete) > 0 {
		logger.WithField("deleted_chunks", len(oldChunkIDsToDelete)).Info("Deleting removed chunks")
		if err := p.db.WithContext(ctx).Where("id IN ?", oldChunkIDsToDelete).Delete(&models.Chunk{}).Error; err != nil {
			logger.WithError(err).Warn("Failed to delete removed chunks")
		}
	}

	if len(chunksToUpdate) > 0 {
		logger.WithField("updated_chunks", len(chunksToUpdate)).Info("Updating existing chunks")
		for _, c := range chunksToUpdate {
			if err := p.db.WithContext(ctx).Save(&c).Error; err != nil {
				logger.WithError(err).Warn("Failed to update chunk")
			}
		}
	}

	if len(chunksToCreate) > 0 {
		logger.WithField("new_chunks", len(chunksToCreate)).Info("Creating new chunks")
		if err := p.db.WithContext(ctx).Create(&chunksToCreate).Error; err != nil {
			p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
			logger.WithError(err).Error("Failed to save new chunks")
			return err
		}

		// Generate embeddings if not skipped
		if !payload.Options.SkipEmbedding && p.embedder != nil {
			logger.Info("Generating embeddings for new chunks")

			embeddings, err := p.embedder.Embed(ctx, chunkContentsToEmbed)
			if err != nil {
				p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
				logger.WithError(err).Error("Failed to generate embeddings")
				return err
			}

			// Save embeddings
			for i, chunk := range chunksToCreate {
				embedding := models.Embedding{
					ChunkID: chunk.ID,
					Model:   p.embedder.Model(),
				}

				if err := p.saveEmbedding(ctx, embedding, embeddings[i]); err != nil {
					logger.WithError(err).Warn("Failed to save embedding for chunk")
				}
			}

			logger.WithField("embedding_count", len(embeddings)).Info("New embeddings generated")
		}
	}

	// Update document status and metadata
	updates := map[string]interface{}{
		"status":       models.DocumentStatusCompleted,
		"content_hash": hash,
		"updated_at":   time.Now(),
	}

	// Update metadata with title if present
	if title, ok := doc.Metadata["title"].(string); ok && title != "" {
		// Get current document to update metadata
		var currentDoc models.Document
		if err := p.db.WithContext(ctx).First(&currentDoc, "id = ?", payload.DocumentID).Error; err == nil {
			if currentDoc.Metadata == nil {
				currentDoc.Metadata = make(map[string]any)
			}
			currentDoc.Metadata["title"] = title
			// Merge other metadata from parsing
			for k, v := range doc.Metadata {
				if k != "title" {
					currentDoc.Metadata[k] = v
				}
			}
			updates["metadata"] = currentDoc.Metadata
		}
	}

	if err := p.db.WithContext(ctx).Model(&models.Document{}).
		Where("id = ?", payload.DocumentID).
		Updates(updates).Error; err != nil {
		logger.WithError(err).Error("Failed to update document status")
		return err
	}

	logger.Info("Document processing completed")
	return nil
}

// handleDocumentDelete handles document deletion
func (p *Processor) handleDocumentDelete(ctx context.Context, task *asynq.Task) error {
	var payload DocumentDeletePayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	logger := p.logger.WithFields(logrus.Fields{
		"document_id":  payload.DocumentID,
		"namespace_id": payload.NamespaceID,
	})

	logger.Info("Deleting document")

	// Get document to find file path
	var doc models.Document
	if err := p.db.WithContext(ctx).First(&doc, "id = ?", payload.DocumentID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.Warn("Document not found")
			return nil // Already deleted
		}
		return err
	}

	// Delete chunks and embeddings (cascade)
	if payload.CascadeDelete {
		if err := p.db.WithContext(ctx).Where("document_id = ?", payload.DocumentID).Delete(&models.Chunk{}).Error; err != nil {
			logger.WithError(err).Error("Failed to delete chunks")
			return err
		}
	}

	// Delete file from storage
	if doc.SourceURI != "" {
		// Extract path from source URI
		path := strings.TrimPrefix(doc.SourceURI, "file://")
		if err := p.storage.Delete(ctx, path); err != nil {
			logger.WithError(err).Warn("Failed to delete file from storage")
			// Continue with document deletion even if file deletion fails
		}
	}

	// Delete document
	if err := p.db.WithContext(ctx).Unscoped().Delete(&doc).Error; err != nil {
		logger.WithError(err).Error("Failed to delete document")
		return err
	}

	logger.Info("Document deleted successfully")
	return nil
}

func (p *Processor) fetchURLContent(ctx context.Context, sourceURL string, options ProcessingOptions) ([]byte, error) {
	fetchOpts := URLFetchOptions{
		RenderTimeout: options.RenderTimeout,
		WaitSelector:  options.WaitSelector,
	}

	if options.RenderJavaScript {
		rendered, err := p.renderedFetcher.Fetch(ctx, sourceURL, fetchOpts)
		if err == nil {
			return rendered, nil
		}
		if !options.RenderFallback {
			return nil, fmt.Errorf("render fetch failed: %w", err)
		}
		p.logger.WithError(err).Warn("Rendered fetch failed, fallback to static fetch")
	}
	return p.staticFetcher.Fetch(ctx, sourceURL, fetchOpts)
}

// handleDocumentUpdate handles document update
func (p *Processor) handleDocumentUpdate(ctx context.Context, task *asynq.Task) error {
	// Update is essentially reprocessing
	return p.handleDocumentProcess(ctx, task)
}

// updateDocumentStatus updates document status
func (p *Processor) updateDocumentStatus(ctx context.Context, docID uuid.UUID, status models.DocumentStatus, errorMsg string) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}
	if errorMsg != "" {
		updates["error_message"] = errorMsg
	}

	return p.db.WithContext(ctx).Model(&models.Document{}).
		Where("id = ?", docID).
		Updates(updates).Error
}

// saveEmbedding saves embedding to database
func (p *Processor) saveEmbedding(ctx context.Context, embedding models.Embedding, vector []float32) error {
	// Use raw SQL to insert vector
	query := `
		INSERT INTO embeddings (id, chunk_id, model, embedding, created_at)
		VALUES (gen_random_uuid(), ?, ?, ?, NOW())
	`

	// Convert float32 slice to PostgreSQL vector format
	vectorStr := vectorToPostgresString(vector)

	return p.db.WithContext(ctx).Exec(query, embedding.ChunkID, embedding.Model, vectorStr).Error
}

// vectorToPostgresString converts float32 slice to PostgreSQL vector string
func vectorToPostgresString(vector []float32) string {
	parts := make([]string, len(vector))
	for i, v := range vector {
		parts[i] = fmt.Sprintf("%f", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// calculateHash calculates SHA256 hash of content
func calculateHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}
