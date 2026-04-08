package storage

import (
	"context"
	"io"
	"strings"
	"testing"

	"pandabase/internal/config"
)

func TestFileSystemStorage(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()

	cfg := &config.StorageConfig{
		Type:        "filesystem",
		DataPath:    tempDir,
		MaxFileSize: 10, // 10 MB
	}

	storage, err := NewFileSystemStorage(cfg)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	ctx := context.Background()

	t.Run("Save and Get", func(t *testing.T) {
		content := "Hello, World!"
		reader := strings.NewReader(content)

		path, err := storage.Save(ctx, "test.txt", reader)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		if path == "" {
			t.Error("Save returned empty path")
		}

		// Get the file
		file, err := storage.Get(ctx, path)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if string(data) != content {
			t.Errorf("Content mismatch: got %q, want %q", string(data), content)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		content := "Test content"
		reader := strings.NewReader(content)

		path, err := storage.Save(ctx, "exists_test.txt", reader)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		exists, err := storage.Exists(ctx, path)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("File should exist")
		}

		// Check non-existent file
		exists, err = storage.Exists(ctx, "nonexistent.txt")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if exists {
			t.Error("File should not exist")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		content := "Delete me"
		reader := strings.NewReader(content)

		path, err := storage.Save(ctx, "delete_test.txt", reader)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		// Delete the file
		err = storage.Delete(ctx, path)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Check it no longer exists
		exists, _ := storage.Exists(ctx, path)
		if exists {
			t.Error("File should be deleted")
		}
	})

	t.Run("Path Traversal Protection", func(t *testing.T) {
		// Try to access file outside data directory
		_, err := storage.Get(ctx, "../etc/passwd")
		if err == nil {
			t.Error("Should reject path traversal attempt")
		}

		err = storage.Delete(ctx, "../important.txt")
		if err == nil {
			t.Error("Should reject path traversal attempt")
		}
	})

	t.Run("File Size Limit", func(t *testing.T) {
		// Create content larger than max size (10 MB)
		largeContent := strings.Repeat("x", 11*1024*1024) // 11 MB
		reader := strings.NewReader(largeContent)

		_, err := storage.Save(ctx, "large.txt", reader)
		if err == nil {
			t.Error("Should reject file exceeding max size")
		}
	})
}

func TestMemoryStorage(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	t.Run("Save and Get", func(t *testing.T) {
		content := "Hello, Memory!"
		reader := strings.NewReader(content)

		path, err := storage.Save(ctx, "test.txt", reader)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		file, err := storage.Get(ctx, path)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		defer file.Close()

		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("Read failed: %v", err)
		}

		if string(data) != content {
			t.Errorf("Content mismatch: got %q, want %q", string(data), content)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		content := "Delete me"
		reader := strings.NewReader(content)

		path, err := storage.Save(ctx, "delete.txt", reader)
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}

		err = storage.Delete(ctx, path)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		exists, _ := storage.Exists(ctx, path)
		if exists {
			t.Error("File should be deleted")
		}
	})
}

func TestNewStorage(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.StorageConfig
		wantErr bool
	}{
		{
			name: "filesystem storage",
			cfg: &config.StorageConfig{
				Type:     "filesystem",
				DataPath: t.TempDir(),
			},
			wantErr: false,
		},
		{
			name: "memory storage",
			cfg: &config.StorageConfig{
				Type: "memory",
			},
			wantErr: false,
		},
		{
			name: "default to filesystem",
			cfg: &config.StorageConfig{
				Type:     "",
				DataPath: t.TempDir(),
			},
			wantErr: false,
		},
		{
			name: "unsupported type",
			cfg: &config.StorageConfig{
				Type: "s3",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage, err := NewStorage(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStorage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && storage == nil {
				t.Error("NewStorage() returned nil storage")
			}
		})
	}
}
