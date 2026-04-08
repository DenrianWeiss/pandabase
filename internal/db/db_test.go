package db

import (
	"testing"

	"pandabase/internal/config"
	"pandabase/internal/db/models"
)

func TestNew(t *testing.T) {
	// This test requires a running PostgreSQL database
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     "5432",
		User:     "pandabase",
		Password: "pandabase",
		Name:     "pandabase_test",
		SSLMode:  "disable",
		LogLevel: "error",
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database connection: %v", err)
	}
	defer db.Close()

	// Test Initialize with dimension
	dimensions := 768
	if err := db.Initialize(dimensions); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	// Verify dimension is stored
	if db.GetDimensions() != dimensions {
		t.Errorf("GetDimensions() = %d, want %d", db.GetDimensions(), dimensions)
	}
}

func TestValidateAndSetDimension(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &config.DatabaseConfig{
		Host:     "localhost",
		Port:     "5432",
		User:     "pandabase",
		Password: "pandabase",
		Name:     "pandabase_test_dim",
		SSLMode:  "disable",
	}

	// First connection - set dimension
	db1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create first connection: %v", err)
	}

	// Clean up test database
	db1.Exec("DROP TABLE IF EXISTS dimension_metadata")
	db1.Exec("DROP TABLE IF EXISTS embeddings")
	db1.Exec("DROP TABLE IF EXISTS chunks")
	db1.Exec("DROP TABLE IF EXISTS documents")
	db1.Exec("DROP TABLE IF EXISTS namespaces")

	if err := db1.Initialize(768); err != nil {
		db1.Close()
		t.Fatalf("Failed to initialize with dimension 768: %v", err)
	}
	db1.Close()

	// Second connection - same dimension should work
	db2, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create second connection: %v", err)
	}
	if err := db2.Initialize(768); err != nil {
		db2.Close()
		t.Fatalf("Failed to initialize with same dimension: %v", err)
	}
	db2.Close()

	// Third connection - different dimension should fail
	db3, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create third connection: %v", err)
	}
	defer db3.Close()

	if err := db3.Initialize(1536); err == nil {
		t.Error("Expected error when using different dimension, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}

func TestGetEmbeddingColumnSQL(t *testing.T) {
	tests := []struct {
		dimensions int
		expected   string
	}{
		{768, "VECTOR(768)"},
		{1536, "VECTOR(1536)"},
		{1024, "VECTOR(1024)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := models.GetEmbeddingColumnSQL(tt.dimensions)
			if got != tt.expected {
				t.Errorf("GetEmbeddingColumnSQL(%d) = %s, want %s", tt.dimensions, got, tt.expected)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
	}{
		{
			name: "valid config with 768 dimensions",
			cfg: &config.Config{
				Embedding: config.EmbeddingConfig{
					Dimensions: 768,
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with 1536 dimensions",
			cfg: &config.Config{
				Embedding: config.EmbeddingConfig{
					Dimensions: 1536,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid config with zero dimensions",
			cfg: &config.Config{
				Embedding: config.EmbeddingConfig{
					Dimensions: 0,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid config with negative dimensions",
			cfg: &config.Config{
				Embedding: config.EmbeddingConfig{
					Dimensions: -1,
				},
			},
			wantErr: true,
		},
		{
			name: "invalid config with too large dimensions",
			cfg: &config.Config{
				Embedding: config.EmbeddingConfig{
					Dimensions: 10000,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
