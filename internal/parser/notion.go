package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jomei/notionapi"
	"pandabase/pkg/plugin"
)

// NotionParser parses Notion pages
type NotionParser struct{}

// NewNotionParser creates a new notion parser
func NewNotionParser() *NotionParser {
	return &NotionParser{}
}

// Name returns the parser name
func (p *NotionParser) Name() string {
	return "notion"
}

// SupportedExtensions returns supported file extensions
func (p *NotionParser) SupportedExtensions() []string {
	return []string{".notion"}
}

// SupportedMimeTypes returns supported MIME types
// application/json since .notion files are internal JSON representations
func (p *NotionParser) SupportedMimeTypes() []string {
	return []string{"application/json", "application/x-notion"}
}

// NotionSource represents the content of a .notion file
type NotionSource struct {
	URL   string `json:"url"`
	Nonce string `json:"nonce"`
}

// Parse parses a Notion document via the Notion API.
// IMPORTANT NOTE FOR FRONTEND INTEGRATION:
// The Notion API key MUST be provided per request via opts.Metadata["notion_api_key"].
// It should not be a globally configured environment variable for security and multi-tenancy.
// A .notion dummy file will be uploaded containing {"url": "...", "nonce": "..."}.
// Different nonce allows cache busting/refetching.
func (p *NotionParser) Parse(ctx context.Context, source io.Reader, opts plugin.ParseOptions) (*plugin.ParsedDocument, error) {
	// Parse the .notion source file
	data, err := io.ReadAll(source)
	if err != nil {
		return nil, fmt.Errorf("failed to read notion source: %w", err)
	}

	var notionSrc NotionSource
	if err := json.Unmarshal(data, &notionSrc); err != nil {
		return nil, fmt.Errorf("invalid .notion file format: %w", err)
	}

	if notionSrc.URL == "" {
		return nil, errors.New("notion URL is missing in source file")
	}

	// Retrieve the API key from request metadata
	apiKeyRaw, exists := opts.Metadata["notion_api_key"]
	if !exists {
		return nil, errors.New("notion_api_key is missing from request metadata")
	}
	
	apiKey, ok := apiKeyRaw.(string)
	if !ok || apiKey == "" {
		return nil, errors.New("notion_api_key must be a non-empty string")
	}

	// Extract Page ID from URL
	pageID := extractPageID(notionSrc.URL)
	if pageID == "" {
		return nil, errors.New("invalid Notion URL")
	}

	// Initialize the Notion API client
	client := notionapi.NewClient(notionapi.Token(apiKey))

	// Fetch page metadata (optional, just to get the title)
	// We'll skip complex page metadata fetching for now to focus on content.
	
	var fullText strings.Builder
	var elements []plugin.Element

	var cursor notionapi.Cursor
	for {
		blocks, err := client.Block.GetChildren(ctx, notionapi.BlockID(pageID), &notionapi.Pagination{
			StartCursor: cursor,
			PageSize:    100,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to fetch notion blocks: %w", err)
		}

		for _, block := range blocks.Results {
			content := extractBlockText(block)
			if content != "" {
				fullText.WriteString(content)
				fullText.WriteString("\n\n")

				elements = append(elements, plugin.Element{
					Type:    string(block.GetType()),
					Content: content,
				})
			}
		}

		if !blocks.HasMore {
			break
		}
		cursor = notionapi.Cursor(blocks.NextCursor)
	}

	structure := &plugin.DocumentStructure{
		Sections: []plugin.Section{
			{
				Title:  opts.Filename, // Could be replaced by actual Notion page title
				Level:  0,
				Offset: 0,
			},
		},
		Elements: elements,
	}

	// Preserve incoming metadata but append the nonce and source url
	meta := opts.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	meta["notion_url"] = notionSrc.URL
	meta["nonce"] = notionSrc.Nonce

	// Clean API key from metadata so it's not saved to vector meta search indefinitely
	delete(meta, "notion_api_key")

	return &plugin.ParsedDocument{
		Content:   strings.TrimSpace(fullText.String()),
		Metadata:  meta,
		Structure: structure,
	}, nil
}

// extractPageID simple extraction of ID from URL
func extractPageID(url string) string {
	// A typical Notion URL looks like https://www.notion.so/Workspace-Name-1234567890abcdef1234567890abcdef
	// We just grab the last 32 chars
	parts := strings.Split(url, "-")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// Removing query params if any
		lastPart = strings.Split(lastPart, "?")[0]
		return lastPart
	}
	return ""
}

// extractBlockText extracts plain text from a Notion Block
func extractBlockText(block notionapi.Block) string {
	switch b := block.(type) {
	case *notionapi.ParagraphBlock:
		return extractRichText(b.Paragraph.RichText)
	case *notionapi.Heading1Block:
		return extractRichText(b.Heading1.RichText)
	case *notionapi.Heading2Block:
		return extractRichText(b.Heading2.RichText)
	case *notionapi.Heading3Block:
		return extractRichText(b.Heading3.RichText)
	case *notionapi.BulletedListItemBlock:
		return extractRichText(b.BulletedListItem.RichText)
	case *notionapi.NumberedListItemBlock:
		return extractRichText(b.NumberedListItem.RichText)
	case *notionapi.QuoteBlock:
		return extractRichText(b.Quote.RichText)
	case *notionapi.CodeBlock:
		return extractRichText(b.Code.RichText)
	}
	return ""
}

// extractRichText converts a slice of RichText to a single string
func extractRichText(richTexts []notionapi.RichText) string {
	var txt string
	for _, rt := range richTexts {
		txt += rt.PlainText
	}
	return txt
}
