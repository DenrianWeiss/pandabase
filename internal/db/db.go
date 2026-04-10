package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"pandabase/internal/config"
	"pandabase/internal/db/models"

	"github.com/sirupsen/logrus"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	dimensions    int
	ftsDictionary string
	useHalfVec    bool
	logger        *logrus.Logger
}

// New creates a new database connection
func New(cfg *config.DatabaseConfig, appLogger *logrus.Logger) (*DB, error) {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.Host, cfg.User, cfg.Password, cfg.Name, cfg.Port, cfg.SSLMode,
	)

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	if cfg.LogLevel == "debug" {
		gormConfig.Logger = logger.Default.LogMode(logger.Info)
	}

	db, err := gorm.Open(postgres.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	ftsDict := cfg.FTSDictionary
	if ftsDict == "" {
		ftsDict = "simple"
	}

	return &DB{DB: db, ftsDictionary: ftsDict, useHalfVec: cfg.UseHalfVec, logger: appLogger}, nil
}

// Initialize sets up the database with the configured embedding dimension
// This must be called after New() and before using the database
func (db *DB) Initialize(dimensions int) error {
	db.dimensions = dimensions

	// Check for index support and warnings
	if dimensions > 2000 && dimensions <= 4000 {
		if !db.useHalfVec {
			db.logger.Warnf("Embedding dimension is %d (> 2000). To enable HNSW indexing, please configure 'database.use_halfvec: true' to use half-precision vectors.", dimensions)
		}
	} else if dimensions > 4000 {
		db.logger.Warnf("Embedding dimension is %d (> 4000). pgvector does not support ANN indexing for dimensions this large. Search will use sequential scans.", dimensions)
	}

	// Enable pgvector extension
	if err := db.enablePgvector(); err != nil {
		return fmt.Errorf("failed to enable pgvector extension: %w", err)
	}

	// Check and validate embedding dimension
	if err := db.validateAndSetDimension(dimensions); err != nil {
		return fmt.Errorf("dimension validation failed: %w", err)
	}

	// Run migrations
	if err := db.migrate(); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// enablePgvector enables the pgvector extension
func (db *DB) enablePgvector() error {
	return db.Exec("CREATE EXTENSION IF NOT EXISTS vector").Error
}

// validateAndSetDimension checks if the configured dimension matches the database
// If no dimension is set, it sets the current one
// If a different dimension is set, it returns an error
func (db *DB) validateAndSetDimension(dimensions int) error {
	// Create dimension_metadata table if not exists
	if err := db.AutoMigrate(&models.DimensionMetadata{}); err != nil {
		return fmt.Errorf("failed to create dimension_metadata table: %w", err)
	}

	// Check existing dimension
	var metadata models.DimensionMetadata
	result := db.Where("key = ?", models.DimensionMetadataKey).First(&metadata)

	if result.Error == gorm.ErrRecordNotFound {
		// No dimension set yet, store the current one
		metadata = models.DimensionMetadata{
			Key:   models.DimensionMetadataKey,
			Value: strconv.Itoa(dimensions),
		}
		if err := db.Create(&metadata).Error; err != nil {
			return fmt.Errorf("failed to store embedding dimension: %w", err)
		}
		return nil
	}

	if result.Error != nil {
		return fmt.Errorf("failed to query dimension metadata: %w", result.Error)
	}

	// Dimension already exists, validate it matches
	storedDim, err := strconv.Atoi(metadata.Value)
	if err != nil {
		return fmt.Errorf("invalid stored dimension value: %s", metadata.Value)
	}

	if storedDim != dimensions {
		return fmt.Errorf(
			"embedding dimension mismatch: database is configured for %d dimensions, but application is configured for %d dimensions. "+
				"Changing embedding dimensions at runtime is not supported. "+
				"Please either use the same dimension or create a new database.",
			storedDim, dimensions,
		)
	}

	return nil
}

// migrate runs database migrations with dynamic vector column
func (db *DB) migrate() error {
	// Migrate base tables (order matters due to foreign keys)
	if err := db.AutoMigrate(
		&models.User{},
		&models.APIToken{},
		&models.Namespace{},
		&models.NamespaceMember{},
		&models.Document{},
		&models.Chunk{},
	); err != nil {
		return fmt.Errorf("failed to migrate base tables: %w", err)
	}

	// Create embeddings table with dynamic vector column
	if err := db.createEmbeddingsTable(); err != nil {
		return fmt.Errorf("failed to create embeddings table: %w", err)
	}

	// Create indexes
	if err := db.createIndexes(); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	return nil
}

// createEmbeddingsTable creates the embeddings table with the configured vector dimension
func (db *DB) createEmbeddingsTable() error {
	// Check if table exists
	var count int64
	db.Raw(`
		SELECT COUNT(*) FROM information_schema.tables 
		WHERE table_schema = 'public' AND table_name = 'embeddings'
	`).Scan(&count)

	if count == 0 {
		// Table doesn't exist, create it with the correct dimension
		vectorType := models.GetEmbeddingColumnSQL(db.dimensions, db.useHalfVec)
		createSQL := fmt.Sprintf(`
			CREATE TABLE embeddings (
				id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
				chunk_id UUID NOT NULL UNIQUE REFERENCES chunks(id) ON DELETE CASCADE,
				embedding %s NOT NULL,
				model VARCHAR(100) NOT NULL,
				created_at TIMESTAMP DEFAULT NOW()
			)
		`, vectorType)

		if err := db.Exec(createSQL).Error; err != nil {
			return fmt.Errorf("failed to create embeddings table: %w", err)
		}
	} else {
		// Table exists, verify the vector column has the correct dimension
		if err := db.verifyVectorDimension(); err != nil {
			return err
		}
	}

	return nil
}

// verifyVectorDimension checks if the existing vector column has the correct dimension
func (db *DB) verifyVectorDimension() error {
	// Query the actual dimension of the vector column
	var actualDim int
	result := db.Raw(`
		SELECT atttypmod 
		FROM pg_attribute 
		WHERE attrelid = 'embeddings'::regclass 
		AND attname = 'embedding'
	`).Scan(&actualDim)

	if result.Error != nil {
		// Column might not exist yet, that's ok
		return nil
	}

	if actualDim != db.dimensions {
		return fmt.Errorf(
			"embeddings table has vector dimension %d, but application is configured for %d. "+
				"Vector dimensions cannot be changed at runtime.",
			actualDim, db.dimensions,
		)
	}

	return nil
}

// createIndexes creates database indexes
func (db *DB) createIndexes() error {
	dict := db.ftsDictionary
	if dict == "" {
		dict = "simple"
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_documents_namespace_id ON documents(namespace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_content_hash ON documents(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_document_id ON chunks(document_id)`,
		`CREATE INDEX IF NOT EXISTS idx_embeddings_chunk_id ON embeddings(chunk_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_user_id ON api_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_api_tokens_revoked_at ON api_tokens(revoked_at)`,
	}

	// HNSW indexing support:
	// - Standard vector: max 2000 dimensions
	// - Halfvec: max 4000 dimensions (requires pgvector 0.8.0+)
	canIndex := false
	if db.useHalfVec && db.dimensions <= 4000 {
		canIndex = true
	} else if !db.useHalfVec && db.dimensions <= 2000 {
		canIndex = true
	}

	if canIndex {
		opsClass := "vector_cosine_ops"
		if db.useHalfVec {
			opsClass = "halfvec_cosine_ops"
		}
		indexes = append(indexes, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_embeddings_embedding ON embeddings USING hnsw (embedding %s)`, opsClass))
	}

	indexes = append(indexes,
		`DROP INDEX IF EXISTS idx_chunks_content_fts`, // Recreate to ensure correct dictionary
		fmt.Sprintf(`CREATE INDEX idx_chunks_content_fts ON chunks USING GIN (to_tsvector('%s', content))`, dict),
	)

	for _, sql := range indexes {
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("failed to create index: %w (sql: %s)", err, sql)
		}
	}

	return nil
}

// EnableRLS enables Row Level Security on tables
func (db *DB) EnableRLS() error {
	tables := []string{"documents", "chunks", "embeddings"}
	for _, table := range tables {
		sql := fmt.Sprintf("ALTER TABLE %s ENABLE ROW LEVEL SECURITY", table)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("failed to enable RLS on %s: %w", table, err)
		}
	}
	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetDimensions returns the configured embedding dimension
func (db *DB) GetDimensions() int {
	return db.dimensions
}

// GetVectorType returns the SQL type name for embeddings (vector or halfvec)
func (db *DB) GetVectorType() string {
	if db.useHalfVec {
		return "halfvec"
	}
	return "vector"
}

// InsertEmbedding inserts an embedding vector with the correct dimension
func (db *DB) InsertEmbedding(ctx context.Context, chunkID string, model string, vector []float32) error {
	if len(vector) != db.dimensions {
		return fmt.Errorf("vector dimension mismatch: expected %d, got %d", db.dimensions, len(vector))
	}

	// Convert vector to PostgreSQL array format
	vectorStr := "["
	for i, v := range vector {
		if i > 0 {
			vectorStr += ","
		}
		vectorStr += fmt.Sprintf("%f", v)
	}
	vectorStr += "]"

	vectorType := "vector"
	if db.useHalfVec {
		vectorType = "halfvec"
	}

	sql := fmt.Sprintf(`
		INSERT INTO embeddings (chunk_id, embedding, model, created_at)
		VALUES (?, ?::%s, ?, NOW())
	`, vectorType)
	return db.WithContext(ctx).Exec(sql, chunkID, vectorStr, model).Error
}
