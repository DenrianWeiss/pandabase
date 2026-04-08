package parser

import (
	"context"
	"strings"
	"testing"

	"pandabase/pkg/plugin"
)

func TestTextParser(t *testing.T) {
	parser := NewTextParser()

	tests := []struct {
		name     string
		content  string
		wantErr  bool
		checkDoc func(t *testing.T, doc *plugin.ParsedDocument)
	}{
		{
			name:    "simple text",
			content: "Hello, World!",
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				if doc.Content != "Hello, World!" {
					t.Errorf("Content = %v, want %v", doc.Content, "Hello, World!")
				}
			},
		},
		{
			name: "multiple paragraphs",
			content: `First paragraph.

Second paragraph.

Third paragraph.`,
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				if len(doc.Structure.Elements) != 3 {
					t.Errorf("Elements count = %v, want %v", len(doc.Structure.Elements), 3)
				}
			},
		},
		{
			name:    "empty content",
			content: "",
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				if doc.Content != "" {
					t.Errorf("Content = %v, want empty", doc.Content)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := strings.NewReader(tt.content)
			opts := plugin.ParseOptions{
				Filename: "test.txt",
			}

			doc, err := parser.Parse(context.Background(), source, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.checkDoc != nil {
				tt.checkDoc(t, doc)
			}
		})
	}
}

func TestTextParser_Name(t *testing.T) {
	parser := NewTextParser()
	if got := parser.Name(); got != "text" {
		t.Errorf("Name() = %v, want %v", got, "text")
	}
}

func TestTextParser_SupportedExtensions(t *testing.T) {
	parser := NewTextParser()
	extensions := parser.SupportedExtensions()
	
	expected := []string{".txt", ".text"}
	if len(extensions) != len(expected) {
		t.Errorf("SupportedExtensions() length = %v, want %v", len(extensions), len(expected))
	}

	for i, ext := range expected {
		if extensions[i] != ext {
			t.Errorf("SupportedExtensions()[%d] = %v, want %v", i, extensions[i], ext)
		}
	}
}

func TestMarkdownParser(t *testing.T) {
	parser := NewMarkdownParser()

	tests := []struct {
		name     string
		content  string
		wantErr  bool
		checkDoc func(t *testing.T, doc *plugin.ParsedDocument)
	}{
		{
			name:    "simple markdown",
			content: "# Title\n\nThis is a paragraph.",
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				if doc.Metadata["title"] != "Title" {
					t.Errorf("Metadata.title = %v, want %v", doc.Metadata["title"], "Title")
				}
				if len(doc.Structure.Sections) != 1 {
					t.Errorf("Sections count = %v, want %v", len(doc.Structure.Sections), 1)
				}
			},
		},
		{
			name: "markdown with headings",
			content: `# Main Title

## Section 1

Content for section 1.

## Section 2

Content for section 2.`,
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				// Main Title, Section 1, Section 2 = 3 headings
				if len(doc.Structure.Sections) != 3 {
					t.Errorf("Sections count = %v, want %v", len(doc.Structure.Sections), 3)
				}
				if doc.Structure.Sections[0].Title != "Main Title" {
					t.Errorf("First section title = %v, want %v", doc.Structure.Sections[0].Title, "Main Title")
				}
				if doc.Structure.Sections[0].Level != 1 {
					t.Errorf("First section level = %v, want %v", doc.Structure.Sections[0].Level, 1)
				}
			},
		},
		{
			name: "markdown with code block",
			content: "# Code Example\n\n```go\nfunc main() {\n    fmt.Println(\"Hello\")\n}\n```",
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				hasCode := false
				for _, elem := range doc.Structure.Elements {
					if elem.Type == "code" {
						hasCode = true
						if elem.Metadata["language"] != "go" {
							t.Errorf("Code language = %v, want %v", elem.Metadata["language"], "go")
						}
						break
					}
				}
				if !hasCode {
					t.Error("Expected code element not found")
				}
			},
		},
		{
			name: "markdown with list",
			content: "# List\n\n- Item 1\n- Item 2\n- Item 3",
			wantErr: false,
			checkDoc: func(t *testing.T, doc *plugin.ParsedDocument) {
				hasList := false
				for _, elem := range doc.Structure.Elements {
					if elem.Type == "list" {
						hasList = true
						if elem.Metadata["items"] != 3 {
							t.Errorf("List items count = %v, want %v", elem.Metadata["items"], 3)
						}
						break
					}
				}
				if !hasList {
					t.Error("Expected list element not found")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := strings.NewReader(tt.content)
			opts := plugin.ParseOptions{
				Filename: "test.md",
			}

			doc, err := parser.Parse(context.Background(), source, opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.checkDoc != nil {
				tt.checkDoc(t, doc)
			}
		})
	}
}

func TestMarkdownParser_Name(t *testing.T) {
	parser := NewMarkdownParser()
	if got := parser.Name(); got != "markdown" {
		t.Errorf("Name() = %v, want %v", got, "markdown")
	}
}

func TestMarkdownParser_SupportedExtensions(t *testing.T) {
	parser := NewMarkdownParser()
	extensions := parser.SupportedExtensions()
	
	expected := []string{".md", ".markdown", ".mdown", ".mkd", ".mkdn"}
	if len(extensions) != len(expected) {
		t.Errorf("SupportedExtensions() length = %v, want %v", len(extensions), len(expected))
	}

	for i, ext := range expected {
		if extensions[i] != ext {
			t.Errorf("SupportedExtensions()[%d] = %v, want %v", i, extensions[i], ext)
		}
	}
}
