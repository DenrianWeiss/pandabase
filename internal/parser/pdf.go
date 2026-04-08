package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
	"pandabase/pkg/plugin"
)

// PDFParser parses PDF files
type PDFParser struct{}

// NewPDFParser creates a new PDF parser
func NewPDFParser() *PDFParser {
	return &PDFParser{}
}

// Name returns the parser name
func (p *PDFParser) Name() string {
	return "pdf"
}

// SupportedExtensions returns supported file extensions
func (p *PDFParser) SupportedExtensions() []string {
	return []string{".pdf"}
}

// SupportedMimeTypes returns supported MIME types
func (p *PDFParser) SupportedMimeTypes() []string {
	return []string{"application/pdf"}
}

// Parse parses a PDF document
func (p *PDFParser) Parse(ctx context.Context, source io.Reader, opts plugin.ParseOptions) (*plugin.ParsedDocument, error) {
	// ledongthuc/pdf requires an io.ReaderAt and size.
	// We read the entire source into memory here.
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read pdf source: %w", err)
	}

	reader := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(reader, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse pdf: %w", err)
	}

	numPages := pdfReader.NumPage()
	var fullText strings.Builder
	var elements []plugin.Element

	for i := 1; i <= numPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}
		
		content, err := page.GetPlainText(nil)
		if err != nil {
			continue // Skip unreadable pages
		}
		
		contentStr := strings.TrimSpace(content)
		if contentStr != "" {
			fullText.WriteString(contentStr)
			fullText.WriteString("\n\n")

			elements = append(elements, plugin.Element{
				Type:    "page_content",
				Content: contentStr,
				Metadata: map[string]any{
					"page": i,
				},
			})
		}
	}

	structure := &plugin.DocumentStructure{
		Sections: []plugin.Section{
			{
				Title:  opts.Filename,
				Level:  0,
				Offset: 0,
			},
		},
		Elements: elements,
	}

	return &plugin.ParsedDocument{
		Content:   strings.TrimSpace(fullText.String()),
		Metadata:  opts.Metadata,
		Structure: structure,
	}, nil
}
