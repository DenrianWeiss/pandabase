package chunker

import (
	"context"
	"strings"
	"unicode/utf8"

	"pandabase/pkg/plugin"
)

// LineBasedChunker splits text by lines with max chunk size
type LineBasedChunker struct {
	maxChunkSize int
	maxLines     int
}

// NewLineBasedChunker creates a new line-based chunker
func NewLineBasedChunker(maxChunkSize, maxLines int) *LineBasedChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 1000
	}
	if maxLines <= 0 {
		maxLines = 50
	}
	return &LineBasedChunker{
		maxChunkSize: maxChunkSize,
		maxLines:     maxLines,
	}
}

// Name returns the chunker name
func (c *LineBasedChunker) Name() string {
	return "line_based"
}

// Split splits the document into chunks based on lines
func (c *LineBasedChunker) Split(ctx context.Context, doc *plugin.ParsedDocument) ([]plugin.Chunk, error) {
	content := doc.Content
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	lines := strings.Split(content, "\n")
	var chunks []plugin.Chunk
	var currentChunk strings.Builder
	var currentLines []string
	var currentOffset int
	chunkIndex := 0

	for i, line := range lines {
		// Check if adding this line would exceed limits
		wouldExceedSize := currentChunk.Len()+len(line)+1 > c.maxChunkSize
		wouldExceedLines := len(currentLines) >= c.maxLines

		if wouldExceedSize || wouldExceedLines {
			// Save current chunk
			if currentChunk.Len() > 0 {
				chunks = append(chunks, plugin.Chunk{
					Content: strings.TrimSpace(currentChunk.String()),
					Location: plugin.LocationInfo{
						Type:      "text",
						LineStart: currentOffset,
						LineEnd:   i,
					},
					Metadata: map[string]any{
						"chunk_index": chunkIndex,
						"line_count":  len(currentLines),
					},
				})
				chunkIndex++
			}
			// Start new chunk
			currentChunk.Reset()
			currentLines = nil
			currentOffset = i
		}

		// Add line to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n")
		}
		currentChunk.WriteString(line)
		currentLines = append(currentLines, line)
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, plugin.Chunk{
			Content: strings.TrimSpace(currentChunk.String()),
			Location: plugin.LocationInfo{
				Type:      "text",
				LineStart: currentOffset,
				LineEnd:   len(lines),
			},
			Metadata: map[string]any{
				"chunk_index": chunkIndex,
				"line_count":  len(currentLines),
			},
		})
	}

	return chunks, nil
}

// StructuredChunker splits text by document structure (headings, sections)
type StructuredChunker struct {
	maxChunkSize int
}

// NewStructuredChunker creates a new structured chunker
func NewStructuredChunker(maxChunkSize int) *StructuredChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &StructuredChunker{maxChunkSize: maxChunkSize}
}

// Name returns the chunker name
func (c *StructuredChunker) Name() string {
	return "structured"
}

// Split splits the document into chunks based on structure
func (c *StructuredChunker) Split(ctx context.Context, doc *plugin.ParsedDocument) ([]plugin.Chunk, error) {
	if doc.Structure == nil || len(doc.Structure.Elements) == 0 {
		// Fall back to line-based chunking
		lineChunker := NewLineBasedChunker(c.maxChunkSize, 50)
		return lineChunker.Split(ctx, doc)
	}

	var chunks []plugin.Chunk
	var currentChunk strings.Builder
	var currentSection string
	var chunkStartLine int
	chunkIndex := 0

	for _, elem := range doc.Structure.Elements {
		elemText := elem.Content
		if strings.TrimSpace(elemText) == "" {
			continue
		}

		// Check if this is a new section (from metadata or element type)
		isNewSection := elem.Type == "heading" ||
			(currentChunk.Len() > 0 && currentChunk.Len()+len(elemText) > c.maxChunkSize)

		if isNewSection && currentChunk.Len() > 0 {
			// Save current chunk
			chunks = append(chunks, plugin.Chunk{
				Content: strings.TrimSpace(currentChunk.String()),
				Location: plugin.LocationInfo{
					Type:    "text",
					Section: currentSection,
				},
				Metadata: map[string]any{
					"chunk_index": chunkIndex,
				},
			})
			currentChunk.Reset()
			chunkIndex++
		}

		// Track section title
		if elem.Type == "heading" {
			currentSection = elemText
		}

		// Add element to current chunk
		if currentChunk.Len() == 0 {
			if startLine, ok := elem.Metadata["start_line"].(int); ok {
				chunkStartLine = startLine
			}
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(elemText)
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, plugin.Chunk{
			Content: strings.TrimSpace(currentChunk.String()),
			Location: plugin.LocationInfo{
				Type:    "text",
				Section: currentSection,
			},
			Metadata: map[string]any{
				"chunk_index": chunkIndex,
				"start_line":  chunkStartLine,
			},
		})
	}

	return chunks, nil
}

