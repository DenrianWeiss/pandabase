package parser

import (
	"bufio"
	"context"
	"io"
	"strings"

	"pandabase/pkg/plugin"
)

// TextParser parses plain text files
type TextParser struct{}

// NewTextParser creates a new text parser
func NewTextParser() *TextParser {
	return &TextParser{}
}

// Name returns the parser name
func (p *TextParser) Name() string {
	return "text"
}

// SupportedExtensions returns supported file extensions
func (p *TextParser) SupportedExtensions() []string {
	return []string{".txt", ".text"}
}

// SupportedMimeTypes returns supported MIME types
func (p *TextParser) SupportedMimeTypes() []string {
	return []string{"text/plain"}
}

// Parse parses a text document
func (p *TextParser) Parse(ctx context.Context, source io.Reader, opts plugin.ParseOptions) (*plugin.ParsedDocument, error) {
	content, err := io.ReadAll(source)
	if err != nil {
		return nil, err
	}

	text := string(content)

	// Build structure by detecting paragraphs
	structure := &plugin.DocumentStructure{
		Sections: []plugin.Section{
			{
				Title:  "Document",
				Level:  0,
				Offset: 0,
			},
		},
		Elements: p.extractElements(text),
	}

	return &plugin.ParsedDocument{
		Content:   text,
		Metadata:  opts.Metadata,
		Structure: structure,
	}, nil
}

// extractElements extracts paragraphs from text
func (p *TextParser) extractElements(text string) []plugin.Element {
	var elements []plugin.Element
	scanner := bufio.NewScanner(strings.NewReader(text))
	
	var currentParagraph strings.Builder
	lineNum := 0
	
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		
		if strings.TrimSpace(line) == "" {
			if currentParagraph.Len() > 0 {
				elements = append(elements, plugin.Element{
					Type:    "paragraph",
					Content: strings.TrimSpace(currentParagraph.String()),
					Metadata: map[string]any{
						"line_start": lineNum - strings.Count(currentParagraph.String(), "\n") - 1,
						"line_end":   lineNum - 1,
					},
				})
				currentParagraph.Reset()
			}
		} else {
			if currentParagraph.Len() > 0 {
				currentParagraph.WriteString("\n")
			}
			currentParagraph.WriteString(line)
		}
	}
	
	// Don't forget the last paragraph
	if currentParagraph.Len() > 0 {
		elements = append(elements, plugin.Element{
			Type:    "paragraph",
			Content: strings.TrimSpace(currentParagraph.String()),
			Metadata: map[string]any{
				"line_start": lineNum - strings.Count(currentParagraph.String(), "\n"),
				"line_end":   lineNum,
			},
		})
	}
	
	return elements
}
