package document

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"pandabase/internal/db/models"
	"pandabase/internal/queue"
	"pandabase/internal/storage"
)

// Service handles document lifecycle management
type Service struct {
	db        *gorm.DB
	storage   storage.Storage
	queue     *queue.Client
	logger    *logrus.Logger
}

// NewService creates a new document service
func NewService(db *gorm.DB, storage storage.Storage, queueClient *queue.Client, logger *logrus.Logger) *Service {
	return &Service{
		db:      db,
		storage: storage,
		queue:   queueClient,
		logger:  logger,
	}
}

// UploadRequest represents a document upload request
type UploadRequest struct {
	NamespaceID uuid.UUID
	UserID      uuid.UUID
	File        multipart.File
	FileHeader  *multipart.FileHeader
	Options     UploadOptions
}

// UploadOptions represents upload options
type UploadOptions struct {
	ChunkSize      int
	ChunkOverlap   int
	ParserType     string // auto, text, markdown
	SkipEmbedding  bool
	ForceReprocess bool
}

// UploadResult represents upload result
type UploadResult struct {
	DocumentID uuid.UUID              `json:"document_id"`
	Status     models.DocumentStatus  `json:"status"`
	TaskID     string                 `json:"task_id,omitempty"`
	Message    string                 `json:"message,omitempty"`
}

// Upload handles document upload and triggers processing
func (s *Service) Upload(ctx context.Context, req UploadRequest) (*UploadResult, error) {
	logger := s.logger.WithFields(logrus.Fields{
		"namespace_id": req.NamespaceID,
		"user_id":      req.UserID,
		"filename":     req.FileHeader.Filename,
	})

	logger.Info("Uploading document")

	// Check permission
	if err := s.checkNamespacePermission(ctx, req.NamespaceID, req.UserID, "write"); err != nil {
		return nil, err
	}

	// Read file content for hash calculation
	content, err := io.ReadAll(req.File)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	req.File.Seek(0, io.SeekStart) // Reset for storage

	// Calculate content hash
	hash := calculateHash(content)

	// Check for duplicate content
	var existingDoc models.Document
	if err := s.db.WithContext(ctx).Where("namespace_id = ? AND content_hash = ? AND status = ?",
		req.NamespaceID, hash, models.DocumentStatusCompleted).First(&existingDoc).Error; err == nil {
		// Content already exists - check if user wants to reprocess
		if !req.Options.ForceReprocess {
			return &UploadResult{
				DocumentID: existingDoc.ID,
				Status:     existingDoc.Status,
				Message:    "Document with same content already exists",
			}, nil
		}
		// Delete old document and reprocess
		if err := s.Delete(ctx, existingDoc.ID, req.NamespaceID, req.UserID, true); err != nil {
			logger.WithError(err).Warn("Failed to delete existing document for reprocessing")
		}
	}

	// Store file
	filePath, err := s.storage.Save(ctx, req.FileHeader.Filename, strings.NewReader(string(content)))
	if err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// Create document record
	doc := models.Document{
		NamespaceID: req.NamespaceID,
		SourceType:  "file",
		SourceURI:   fmt.Sprintf("file://%s", filePath),
		ContentHash: hash,
		Status:      models.DocumentStatusPending,
		Metadata: map[string]any{
			"original_filename": req.FileHeader.Filename,
			"file_size":         req.FileHeader.Size,
			"uploaded_by":       req.UserID,
			"content_type":      req.FileHeader.Header.Get("Content-Type"),
		},
	}

	if err := s.db.WithContext(ctx).Create(&doc).Error; err != nil {
		// Clean up stored file
		s.storage.Delete(ctx, filePath)
		return nil, fmt.Errorf("failed to create document record: %w", err)
	}

	// Set default options
	chunkSize := req.Options.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	chunkOverlap := req.Options.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 100
	}

	// Enqueue processing task
	payload := queue.DocumentProcessPayload{
		TaskPayload: queue.TaskPayload{
			DocumentID:  doc.ID,
			NamespaceID: req.NamespaceID,
			UserID:      req.UserID,
		},
		FilePath:    filePath,
		FileName:    req.FileHeader.Filename,
		ContentType: req.FileHeader.Header.Get("Content-Type"),
		Options: queue.ProcessingOptions{
			ChunkSize:      chunkSize,
			ChunkOverlap:   chunkOverlap,
			ParserType:     req.Options.ParserType,
			SkipEmbedding:  req.Options.SkipEmbedding,
			ForceReprocess: req.Options.ForceReprocess,
		},
	}

	taskInfo, err := s.queue.EnqueueDocumentProcess(ctx, payload, queue.DefaultTaskOptions()...)
	if err != nil {
		// Update document status to failed
		s.db.WithContext(ctx).Model(&doc).Update("status", models.DocumentStatusFailed)
		return nil, fmt.Errorf("failed to enqueue processing task: %w", err)
	}

	logger.WithFields(logrus.Fields{
		"document_id": doc.ID,
		"task_id":     taskInfo.ID,
	}).Info("Document upload successful, processing queued")

	return &UploadResult{
		DocumentID: doc.ID,
		Status:     models.DocumentStatusPending,
		TaskID:     taskInfo.ID,
		Message:    "Document uploaded and queued for processing",
	}, nil
}