// MarkdownChunker splits markdown by headings (respecting document structure)
type MarkdownChunker struct {
	maxChunkSize int
}

// NewMarkdownChunker creates a new markdown chunker
func NewMarkdownChunker(maxChunkSize int) *MarkdownChunker {
	if maxChunkSize <= 0 {
		maxChunkSize = 2000
	}
	return &MarkdownChunker{maxChunkSize: maxChunkSize}
}

// Name returns the chunker name
func (c *MarkdownChunker) Name() string {
	return "markdown"
}

// Split splits markdown document by sections
func (c *MarkdownChunker) Split(ctx context.Context, doc *plugin.ParsedDocument) ([]plugin.Chunk, error) {
	content := doc.Content
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}

	// If we have structure with sections, use them
	if doc.Structure != nil && len(doc.Structure.Sections) > 0 {
		return c.splitBySections(doc)
	}

	// Fall back to line-based chunking
	lineChunker := NewLineBasedChunker(c.maxChunkSize, 50)
	return lineChunker.Split(ctx, doc)
}

// splitBySections splits by markdown sections (headings)
func (c *MarkdownChunker) splitBySections(doc *plugin.ParsedDocument) ([]plugin.Chunk, error) {
	lines := strings.Split(doc.Content, "\n")
	var chunks []plugin.Chunk
	var currentChunk strings.Builder
	var currentHeading string
	var currentLevel int
	var chunkStartLine int
	chunkIndex := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is a heading
		if isHeading, level, title := parseHeading(trimmed); isHeading {
			// Save previous chunk if exists
			if currentChunk.Len() > 0 {
				chunks = append(chunks, plugin.Chunk{
					Content: strings.TrimSpace(currentChunk.String()),
					Location: plugin.LocationInfo{
						Type:      "markdown",
						Section:   currentHeading,
						LineStart: chunkStartLine,
						LineEnd:   i,
					},
					Metadata: map[string]any{
						"chunk_index":   chunkIndex,
						"heading_level": currentLevel,
					},
				})
				currentChunk.Reset()
				chunkIndex++
			}
			// Start new section
			currentHeading = title
			currentLevel = level
			chunkStartLine = i
		}

		// Add line to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n")
		}
		currentChunk.WriteString(line)

		// Check if chunk is getting too large
		if currentChunk.Len() > c.maxChunkSize && currentLevel > 1 {
			// Split at this point, but keep the same section context
			chunks = append(chunks, plugin.Chunk{
				Content: strings.TrimSpace(currentChunk.String()),
				Location: plugin.LocationInfo{
					Type:      "markdown",
					Section:   currentHeading,
					LineStart: chunkStartLine,
					LineEnd:   i + 1,
				},
				Metadata: map[string]any{
					"chunk_index":   chunkIndex,
					"heading_level": currentLevel,
					"continued":     true,
				},
			})
			currentChunk.Reset()
			chunkIndex++
			chunkStartLine = i + 1
		}
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunks = append(chunks, plugin.Chunk{
			Content: strings.TrimSpace(currentChunk.String()),
			Location: plugin.LocationInfo{
				Type:      "markdown",
				Section:   currentHeading,
				LineStart: chunkStartLine,
				LineEnd:   len(lines),
			},
			Metadata: map[string]any{
				"chunk_index":   chunkIndex,
				"heading_level": currentLevel,
			},
		})
	}

	return chunks, nil
}

// parseHeading parses a markdown heading line
func parseHeading(line string) (isHeading bool, level int, title string) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return false, 0, ""
	}

	level = 0
	for i := 0; i < len(line) && line[i] == '#'; i++ {
		level++
	}

	if level > 6 {
		return false, 0, ""
	}

	// Must have a space after #
	if level >= len(line) || line[level] != ' ' {
		return false, 0, ""
	}

	title = strings.TrimSpace(line[level:])
	return true, level, title
}

// TokenCount returns an approximate token count for a string
// This is a simple estimation (1 token ≈ 4 characters for English text)
func TokenCount(text string) int {
	return utf8.RuneCountInString(text) / 4
}
