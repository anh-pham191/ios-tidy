package main

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleVersion_returnsBuildStampedVersion(t *testing.T) {
	saved := Version
	Version = "v0.1.0-test"
	defer func() { Version = saved }()

	result, err := handleVersion(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("handleVersion: %v", err)
	}
	if result == nil {
		t.Fatal("handleVersion returned nil result")
	}

	text := extractText(result)
	if !strings.Contains(text, "ios-tidy-mcp v0.1.0-test") {
		t.Errorf("version output = %q, want substring %q", text, "ios-tidy-mcp v0.1.0-test")
	}
}

// extractText concatenates the text from every TextContent block in result.
// In mark3labs/mcp-go, NewToolResultText produces a single TextContent entry
// in Content, but iterating tolerates future tool results that emit multiple
// blocks.
func extractText(r *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}