// UpdateRequest represents a document update request
type UpdateRequest struct {
	DocumentID  uuid.UUID
	NamespaceID uuid.UUID
	UserID      uuid.UUID
	File        multipart.File
	FileHeader  *multipart.FileHeader
	Options     UploadOptions
}

// Update handles document update (re-upload and reprocess)
func (s *Service) Update(ctx context.Context, req UpdateRequest) (*UploadResult, error) {
	logger := s.logger.WithFields(logrus.Fields{
		"document_id":  req.DocumentID,
		"namespace_id": req.NamespaceID,
		"user_id":      req.UserID,
	})

	logger.Info("Updating document")

	// Check permission
	if err := s.checkNamespacePermission(ctx, req.NamespaceID, req.UserID, "write"); err != nil {
		return nil, err
	}

	// Get existing document
	var doc models.Document
	if err := s.db.WithContext(ctx).Where("id = ? AND namespace_id = ?", req.DocumentID, req.NamespaceID).First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("document not found")
		}
		return nil, err
	}

	// Delete old file from storage
	if doc.SourceURI != "" {
		oldPath := strings.TrimPrefix(doc.SourceURI, "file://")
		s.storage.Delete(ctx, oldPath)
	}

	// Read new file content
	content, err := io.ReadAll(req.File)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Calculate new hash
	hash := calculateHash(content)

	// Store new file
	filePath, err := s.storage.Save(ctx, req.FileHeader.Filename, strings.NewReader(string(content)))
	if err != nil {
		return nil, fmt.Errorf("failed to save file: %w", err)
	}

	// Update document record
	doc.SourceURI = fmt.Sprintf("file://%s", filePath)
	doc.ContentHash = hash
	doc.Status = models.DocumentStatusPending
	doc.Metadata["original_filename"] = req.FileHeader.Filename
	doc.Metadata["file_size"] = req.FileHeader.Size
	doc.Metadata["updated_by"] = req.UserID
	doc.Metadata["content_type"] = req.FileHeader.Header.Get("Content-Type")
	doc.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(&doc).Error; err != nil {
		s.storage.Delete(ctx, filePath)
		return nil, fmt.Errorf("failed to update document record: %w", err)
	}

	// Enqueue update task
	payload := queue.DocumentProcessPayload{
		TaskPayload: queue.TaskPayload{
			DocumentID:  doc.ID,
			NamespaceID: req.NamespaceID,
			UserID:      req.UserID,
		},
		FilePath:    filePath,
		FileName:    req.FileHeader.Filename,
		ContentType: req.FileHeader.Header.Get("Content-Type"),
		Options: queue.ProcessingOptions{
			ChunkSize:      req.Options.ChunkSize,
			ChunkOverlap:   req.Options.ChunkOverlap,
			ParserType:     req.Options.ParserType,
			SkipEmbedding:  req.Options.SkipEmbedding,
			ForceReprocess: true,
		},
	}

	taskInfo, err := s.queue.EnqueueDocumentUpdate(ctx, payload, queue.CriticalTaskOptions()...)
	if err != nil {
		s.db.WithContext(ctx).Model(&doc).Update("status", models.DocumentStatusFailed)
		return nil, fmt.Errorf("failed to enqueue update task: %w", err)
	}

	logger.WithField("task_id", taskInfo.ID).Info("Document update queued")

	return &UploadResult{
		DocumentID: doc.ID,
		Status:     models.DocumentStatusPending,
		TaskID:     taskInfo.ID,
		Message:    "Document updated and queued for reprocessing",
	}, nil
}

