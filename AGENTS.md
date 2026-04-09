# AGENTS.md - Pandabase RAG Knowledge Base System

## Project Overview

Pandabase is a **document retrieval system** built in Go that uses PostgreSQL with pgvector for vector storage. It's designed as a RAG (Retrieval-Augmented Generation) knowledge base with support for multiple document formats, incremental updates, and namespace-based access control.

**Key Features:**
- Multi-format document ingestion (Markdown, PDF, Notion, web pages, images via OCR)
- Vector similarity search with pgvector
- Incremental document updates (detects changes via content hash)
- Namespace-based multi-tenancy with access control
- Plugin architecture for extensible parsers
- Web UI for management and API for integrations
- MCP (Model Context Protocol) Skill API for AI modules

## Project Status

⚠️ **This is a new project in early development.** Currently only the design plan exists (`PLAN.md`). No source code has been implemented yet.

## Tech Stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.24+ |
| Web Framework | Gin |
| ORM | GORM with pgvector-go |
| Database | PostgreSQL 15+ with pgvector extension |
| Cache/Queue | Redis + Asynq |
| File Storage | Local filesystem |
| Frontend | Vue 3 + TypeScript |
| Logging | Logrus |
| Observability | OpenTelemetry |

## Project Structure (Planned)

```
/Users/admin/Workspace/pandabase/
├── cmd/server/                    # Application entry points
├── internal/                      # Private application code
│   ├── config/                    # Configuration management (Viper)
│   ├── db/                        # Database models and migrations
│   │   ├── db.go                  # Database connection with dimension validation
│   │   └── models/models.go       # GORM models
│   ├── logger/logger.go           # Structured logging (Logrus)
│   ├── parser/                    # Document parsers
│   │   ├── text.go                # Plain text parser
│   │   ├── markdown.go            # Markdown parser (goldmark)
│   │   └── parser_test.go         # Parser tests
│   ├── chunker/                   # Text chunking strategies
│   │   ├── chunker.go             # Line-based, Markdown, Structured chunkers
│   │   └── chunker_test.go        # Chunker tests
│   ├── storage/                   # File storage
│   │   ├── storage.go             # Filesystem and memory storage
│   │   └── storage_test.go        # Storage tests
│   ├── api/                       # HTTP handlers (Gin)
│   ├── retriever/                 # Search and retrieval logic
│   └── worker/                    # Background job processors
├── pkg/plugin/interfaces.go       # Plugin interfaces
├── web/                           # Vue 3 frontend
├── docker-compose.yml             # PostgreSQL + Redis
├── config.example.yaml            # Configuration example
└── .env.example                   # Environment variables example
```

## Essential Commands (To Be Implemented)

### Development
```bash
# Run the server
go run ./cmd/server

# Run tests
go test ./...

# Build binary
go build -o pandabase ./cmd/server

# Run with config
./pandabase -config config.yaml
```

### Database
```bash
# Start local PostgreSQL + Redis
docker-compose up -d

# Run migrations (programmatic, with dimension validation)
go run ./cmd/server

# Reset database
docker-compose down -v && docker-compose up -d
```

### Frontend
```bash
cd web/
npm install
npm run dev      # Development server
npm run build    # Production build
```

## Key Design Patterns

### 1. Plugin Architecture for Document Parsers

All document parsers implement the `DocumentParser` interface:

```go
type DocumentParser interface {
    Name() string
    SupportedExtensions() []string
    SupportedMimeTypes() []string
    Parse(ctx context.Context, source io.Reader, opts ParseOptions) (*ParsedDocument, error)
}
```

Parsers are registered in a `PluginRegistry` and selected by file extension/MIME type.

### 2. Incremental Update Detection

Documents are tracked by SHA256 content hash. When re-uploaded:
- Hash match → No action needed
- Hash mismatch → Soft delete old chunks/embeddings, re-index
- Missing → New document, full indexing

### 3. Namespace-Based Multi-Tenancy

All data is scoped to a `namespace_id`. PostgreSQL RLS (Row Level Security) policies enforce access control at the database level.

### 4. Location-Aware Chunking

Each chunk stores `LocationInfo` for traceability:

```go
type LocationInfo struct {
    Type      string `json:"type"`      // file/notion/webpage
    URI       string `json:"uri"`       // Source address
    Section   string `json:"section"`   // Section heading
    LineStart int    `json:"line_start"`
    LineEnd   int    `json:"line_end"`
    Offset    int    `json:"offset"`
}
```

## Database Schema

See `PLAN.md` for complete schema. Key tables:

- `namespaces` - Multi-tenant isolation root
- `documents` - Source documents with content hash
- `chunks` - Text segments with location metadata
- `embeddings` - Vector representations (pgvector VECTOR(1536))

Indexes:
- HNSW index on embeddings for fast ANN search
- GIN index on chunks for full-text search

## Configuration

### Embedding Dimensions (Important!)

The embedding dimension is configured at startup and **cannot be changed** without creating a new database:

```yaml
embedding:
  dimensions: 1536  # Supported: 384, 768, 1024, 1536, 3072
```

If the configured dimension doesn't match the database, the application will exit with an error.

### File Storage

Files are stored on the local filesystem (configurable):

```yaml
storage:
  type: filesystem        # or "memory" for testing
  data_path: ./data/files
  max_file_size: 100      # MB
```

Per `PLAN.md`, development follows this sequence:

1. **Week 1-2**: Project setup, database, config, logging
2. **Week 3-4**: Document parsers, chunkers, embedders
3. **Week 5**: Vector storage and retrieval
4. **Week 6**: Document lifecycle (ingest, update, delete)
5. **Week 7-8**: External integrations (PDF, Notion, web)
6. **Week 9-10**: REST API and web UI
7. **Week 11**: Advanced plugins (OCR, Vision)
8. **Week 12**: Testing and deployment

## Recommended Libraries

| Purpose | Library |
|---------|---------|
| Document parsing | extractous-go, kreuzberg |
| Markdown parsing | goldmark |
| Web scraping | go-readability, go-rod |
| Notion API | jomei/notionapi |
| OpenAI API | sashabaranov/go-openai |
| Vector DB | pgvector/pgvector-go |

## Gotchas & Notes

1. **pgvector required**: PostgreSQL must have the pgvector extension installed
2. **Embedding dimensions**: Default is 1536 (OpenAI ada-002). Update if using different models.
3. **RLS policies**: Must be configured for proper namespace isolation
4. **Async processing**: Document ingestion uses Asynq for background processing
5. **Content hash**: SHA256 is used for change detection, not for security

## Next Steps for Development

1. Initialize project structure (`internal/`, `cmd/`, `pkg/`)
2. Set up Docker Compose for PostgreSQL + pgvector, Redis, MinIO
3. Create database migrations for core tables
4. Implement configuration system with Viper
5. Add structured logging with Logrus
6. Create base interfaces (DocumentParser, Chunker, Embedder)

## References

- `PLAN.md` - Complete design specification (in Chinese)
- `go.mod` - Go module definition
