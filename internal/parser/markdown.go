package parser

import (
	"context"
	"io"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"pandabase/pkg/plugin"
)

// MarkdownParser parses Markdown files using goldmark
type MarkdownParser struct {
	md goldmark.Markdown
}

// NewMarkdownParser creates a new markdown parser
func NewMarkdownParser() *MarkdownParser {
	return &MarkdownParser{
		md: goldmark.New(),
	}
}

// Name returns the parser name
func (p *MarkdownParser) Name() string {
	return "markdown"
}

// SupportedExtensions returns supported file extensions
func (p *MarkdownParser) SupportedExtensions() []string {
	return []string{".md", ".markdown", ".mdown", ".mkd", ".mkdn"}
}

// SupportedMimeTypes returns supported MIME types
func (p *MarkdownParser) SupportedMimeTypes() []string {
	return []string{"text/markdown", "text/x-markdown"}
}

// Parse parses a markdown document
func (p *MarkdownParser) Parse(ctx context.Context, source io.Reader, opts plugin.ParseOptions) (*plugin.ParsedDocument, error) {
	content, err := io.ReadAll(source)
	if err != nil {
		return nil, err
	}

	sourceText := string(content)

	// Parse the markdown AST
	doc := p.md.Parser().Parse(text.NewReader(content))

	// Extract structure using AST
	structure := p.extractStructure(doc, content, sourceText)

	return &plugin.ParsedDocument{
		Content:   sourceText,
		Metadata:  p.extractMetadata(sourceText, opts.Metadata),
		Structure: structure,
	}, nil
}

// extractMetadata extracts metadata from frontmatter or options
func (p *MarkdownParser) extractMetadata(content string, optsMetadata map[string]any) map[string]any {
	metadata := make(map[string]any)

	// Copy options metadata
	for k, v := range optsMetadata {
		metadata[k] = v
	}

	// Try to extract title from first heading
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			metadata["title"] = strings.TrimPrefix(line, "# ")
			break
		}
	}

	return metadata
}

// extractStructure extracts document structure from markdown AST
func (p *MarkdownParser) extractStructure(doc ast.Node, source []byte, sourceText string) *plugin.DocumentStructure {
	var sections []plugin.Section
	var elements []plugin.Element

	// Walk the AST
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch node := n.(type) {
		case *ast.Heading:
			// Get heading text from children
			var title strings.Builder
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				if textNode, ok := child.(*ast.Text); ok {
					title.Write(textNode.Value(source))
				}
			}
			section := plugin.Section{
				Title:  strings.TrimSpace(title.String()),
				Level:  node.Level,
				Offset: node.Lines().At(0).Start,
			}
			sections = append(sections, section)

		case *ast.Paragraph:
			lines := node.Lines()
			if lines.Len() > 0 {
				startLine := lines.At(0).Start
				endLine := lines.At(lines.Len() - 1).Stop
				content := string(source[startLine:endLine])
				if strings.TrimSpace(content) != "" {
					elements = append(elements, plugin.Element{
						Type:    "paragraph",
						Content: strings.TrimSpace(content),
						Metadata: map[string]any{
							"start_line": startLine,
							"end_line":   endLine,
						},
					})
				}
			}

		case *ast.FencedCodeBlock:
			lines := node.Lines()
			if lines.Len() > 0 {
				startLine := lines.At(0).Start
				endLine := lines.At(lines.Len() - 1).Stop
				content := string(source[startLine:endLine])
				lang := string(node.Language(source))
				elements = append(elements, plugin.Element{
					Type:    "code",
					Content: strings.TrimSpace(content),
					Metadata: map[string]any{
						"start_line": startLine,
						"end_line":   endLine,
						"language":   lang,
					},
				})
			}

		case *ast.CodeBlock:
			// Handle indented code blocks (non-fenced)
			lines := node.Lines()
			if lines.Len() > 0 {
				startLine := lines.At(0).Start
				endLine := lines.At(lines.Len() - 1).Stop
				content := string(source[startLine:endLine])
				elements = append(elements, plugin.Element{
					Type:    "code",
					Content: strings.TrimSpace(content),
					Metadata: map[string]any{
						"start_line": startLine,
						"end_line":   endLine,
						"language":   "",
					},
				})
			}

		case *ast.List:
			var items []string
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				if listItem, ok := child.(*ast.ListItem); ok {
					// Get text from list item children
					var itemText strings.Builder
					for itemChild := listItem.FirstChild(); itemChild != nil; itemChild = itemChild.NextSibling() {
						if para, ok := itemChild.(*ast.Paragraph); ok {
							lines := para.Lines()
							for i := 0; i < lines.Len(); i++ {
								seg := lines.At(i)
								itemText.Write(source[seg.Start:seg.Stop])
							}
						}
					}
					items = append(items, strings.TrimSpace(itemText.String()))
				}
			}
			if len(items) > 0 {
				elements = append(elements, plugin.Element{
					Type:    "list",
					Content: strings.Join(items, "\n"),
					Metadata: map[string]any{
						"items": len(items),
					},
				})
			}
			return ast.WalkSkipChildren, nil
		}

		return ast.WalkContinue, nil
	})

	return &plugin.DocumentStructure{
		Sections: sections,
		Elements: elements,
	}
}
