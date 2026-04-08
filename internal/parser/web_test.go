package parser

import (
	"context"
	"strings"
	"testing"
	"pandabase/pkg/plugin"
)

func TestWebParser(t *testing.T) {
	parser := NewWebParser()
	
	htmlContent := `
	<html>
		<head><title>Test Article</title></head>
		<body>
			<nav>Skip this</nav>
			<article>
				<h1>Main Content</h1>
				<p>This is the main readable content.</p>
			</article>
			<footer>And skip this</footer>
		</body>
	</html>
	`
	
	doc, err := parser.Parse(context.Background(), strings.NewReader(htmlContent), plugin.ParseOptions{
		Metadata: map[string]any{
			"source_url": "https://example.com/article",
		},
	})
	
	if err != nil {
		t.Fatalf("Failed to parse html: %v", err)
	}
	
	if doc.Structure.Sections[0].Title != "Test Article" {
		t.Errorf("Expected title 'Test Article', got %s", doc.Structure.Sections[0].Title)
	}
}
