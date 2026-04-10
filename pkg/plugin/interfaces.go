package plugin

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Common errors
var (
	ErrUnsupportedFormat = errors.New("unsupported file format")
	ErrParserNotFound    = errors.New("parser not found")
	ErrEmbedderNotFound  = errors.New("embedder not found")
)

// ParseOptions contains options for document parsing
type ParseOptions struct {
	Filename         string
	ContentType      string
	Metadata         map[string]any
	RenderJavaScript bool
	RenderTimeout    int
	WaitSelector     string
}

// DocumentStructure represents the structure of a document
type DocumentStructure struct {
	Sections []Section `json:"sections"`
	Elements []Element `json:"elements"`
}

// Section represents a document section
type Section struct {
	Title    string    `json:"title"`
	Level    int       `json:"level"`
	Offset   int       `json:"offset"`
	Children []Section `json:"children,omitempty"`
}

// Element represents a document element (paragraph, table, image, etc.)
type Element struct {
	Type     string         `json:"type"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ParsedDocument represents the result of parsing a document
type ParsedDocument struct {
	Content   string             `json:"content"`
	Metadata  map[string]any     `json:"metadata"`
	Structure *DocumentStructure `json:"structure,omitempty"`
}

// DocumentParser defines the interface for document parsers
type DocumentParser interface {
	// Name returns the parser name
	Name() string

	// SupportedExtensions returns the file extensions this parser supports
	SupportedExtensions() []string

	// SupportedMimeTypes returns the MIME types this parser supports
	SupportedMimeTypes() []string

	// Parse parses the document from the given reader
	Parse(ctx context.Context, source io.Reader, opts ParseOptions) (*ParsedDocument, error)
}

// LocationInfo represents the location of a chunk in the source document
type LocationInfo struct {
	Type      string `json:"type"`    // file, notion, webpage, etc.
	URI       string `json:"uri"`     // Source address
	Section   string `json:"section"` // Section heading
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Offset    int    `json:"offset"`
}

// Chunk represents a text segment from a document
type Chunk struct {
	Content  string         `json:"content"`
	Location LocationInfo   `json:"location"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Chunker defines the interface for text chunking strategies
type Chunker interface {
	// Name returns the chunker name
	Name() string

	// Split splits the parsed document into chunks
	Split(ctx context.Context, doc *ParsedDocument) ([]Chunk, error)
}

// Embedder defines the interface for embedding services
type Embedder interface {
	// Name returns the embedder name
	Name() string

	// Embed generates embeddings for the given texts
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimension of the embeddings
	Dimensions() int

	// Model returns the model name used for embeddings
	Model() string
}

// SearchOptions contains options for searching
type SearchOptions struct {
	NamespaceIDs []string       `json:"namespace_ids"`
	TopK         int            `json:"top_k"`
	MinScore     float32        `json:"min_score"`
	HybridWeight float64        `json:"hybrid_weight"` // Weight for vector vs full-text search
	Filters      map[string]any `json:"filters,omitempty"`
}

// SearchResult represents a search result
type SearchResult struct {
	Chunk     Chunk     `json:"chunk"`
	Score     float32   `json:"score"`
	Embedding []float32 `json:"embedding,omitempty"`
}

// Retriever defines the interface for document retrieval
type Retriever interface {
	// Search performs a search with the given query and options
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}

// PluginRegistry manages registered plugins
type PluginRegistry struct {
	parsers   map[string]DocumentParser
	chunkers  map[string]Chunker
	embedders map[string]Embedder
	mu        sync.RWMutex
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		parsers:   make(map[string]DocumentParser),
		chunkers:  make(map[string]Chunker),
		embedders: make(map[string]Embedder),
	}
}

// RegisterParser registers a document parser
func (r *PluginRegistry) RegisterParser(parser DocumentParser) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.parsers[parser.Name()] = parser
}

// GetParser returns a parser by name
func (r *PluginRegistry) GetParser(name string) (DocumentParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parser, ok := r.parsers[name]
	if !ok {
		return nil, ErrParserNotFound
	}
	return parser, nil
}

// GetParserForFile returns a parser that supports the given file
func (r *PluginRegistry) GetParserForFile(filename string) (DocumentParser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, parser := range r.parsers {
		for _, ext := range parser.SupportedExtensions() {
			if hasSuffix(filename, ext) {
				return parser, nil
			}
		}
	}
	return nil, ErrUnsupportedFormat
}

// RegisterChunker registers a chunker
func (r *PluginRegistry) RegisterChunker(chunker Chunker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunkers[chunker.Name()] = chunker
}

// GetChunker returns a chunker by name
func (r *PluginRegistry) GetChunker(name string) (Chunker, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	chunker, ok := r.chunkers[name]
	if !ok {
		return nil, errors.New("chunker not found")
	}
	return chunker, nil
}

// RegisterEmbedder registers an embedder
func (r *PluginRegistry) RegisterEmbedder(embedder Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedders[embedder.Name()] = embedder
}

// GetEmbedder returns an embedder by name
func (r *PluginRegistry) GetEmbedder(name string) (Embedder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	embedder, ok := r.embedders[name]
	if !ok {
		return nil, ErrEmbedderNotFound
	}
	return embedder, nil
}

// ListParsers returns all registered parser names
func (r *PluginRegistry) ListParsers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.parsers))
	for name := range r.parsers {
		names = append(names, name)
	}
	return names
}

// ListChunkers returns all registered chunker names
func (r *PluginRegistry) ListChunkers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.chunkers))
	for name := range r.chunkers {
		names = append(names, name)
	}
	return names
}

// ListEmbedders returns all registered embedder names
func (r *PluginRegistry) ListEmbedders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.embedders))
	for name := range r.embedders {
		names = append(names, name)
	}
	return names
}

// hasSuffix checks if s ends with suffix (case-insensitive)
func hasSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix ||
		toLower(s[len(s)-len(suffix):]) == toLower(suffix)
}

// toLower converts a string to lowercase
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}
