package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Namespace represents a multi-tenant isolation unit
type Namespace struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Name        string         `gorm:"type:varchar(255);not null;uniqueIndex" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	AccessLevel string         `gorm:"type:varchar(50);not null;default:'private'" json:"access_level"`
	OwnerID     uuid.UUID      `gorm:"type:uuid;not null" json:"owner_id"`
	Metadata    map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	Documents []Document `gorm:"foreignKey:NamespaceID;constraint:OnDelete:CASCADE" json:"documents,omitempty"`
}

// TableName specifies the table name for Namespace
func (Namespace) TableName() string {
	return "namespaces"
}

// DocumentStatus represents the status of a document
type DocumentStatus string

const (
	DocumentStatusPending    DocumentStatus = "pending"
	DocumentStatusProcessing DocumentStatus = "processing"
	DocumentStatusCompleted  DocumentStatus = "completed"
	DocumentStatusFailed     DocumentStatus = "failed"
	DocumentStatusDeleted    DocumentStatus = "deleted"
)

// Document represents a source document
type Document struct {
	ID           uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	NamespaceID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"namespace_id"`
	SourceType   string         `gorm:"type:varchar(50);not null" json:"source_type"`
	SourceURI    string         `gorm:"type:text;not null" json:"source_uri"`
	ContentHash  string         `gorm:"type:varchar(64);not null;index" json:"content_hash"`
	Status       DocumentStatus `gorm:"type:varchar(50);not null;default:'pending'" json:"status"`
	Metadata     map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`

	// Associations
	Namespace Namespace `gorm:"foreignKey:NamespaceID" json:"namespace,omitempty"`
	Chunks    []Chunk   `gorm:"foreignKey:DocumentID;constraint:OnDelete:CASCADE" json:"chunks,omitempty"`
}

// TableName specifies the table name for Document
func (Document) TableName() string {
	return "documents"
}

// BeforeCreate hook to set default status
func (d *Document) BeforeCreate(tx *gorm.DB) error {
	if d.Status == "" {
		d.Status = DocumentStatusPending
	}
	return nil
}

// LocationInfo represents the location of a chunk in the source document
type LocationInfo struct {
	Type      string `json:"type"`       // file, notion, webpage, etc.
	URI       string `json:"uri"`        // Source address
	Section   string `json:"section"`    // Section heading
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Offset    int    `json:"offset"`
}

// Chunk represents a text segment from a document
type Chunk struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	DocumentID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"document_id"`
	ChunkIndex  int            `gorm:"not null" json:"chunk_index"`
	Content     string         `gorm:"type:text;not null" json:"content"`
	Location    LocationInfo   `gorm:"type:jsonb;serializer:json" json:"location"`
	TokenCount  int            `json:"token_count"`
	Metadata    map[string]any `gorm:"type:jsonb;serializer:json" json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`

	// Associations
	Document  Document   `gorm:"foreignKey:DocumentID" json:"document,omitempty"`
	Embedding *Embedding `gorm:"foreignKey:ChunkID;constraint:OnDelete:CASCADE" json:"embedding,omitempty"`
}

// TableName specifies the table name for Chunk
func (Chunk) TableName() string {
	return "chunks"
}

// Embedding represents a vector embedding of a chunk
// The vector dimension is configured at startup and cannot be changed at runtime
type Embedding struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	ChunkID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"chunk_id"`
	Model     string    `gorm:"type:varchar(100);not null" json:"model"`
	CreatedAt time.Time `json:"created_at"`

	// Embedding vector is stored using pgvector with dynamic dimensions
	// The actual dimension is set at database initialization time
	EmbeddingVector interface{} `gorm:"-" json:"-"`

	// Associations
	Chunk Chunk `gorm:"foreignKey:ChunkID" json:"chunk,omitempty"`
}

// TableName specifies the table name for Embedding
func (Embedding) TableName() string {
	return "embeddings"
}

// GetEmbeddingColumnSQL returns the SQL for creating the embedding column with specified dimensions
func GetEmbeddingColumnSQL(dimensions int) string {
	return fmt.Sprintf("VECTOR(%d)", dimensions)
}

// DimensionMetadata stores the configured embedding dimension in the database
type DimensionMetadata struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	Key       string    `gorm:"type:varchar(100);not null;uniqueIndex" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName specifies the table name for DimensionMetadata
func (DimensionMetadata) TableName() string {
	return "dimension_metadata"
}

const (
	// DimensionMetadataKey is the key for storing embedding dimension
	DimensionMetadataKey = "embedding_dimension"
)
