package chunker

import (
	"context"
	"strings"
	"testing"

	"pandabase/pkg/plugin"
)

func TestLineBasedChunker(t *testing.T) {
	tests := []struct {
		name         string
		maxChunkSize int
		maxLines     int
		content      string
		wantChunks   int
		checkChunks  func(t *testing.T, chunks []plugin.Chunk)
	}{
		{
			name:         "small content single chunk",
			maxChunkSize: 1000,
			maxLines:     50,
			content:      "Hello, World!",
			wantChunks:   1,
			checkChunks: func(t *testing.T, chunks []plugin.Chunk) {
				if chunks[0].Content != "Hello, World!" {
					t.Errorf("Content = %v, want %v", chunks[0].Content, "Hello, World!")
				}
			},
		},
		{
			name:         "content split by lines",
			maxChunkSize: 1000,
			maxLines:     2,
			content:      "Line 1\nLine 2\nLine 3\nLine 4\nLine 5",
			wantChunks:   3,
			checkChunks: func(t *testing.T, chunks []plugin.Chunk) {
				// Check that chunks have correct line counts
				if chunks[0].Metadata["line_count"] != 2 {
					t.Errorf("First chunk line count = %v, want %v", chunks[0].Metadata["line_count"], 2)
				}
			},
		},
		{
			name:         "content split by size",
			maxChunkSize: 20,
			maxLines:     50,
			content:      "Line 1\nLine 2 is longer\nLine 3\nLine 4 is even longer text",
			wantChunks:   4, // Each long line creates a new chunk
		},
		{
			name:         "empty content",
			maxChunkSize: 100,
			maxLines:     50,
			content:      "",
			wantChunks:   0,
		},
		{
			name:         "whitespace only content",
			maxChunkSize: 100,
			maxLines:     50,
			content:      "   \n\t  \n  ",
			wantChunks:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewLineBasedChunker(tt.maxChunkSize, tt.maxLines)
			doc := &plugin.ParsedDocument{
				Content: tt.content,
			}

			chunks, err := chunker.Split(context.Background(), doc)
			if err != nil {
				t.Errorf("Split() error = %v", err)
				return
			}

			if len(chunks) != tt.wantChunks {
				t.Errorf("Split() returned %d chunks, want %d", len(chunks), tt.wantChunks)
			}

			if tt.checkChunks != nil {
				tt.checkChunks(t, chunks)
			}
		})
	}
}

func TestLineBasedChunker_Name(t *testing.T) {
	chunker := NewLineBasedChunker(100, 10)
	if got := chunker.Name(); got != "line_based" {
		t.Errorf("Name() = %v, want %v", got, "line_based")
	}
}

func TestLineBasedChunker_Defaults(t *testing.T) {
	// Test with zero values
	chunker := NewLineBasedChunker(0, 0)

	if chunker.maxChunkSize != 1000 {
		t.Errorf("Default maxChunkSize = %v, want %v", chunker.maxChunkSize, 1000)
	}
	if chunker.maxLines != 50 {
		t.Errorf("Default maxLines = %v, want %v", chunker.maxLines, 50)
	}
}

func TestMarkdownChunker(t *testing.T) {
	tests := []struct {
		name        string
		maxSize     int
		content     string
		structure   *plugin.DocumentStructure
		wantChunks  int
		checkChunks func(t *testing.T, chunks []plugin.Chunk)
	}{
		{
			name:       "simple markdown",
			maxSize:    1000,
			content:    "# Title\n\nThis is a paragraph.",
			structure:  nil,
			wantChunks: 1,
		},
		{
			name:    "markdown with headings",
			maxSize: 1000,
			content: `# Main Title

## Section 1

Content for section 1.

## Section 2

Content for section 2.`,
			structure: &plugin.DocumentStructure{
				Sections: []plugin.Section{
					{Title: "Main Title", Level: 1},
					{Title: "Section 1", Level: 2},
					{Title: "Section 2", Level: 2},
				},
			},
			wantChunks: 3,
			checkChunks: func(t *testing.T, chunks []plugin.Chunk) {
				// First chunk should be Main Title section
				if chunks[0].Location.Section != "Main Title" {
					t.Errorf("First chunk section = %v, want %v", chunks[0].Location.Section, "Main Title")
				}
			},
		},
		{
			name:       "empty content",
			maxSize:    1000,
			content:    "",
			structure:  nil,
			wantChunks: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewMarkdownChunker(tt.maxSize)
			doc := &plugin.ParsedDocument{
				Content:   tt.content,
				Structure: tt.structure,
			}

			chunks, err := chunker.Split(context.Background(), doc)
			if err != nil {
				t.Errorf("Split() error = %v", err)
				return
			}

			if len(chunks) != tt.wantChunks {
				t.Errorf("Split() returned %d chunks, want %d", len(chunks), tt.wantChunks)
			}

			if tt.checkChunks != nil {
				tt.checkChunks(t, chunks)
			}
		})
	}
}

