package ui

import (
	"fmt"
	"io"
)

// Action is a single destructive operation to be displayed in a plan
// rendered by RenderPlan. The plan-renderer is intentionally domain-agnostic
// so it can serve crashlogs (M4) and per-app sandbox cleanup (M6) alike.
//
// Action mirrors crashlogs.Entry minus the ModTime field so that
// internal/ui does not depend on internal/crashlogs. The conversion is a
// one-line loop at the call site.
type Action struct {
	Path string
	Size int64
}

// RenderPlan writes a destructive-operation plan to out:
//
//	Plan: <title>
//	  <path>  <human-readable size>
//	  ...
//	Total: N files, X (human-readable bytes)
//
// It returns the total bytes summed across actions so callers can reuse the
// figure (e.g. in a confirmation prompt) without re-iterating actions.
// out is written-to exactly once per call; errors from out are ignored
// because the plan is purely advisory output.
func RenderPlan(out io.Writer, title string, actions []Action) (totalBytes int64) {
	fmt.Fprintf(out, "Plan: %s\n", title)
	for _, a := range actions {
		fmt.Fprintf(out, "  %s\t%s\n", a.Path, FormatBytes(uint64(a.Size)))
		totalBytes += a.Size
	}
	fmt.Fprintf(out, "Total: %d files, %s\n", len(actions), FormatBytes(uint64(totalBytes)))
	return totalBytes
}