// Delete handles document deletion
func (s *Service) Delete(ctx context.Context, documentID, namespaceID, userID uuid.UUID, cascade bool) error {
	logger := s.logger.WithFields(logrus.Fields{
		"document_id":  documentID,
		"namespace_id": namespaceID,
		"user_id":      userID,
	})

	logger.Info("Deleting document")

	// Check permission (need write or owner)
	if err := s.checkNamespacePermission(ctx, namespaceID, userID, "write"); err != nil {
		// Check if user is document owner
		var doc models.Document
		if err := s.db.WithContext(ctx).First(&doc, "id = ?", documentID).Error; err != nil {
			return err
		}
		if uploadedBy, ok := doc.Metadata["uploaded_by"].(string); ok {
			if uploadedBy != userID.String() {
				return errors.New("permission denied")
			}
		} else {
			return errors.New("permission denied")
		}
	}

	// Enqueue deletion task
	payload := queue.DocumentDeletePayload{
		TaskPayload: queue.TaskPayload{
			DocumentID:  documentID,
			NamespaceID: namespaceID,
			UserID:      userID,
		},
		CascadeDelete: cascade,
	}

	_, err := s.queue.EnqueueDocumentDelete(ctx, payload, queue.DefaultTaskOptions()...)
	if err != nil {
		return fmt.Errorf("failed to enqueue deletion task: %w", err)
	}

	// Update document status to deleted
	if err := s.db.WithContext(ctx).Model(&models.Document{}).
		Where("id = ?", documentID).
		Update("status", models.DocumentStatusDeleted).Error; err != nil {
		logger.WithError(err).Warn("Failed to update document status to deleted")
	}

	logger.Info("Document deletion queued")
	return nil
}

// Get retrieves a document by ID
func (s *Service) Get(ctx context.Context, documentID, namespaceID uuid.UUID) (*models.Document, error) {
	var doc models.Document
	if err := s.db.WithContext(ctx).Preload("Chunks").Where("id = ? AND namespace_id = ?", documentID, namespaceID).First(&doc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("document not found")
		}
		return nil, err
	}
	return &doc, nil
}

// List retrieves documents in a namespace with pagination
func (s *Service) List(ctx context.Context, namespaceID uuid.UUID, status string, page, pageSize int) ([]models.Document, int64, error) {
	var docs []models.Document
	var total int64

	query := s.db.WithContext(ctx).Model(&models.Document{}).Where("namespace_id = ?", namespaceID)
	
	if status != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}

	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Limit(pageSize).Offset(offset).Find(&docs).Error; err != nil {
		return nil, 0, err
	}

	return docs, total, nil
}

// checkNamespacePermission checks if user has permission in namespace
func (s *Service) checkNamespacePermission(ctx context.Context, namespaceID, userID uuid.UUID, action string) error {
	// Check if user is namespace owner
	var namespace models.Namespace
	if err := s.db.WithContext(ctx).First(&namespace, "id = ?", namespaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("namespace not found")
		}
		return err
	}

	if namespace.OwnerID == userID {
		return nil
	}

	// Check namespace membership
	var member models.NamespaceMember
	if err := s.db.WithContext(ctx).Where("namespace_id = ? AND user_id = ?", namespaceID, userID).First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("permission denied")
		}
		return err
	}

	// Check role permissions
	requiredRole := "viewer"
	if action == "write" || action == "delete" {
		requiredRole = "editor"
	}

	if !member.HasPermission(requiredRole) {
		return errors.New("permission denied")
	}

	return nil
}

// calculateHash calculates SHA256 hash of content
func calculateHash(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// GetFileContent retrieves file content for a document
func (s *Service) GetFileContent(ctx context.Context, documentID, namespaceID uuid.UUID) (io.ReadCloser, string, error) {
	doc, err := s.Get(ctx, documentID, namespaceID)
	if err != nil {
		return nil, "", err
	}

	if doc.SourceURI == "" {
		return nil, "", errors.New("document has no file")
	}

	path := strings.TrimPrefix(doc.SourceURI, "file://")
	reader, err := s.storage.Get(ctx, path)
	if err != nil {
		return nil, "", err
	}

	filename := ""
	if fn, ok := doc.Metadata["original_filename"].(string); ok {
		filename = fn
	} else {
		filename = filepath.Base(path)
	}

	return reader, filename, nil
}