func TestMarkdownChunker_Name(t *testing.T) {
	chunker := NewMarkdownChunker(1000)
	if got := chunker.Name(); got != "markdown" {
		t.Errorf("Name() = %v, want %v", got, "markdown")
	}
}

func TestMarkdownChunker_Defaults(t *testing.T) {
	chunker := NewMarkdownChunker(0)
	if chunker.maxChunkSize != 2000 {
		t.Errorf("Default maxChunkSize = %v, want %v", chunker.maxChunkSize, 2000)
	}
}

func TestStructuredChunker(t *testing.T) {
	tests := []struct {
		name        string
		maxSize     int
		content     string
		structure   *plugin.DocumentStructure
		wantChunks  int
		checkChunks func(t *testing.T, chunks []plugin.Chunk)
	}{
		{
			name:       "simple paragraph",
			maxSize:    1000,
			content:    "This is a simple paragraph.",
			structure:  nil,
			wantChunks: 1,
		},
		{
			name:    "with document structure",
			maxSize: 1000,
			content: "First paragraph.\n\nSecond paragraph.",
			structure: &plugin.DocumentStructure{
				Elements: []plugin.Element{
					{Type: "paragraph", Content: "First paragraph."},
					{Type: "paragraph", Content: "Second paragraph."},
				},
			},
			wantChunks: 1,
		},
		{
			name:       "empty content",
			maxSize:    1000,
			content:    "",
			structure:  nil,
			wantChunks: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunker := NewStructuredChunker(tt.maxSize)
			doc := &plugin.ParsedDocument{
				Content:   tt.content,
				Structure: tt.structure,
			}

			chunks, err := chunker.Split(context.Background(), doc)
			if err != nil {
				t.Errorf("Split() error = %v", err)
				return
			}

			if len(chunks) != tt.wantChunks {
				t.Errorf("Split() returned %d chunks, want %d", len(chunks), tt.wantChunks)
			}

			if tt.checkChunks != nil {
				tt.checkChunks(t, chunks)
			}
		})
	}
}

func TestStructuredChunker_Name(t *testing.T) {
	chunker := NewStructuredChunker(1000)
	if got := chunker.Name(); got != "structured" {
		t.Errorf("Name() = %v, want %v", got, "structured")
	}
}

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line       string
		isHeading  bool
		level      int
		title      string
	}{
		{"# Title", true, 1, "Title"},
		{"## Section", true, 2, "Section"},
		{"### Deep Section", true, 3, "Deep Section"},
		{"#### Level 4", true, 4, "Level 4"},
		{"##### Level 5", true, 5, "Level 5"},
		{"###### Level 6", true, 6, "Level 6"},
		{"####### Too Deep", false, 0, ""},
		{"Not a heading", false, 0, ""},
		{"#Title", false, 0, ""},
		{"  # Indented Title  ", true, 1, "Indented Title"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			isHeading, level, title := parseHeading(tt.line)
			if isHeading != tt.isHeading {
				t.Errorf("isHeading = %v, want %v", isHeading, tt.isHeading)
			}
			if isHeading {
				if level != tt.level {
					t.Errorf("level = %v, want %v", level, tt.level)
				}
				if title != tt.title {
					t.Errorf("title = %v, want %v", title, tt.title)
				}
			}
		})
	}
}

func TestTokenCount(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello",
			expected: 1,
		},
		{
			name:     "longer text",
			text:     strings.Repeat("a", 400),
			expected: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TokenCount(tt.text)
			if got != tt.expected {
				t.Errorf("TokenCount() = %v, want %v", got, tt.expected)
			}
		})
	}
}
