// cmd/ios-tidy-mcp/main.go
//
// ios-tidy-mcp speaks the Model Context Protocol over stdio so an MCP
// client (Claude Desktop, Claude Code, etc.) can drive ios-tidy. Tools
// are added incrementally; this binary starts with only `version` as a
// connect smoke-test.
//
// Safety model (binding for every destructive tool added later):
//   - Destructive tools refuse without an explicit confirmation arg.
//   - apps_clean's --include-documents requires the caller to re-state
//     the bundle ID AND set i_understand_documents_are_unrecoverable.
//   - --dry-run is the default for destructive tools.
//   - The interactive y/N prompt path is REPLACED by arg-level confirm;
//     there is no stdin to read over MCP.
//
// Logging: write to stderr only. stdout is the MCP transport.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Version is set at build time via -ldflags '-X main.Version=...' and
// mirrors the ios-tidy CLI's version stamp. See cmd/ios-tidy/version.go.
var Version = "dev"

func main() {
	log.SetOutput(os.Stderr) // never write to stdout — stdio belongs to MCP
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	s := server.NewMCPServer(
		"ios-tidy-mcp",
		Version,
		// Add capabilities here as we grow.
	)

	s.AddTool(
		mcp.NewTool("version",
			mcp.WithDescription("Returns the ios-tidy-mcp server version string. Smoke-test tool."),
		),
		handleVersion,
	)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("serve stdio: %v", err)
	}
}

func handleVersion(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(fmt.Sprintf("ios-tidy-mcp %s", Version)), nil
}
