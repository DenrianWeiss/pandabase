package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"pandabase/internal/config"
)

// Storage defines the interface for file storage
type Storage interface {
	// Save stores a file and returns the file path/ID
	Save(ctx context.Context, filename string, content io.Reader) (string, error)
	// Get retrieves a file by path/ID
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	// Delete removes a file
	Delete(ctx context.Context, path string) error
	// Exists checks if a file exists
	Exists(ctx context.Context, path string) (bool, error)
	// GetURL returns a URL for accessing the file (if applicable)
	GetURL(ctx context.Context, path string) (string, error)
}

// FileSystemStorage implements Storage using local filesystem
type FileSystemStorage struct {
	dataPath    string
	maxFileSize int64
}

// NewFileSystemStorage creates a new filesystem storage
func NewFileSystemStorage(cfg *config.StorageConfig) (*FileSystemStorage, error) {
	dataPath := cfg.DataPath
	if dataPath == "" {
		dataPath = "./data/files"
	}

	// Ensure absolute path
	if !filepath.IsAbs(dataPath) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
		dataPath = filepath.Join(wd, dataPath)
	}

	// Create data directory if not exists
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	maxFileSize := cfg.MaxFileSize
	if maxFileSize <= 0 {
		maxFileSize = 100 // 100 MB default
	}

	return &FileSystemStorage{
		dataPath:    dataPath,
		maxFileSize: maxFileSize * 1024 * 1024, // Convert MB to bytes
	}, nil
}

// Save stores a file on the filesystem
func (s *FileSystemStorage) Save(ctx context.Context, filename string, content io.Reader) (string, error) {
	// Generate unique filename to avoid collisions
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filepath.Base(filename), ext)
	uniqueName := fmt.Sprintf("%s_%s%s", base, uuid.New().String(), ext)
	
	// Organize files in subdirectories by date
	now := time.Now()
	subDir := filepath.Join(
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", now.Month()),
	)
	
	dirPath := filepath.Join(s.dataPath, subDir)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	filePath := filepath.Join(subDir, uniqueName)
	fullPath := filepath.Join(s.dataPath, filePath)

	// Create file with size limit
	file, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy with size limit
	limitedReader := io.LimitReader(content, s.maxFileSize+1)
	n, err := io.Copy(file, limitedReader)
	if err != nil {
		os.Remove(fullPath)
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if n > s.maxFileSize {
		os.Remove(fullPath)
		return "", fmt.Errorf("file exceeds maximum size of %d MB", s.maxFileSize/(1024*1024))
	}

	return filePath, nil
}

// Get retrieves a file from the filesystem
func (s *FileSystemStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	// Security: prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return nil, fmt.Errorf("invalid path: directory traversal not allowed")
	}

	fullPath := filepath.Join(s.dataPath, cleanPath)
	
	// Ensure the path is within data directory
	if !strings.HasPrefix(fullPath, s.dataPath) {
		return nil, fmt.Errorf("invalid path: access denied")
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// Delete removes a file from the filesystem
func (s *FileSystemStorage) Delete(ctx context.Context, path string) error {
	// Security: prevent directory traversal
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf("invalid path: directory traversal not allowed")
	}

	fullPath := filepath.Join(s.dataPath, cleanPath)
	
	// Ensure the path is within data directory
	if !strings.HasPrefix(fullPath, s.dataPath) {
		return fmt.Errorf("invalid path: access denied")
	}

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", path)
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// Exists checks if a file exists
func (s *FileSystemStorage) Exists(ctx context.Context, path string) (bool, error) {
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return false, fmt.Errorf("invalid path: directory traversal not allowed")
	}

	fullPath := filepath.Join(s.dataPath, cleanPath)
	
	if !strings.HasPrefix(fullPath, s.dataPath) {
		return false, fmt.Errorf("invalid path: access denied")
	}

	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// GetURL returns a local file path as URL
func (s *FileSystemStorage) GetURL(ctx context.Context, path string) (string, error) {
	return fmt.Sprintf("file://%s", filepath.Join(s.dataPath, path)), nil
}

// MemoryStorage implements in-memory storage for testing
type MemoryStorage struct {
	files map[string][]byte
}

// NewMemoryStorage creates a new in-memory storage
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		files: make(map[string][]byte),
	}
}

// Save stores a file in memory
func (s *MemoryStorage) Save(ctx context.Context, filename string, content io.Reader) (string, error) {
	data, err := io.ReadAll(content)
	if err != nil {
		return "", err
	}

	path := fmt.Sprintf("memory://%s_%s", uuid.New().String(), filename)
	s.files[path] = data
	return path, nil
}

// Get retrieves a file from memory
func (s *MemoryStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	data, ok := s.files[path]
	if !ok {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return io.NopCloser(strings.NewReader(string(data))), nil
}

// Delete removes a file from memory
func (s *MemoryStorage) Delete(ctx context.Context, path string) error {
	delete(s.files, path)
	return nil
}

// Exists checks if a file exists in memory
func (s *MemoryStorage) Exists(ctx context.Context, path string) (bool, error) {
	_, ok := s.files[path]
	return ok, nil
}

// GetURL returns the memory path
func (s *MemoryStorage) GetURL(ctx context.Context, path string) (string, error) {
	return path, nil
}

// NewStorage creates a storage based on configuration
func NewStorage(cfg *config.StorageConfig) (Storage, error) {
	switch cfg.Type {
	case "filesystem", "":
		return NewFileSystemStorage(cfg)
	case "memory":
		return NewMemoryStorage(), nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
