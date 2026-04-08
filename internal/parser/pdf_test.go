package parser

import (
	"context"
	"strings"
	"testing"
	"pandabase/pkg/plugin"
)

// In a real scenario we would pass actual PDF bytes. We'll just test that
// it errors gracefully when given invalid bytes.
func TestPDFParserInvalidPDF(t *testing.T) {
	parser := NewPDFParser()
	source := strings.NewReader("not a real pdf content")
	
	_, err := parser.Parse(context.Background(), source, plugin.ParseOptions{
		Filename: "test.pdf",
	})
	
	if err == nil {
		t.Errorf("Expected error when parsing invalid PDF, got nil")
	}
}
