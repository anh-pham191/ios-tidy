// cmd/ios-tidy-mcp/main.go
//
// ios-tidy-mcp speaks the Model Context Protocol over stdio so an MCP
// client (Claude Desktop, Claude Code, etc.) can drive ios-tidy. Tools
// are added incrementally; this binary currently exposes:
//   - version             (smoke-test)
//   - devices_list        (read-only)
//   - storage             (read-only)
//   - crashlogs_list      (read-only)
//   - apps_list           (read-only)
//   - apps_probe          (read-only; persists results to the shared cache)
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

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/iosbackend"
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

	deps, err := buildServerDeps()
	if err != nil {
		log.Fatalf("build deps: %v", err)
	}

	s.AddTool(
		mcp.NewTool("version",
			mcp.WithDescription("Returns the ios-tidy-mcp server version string. Smoke-test tool."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		handleVersion,
	)
	addReadOnlyTools(s, deps)

	if err := server.ServeStdio(s); err != nil {
		log.Fatalf("serve stdio: %v", err)
	}
}

// buildServerDeps wires the production seams once at startup. The
// resulting struct is captured by every tool handler — no per-request
// construction so we don't pay for go-ios setup on hot calls.
//
// Only the read-only seams are populated for this commit. The Sandbox
// field stays unwired until the destructive-tools commit; the prober
// already routes through Sandbox under the hood, so we DO wire Sandbox
// here for apps_probe.
func buildServerDeps() (serverDeps, error) {
	listerAll, _ := iosbackend.NewApps()
	sb := iosbackend.NewSandbox()
	storeDir, err := defaultProbeStoreDir()
	if err != nil {
		return serverDeps{}, fmt.Errorf("probe store dir: %w", err)
	}
	return serverDeps{
		Lister:       iosbackend.NewDeviceLister(),
		TrustChecker: iosbackend.NewTrustChecker(),
		Storage:      iosbackend.NewStorage(),
		Apps:         listerAll,
		CrashLogs:    iosbackend.NewCrashLogs(),
		Sandbox:      sb,
		Prober:       apps.NewProber(sb),
		ProbeStore:   apps.NewFileProbeStore(storeDir),
	}, nil
}

func handleVersion(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(fmt.Sprintf("ios-tidy-mcp %s", Version)), nil
}
