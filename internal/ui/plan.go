package ui

import (
	"fmt"
	"io"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
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

// RenderCleanPlan writes a multi-target sandbox-clean plan to w. Format:
//
//	Clean plan for <bundleID>:
//	  <Target.Name>/  N files  X (human-readable)
//	  ...
//	Total: X across N target(s)
//
// This sits alongside RenderPlan (which is crash-log-shaped: one flat list
// of paths) because the cleaner emits per-target groupings, not a single
// path list. Keeping the format separate avoids fudging RenderPlan into a
// dual-purpose helper whose callers each only want half its output.
func RenderCleanPlan(w io.Writer, bundleID string, plans []sandbox.CleanPlan) {
	fmt.Fprintf(w, "Clean plan for %s:\n", bundleID)
	var total int64
	for _, p := range plans {
		fmt.Fprintf(w, "  %s/  %d files  %s\n",
			p.Target.Name, len(p.Files), FormatBytes(uint64(p.TotalBytes)))
		total += p.TotalBytes
	}
	fmt.Fprintf(w, "Total: %s across %d target(s)\n",
		FormatBytes(uint64(total)), len(plans))
}
