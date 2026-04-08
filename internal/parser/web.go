package parser

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/go-shiori/go-readability"
	"pandabase/pkg/plugin"
)

// WebParser parses web content (HTML)
type WebParser struct{}

// NewWebParser creates a new web parser
func NewWebParser() *WebParser {
	return &WebParser{}
}

// Name returns the parser name
func (p *WebParser) Name() string {
	return "web"
}

// SupportedExtensions returns supported file extensions
func (p *WebParser) SupportedExtensions() []string {
	return []string{".html", ".htm"}
}

// SupportedMimeTypes returns supported MIME types
func (p *WebParser) SupportedMimeTypes() []string {
	return []string{"text/html"}
}

// Parse parses a web document (HTML content)
func (p *WebParser) Parse(ctx context.Context, source io.Reader, opts plugin.ParseOptions) (*plugin.ParsedDocument, error) {
	// Parse HTML using readability
	// Normally readability takes an io.Reader and a parsed URL to resolve relative links
	var parsedUrl *url.URL
	
	// Option to pass source URL via metadata
	if srcUrl, ok := opts.Metadata["source_url"].(string); ok && srcUrl != "" {
		u, err := url.Parse(srcUrl)
		if err == nil {
			parsedUrl = u
		}
	} else {
		// Dummy URL if none provided
		parsedUrl, _ = url.Parse("http://localhost")
	}

	article, err := readability.FromReader(source, parsedUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse web content: %w", err)
	}

	// Content is the text body of the main article
	text := strings.TrimSpace(article.TextContent)
	if text == "" {
		// Fallback to title if content is empty
		text = article.Title
	}

	// Extract basic elements (could be improved by integrating a markdown converter or DOM traversal, 
	// but readability's TextContent handles the bulk of it well for now)
	elements := []plugin.Element{
		{
			Type:    "article_body",
			Content: text,
		},
	}

	structure := &plugin.DocumentStructure{
		Sections: []plugin.Section{
			{
				Title:  article.Title,
				Level:  1,
				Offset: 0,
			},
		},
		Elements: elements,
	}

	// Add extraction metadata
	meta := opts.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["title"] = article.Title
	meta["byline"] = article.Byline
	meta["site_name"] = article.SiteName
	meta["excerpt"] = article.Excerpt

	return &plugin.ParsedDocument{
		Content:   text,
		Metadata:  meta,
		Structure: structure,
	}, nil
}
