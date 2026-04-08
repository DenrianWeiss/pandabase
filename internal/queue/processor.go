package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	"pandabase/internal/storage"
	"pandabase/pkg/plugin"
)

// Processor handles task processing
type Processor struct {
	db       *gorm.DB
	storage  storage.Storage
	embedder plugin.Embedder
	logger   *logrus.Logger
}

// NewProcessor creates a new task processor
func NewProcessor(db *gorm.DB, storage storage.Storage, embedder plugin.Embedder, logger *logrus.Logger) *Processor {
	return &Processor{
		db:       db,
		storage:  storage,
		embedder: embedder,
		logger:   logger,
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

	// Get file from storage
	file, err := p.storage.Get(ctx, payload.FilePath)
	if err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to get file from storage")
		return err
	}
	defer file.Close()

	// Read content
	content, err := io.ReadAll(file)
	if err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to read file content")
		return err
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
	contentType := payload.ContentType
	if payload.Options.ParserType != "auto" && payload.Options.ParserType != "" {
		contentType = payload.Options.ParserType
	}

	var docParser plugin.DocumentParser
	ext := strings.ToLower(filepath.Ext(payload.FileName))
	
	switch {
	case strings.Contains(contentType, "pdf") || ext == ".pdf":
		docParser = parser.NewPDFParser()
	case strings.Contains(contentType, "notion") || ext == ".notion":
		docParser = parser.NewNotionParser()
	case strings.Contains(contentType, "html") || ext == ".html" || ext == ".htm":
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
		Filename:    payload.FileName,
		ContentType: payload.ContentType,
		Metadata: map[string]any{
			"file_name":    payload.FileName,
			"content_type": payload.ContentType,
		},
	}
	
	doc, err := docParser.Parse(ctx, strings.NewReader(string(content)), parseOpts)
	if err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to parse document")
		return err
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

	// Delete existing chunks if any (for reprocessing)
	if err := p.db.WithContext(ctx).Where("document_id = ?", payload.DocumentID).Delete(&models.Chunk{}).Error; err != nil {
		logger.WithError(err).Warn("Failed to delete existing chunks")
	}

	// Create chunks
	dbChunks := make([]models.Chunk, len(chunks))
	chunkContents := make([]string, len(chunks))
	
	for i, chunk := range chunks {
		dbChunks[i] = models.Chunk{
			DocumentID: payload.DocumentID,
			ChunkIndex: i,
			Content:    chunk.Content,
			Location: models.LocationInfo{
				Type:      chunk.Location.Type,
				URI:       chunk.Location.URI,
				Section:   chunk.Location.Section,
				LineStart: chunk.Location.LineStart,
				LineEnd:   chunk.Location.LineEnd,
				Offset:    chunk.Location.Offset,
			},
			TokenCount: chunker.TokenCount(chunk.Content),
			Metadata: map[string]any{
				"file_name":    payload.FileName,
				"content_type": payload.ContentType,
			},
		}
		chunkContents[i] = chunk.Content
	}

	// Save chunks to database
	if err := p.db.WithContext(ctx).Create(&dbChunks).Error; err != nil {
		p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
		logger.WithError(err).Error("Failed to save chunks")
		return err
	}

	// Generate embeddings if not skipped
	if !payload.Options.SkipEmbedding && p.embedder != nil {
		logger.Info("Generating embeddings")
		
		embeddings, err := p.embedder.Embed(ctx, chunkContents)
		if err != nil {
			p.updateDocumentStatus(ctx, payload.DocumentID, models.DocumentStatusFailed, err.Error())
			logger.WithError(err).Error("Failed to generate embeddings")
			return err
		}

		// Save embeddings
		for i, chunk := range dbChunks {
			embedding := models.Embedding{
				ChunkID: chunk.ID,
				Model:   p.embedder.Model(),
			}
			
			// The actual vector will be set by the database layer
			// We need to use raw SQL or a custom method to insert the vector
			if err := p.saveEmbedding(ctx, embedding, embeddings[i]); err != nil {
				logger.WithError(err).Warn("Failed to save embedding for chunk")
			}
		}

		logger.WithField("embedding_count", len(embeddings)).Info("Embeddings generated")
	}

	// Update document status
	if err := p.db.WithContext(ctx).Model(&models.Document{}).
		Where("id = ?", payload.DocumentID).
		Updates(map[string]interface{}{
			"status":       models.DocumentStatusCompleted,
			"content_hash": hash,
			"updated_at":   time.Now(),
		}).Error; err != nil {
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
	if err := p.db.WithContext(ctx).Delete(&doc).Error; err != nil {
		logger.WithError(err).Error("Failed to delete document")
		return err
	}

	logger.Info("Document deleted successfully")
	return nil
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
