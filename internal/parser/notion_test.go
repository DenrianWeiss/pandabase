package parser

import (
	"context"
	"strings"
	"testing"
	"pandabase/pkg/plugin"
)

func TestExtractPageID(t *testing.T) {
	url := "https://www.notion.so/Workspace-Name-1234567890abcdef1234567890abcdef"
	id := extractPageID(url)
	if id != "1234567890abcdef1234567890abcdef" {
		t.Errorf("Expected 1234567890abcdef1234567890abcdef, got %s", id)
	}

	urlQuery := "https://www.notion.so/Workspace-Name-1234567890abcdef?query=1"
	idQuery := extractPageID(urlQuery)
	if idQuery != "1234567890abcdef" {
		t.Errorf("Expected 1234567890abcdef, got %s", idQuery)
	}
}

func TestNotionParserNoKey(t *testing.T) {
	parser := NewNotionParser()
	doc := `{"url": "https://notion.so/test-123", "nonce": "abc"}`
	
	_, err := parser.Parse(context.Background(), strings.NewReader(doc), plugin.ParseOptions{
		Metadata: map[string]any{},
	})
	
	if err == nil || !strings.Contains(err.Error(), "notion_api_key is missing") {
		t.Errorf("Expected missing api key error, got: %v", err)
	}
}
