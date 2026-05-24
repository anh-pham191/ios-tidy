package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"text/tabwriter"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// runDeps groups the injected dependencies for the crashlogs subcommand
// (list/pull/clean) so tests can wire fakes without touching globals. It
// carries the stdout/stderr writers alongside the seam interfaces so each
// run* handler has a single struct argument plus its args slice.
type runDeps struct {
	Lister   device.Lister
	Client   crashlogs.Client
	Prompter ui.Prompter
	Stdout   io.Writer
	Stderr   io.Writer
}

// runCrashLogsList implements `crashlogs list`. Returns the process exit code.
func runCrashLogsList(ctx context.Context, deps runDeps, args []string) int {
	fs := flag.NewFlagSet("crashlogs list", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	var (
		udidFlag    = fs.String("device", "", "UDID of the target device")
		patternFlag = fs.String("pattern", "*", "filepath.Match glob applied to filepath.Base of each path")
		jsonFlag    = fs.Bool("json", false, "emit JSON instead of a table")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Lister, *udidFlag, deps.Stderr)
	if errors.Is(err, errNoDevicesAttached) {
		return 0
	}
	if err != nil {
		return 1
	}

	entries, err := deps.Client.List(ctx, udid, *patternFlag)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "list crash logs: %v\n", err)
		return 1
	}
	// Defense in depth: the real adapter pushes pattern matching down to
	// go-ios, but tests use fakes that don't filter. Apply MatchEntries
	// client-side so behaviour is consistent across seam implementations.
	entries, err = crashlogs.MatchEntries(entries, *patternFlag)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "bad pattern: %v\n", err)
		return 2
	}

	if *jsonFlag {
		enc := json.NewEncoder(deps.Stdout)
		enc.SetIndent("", "  ")
		return jsonExit(enc.Encode(entries), deps.Stderr)
	}

	w := tabwriter.NewWriter(deps.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tSIZE\tMTIME")
	for _, e := range entries {
		mt := "-"
		if !e.ModTime.IsZero() {
			mt = e.ModTime.UTC().Format("2006-01-02 15:04:05Z")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Path, ui.FormatBytes(uint64(e.Size)), mt)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(deps.Stderr, "render: %v\n", err)
		return 1
	}
	return 0
}

func jsonExit(err error, stderr io.Writer) int {
	if err != nil {
		fmt.Fprintf(stderr, "json: %v\n", err)
		return 1
	}
	return 0
}

// runCrashLogs is the top-level dispatcher for `ios-tidy crashlogs ...`. It
// is called from main.go and routes to list/pull/clean.
func runCrashLogs(ctx context.Context, deps runDeps, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy crashlogs {list|pull|clean} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runCrashLogsList(ctx, deps, args[1:])
	case "pull":
		return runCrashLogsPull(ctx, deps, args[1:])
	case "clean":
		return runCrashlogsClean(ctx, deps, args[1:])
	default:
		fmt.Fprintf(deps.Stderr, "unknown crashlogs subcommand: %q\n", args[0])
		return 2
	}
}

// ErrSkippedOverwrite is the sentinel reported in stderr when the user
// declines an overwrite during the pre-scan. Exported so M4 / future
// callers can reuse the vocabulary.
var ErrSkippedOverwrite = errors.New("skipped: user declined overwrite")

// summaryFormat is the uniform shape for the per-run summary line emitted to
// stdout. All three exit paths (declined-abort, total-failure, normal) use
// this format with different values, so the shape stays consistent for
// scripts that parse it. Order: pulled, total, bytesFormatted, skipped, failed.
const summaryFormat = "pulled %d of %d (%s), skipped %d, failed %d\n"

func runCrashLogsPull(ctx context.Context, deps runDeps, args []string) int {
	fs := flag.NewFlagSet("crashlogs pull", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	var (
		udidFlag    = fs.String("device", "", "UDID of the target device")
		patternFlag = fs.String("pattern", "*", "filepath.Match glob applied to filepath.Base of each path")
		outFlag     = fs.String("out", "", "destination directory (required)")
		forceFlag   = fs.Bool("force", false, "overwrite existing files without prompting")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *outFlag == "" {
		fmt.Fprintln(deps.Stderr, "crashlogs pull: --out DIR is required")
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Lister, *udidFlag, deps.Stderr)
	if errors.Is(err, errNoDevicesAttached) {
		return 0
	}
	if err != nil {
		return 1
	}

	entries, err := deps.Client.List(ctx, udid, *patternFlag)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "list crash logs: %v\n", err)
		return 1
	}

	if err := os.MkdirAll(*outFlag, 0o755); err != nil {
		fmt.Fprintf(deps.Stderr, "create out dir: %v\n", err)
		return 1
	}

	// Pre-scan: identify conflicts and prompt the user about each. If any
	// answer is "no" (and --force is off), abort before the bulk Pull starts.
	var declined []string
	if !*forceFlag {
		for _, e := range entries {
			dst := crashlogs.DestPath(*outFlag, e.Path)
			_, statErr := os.Stat(dst)
			if errors.Is(statErr, iofs.ErrNotExist) {
				continue // no conflict
			}
			if statErr != nil {
				// Non-NotExist stat error (e.g. permission denied). Surface
				// it so the user sees the path that the bulk Pull is about
				// to fall over rather than swallowing the symptom.
				fmt.Fprintf(deps.Stderr, "stat %s: %v\n", dst, statErr)
				return 1
			}
			ok, perr := deps.Prompter.Confirm(ctx, fmt.Sprintf("Overwrite %s?", dst))
			if perr != nil {
				fmt.Fprintf(deps.Stderr, "prompt: %v\n", perr)
				return 1
			}
			if !ok {
				declined = append(declined, dst)
			}
		}
	}
	if len(declined) > 0 {
		fmt.Fprintf(deps.Stderr, "%s: declined %d overwrite(s); re-run with --force or remove the conflict(s):\n",
			ErrSkippedOverwrite.Error(), len(declined))
		for _, d := range declined {
			fmt.Fprintf(deps.Stderr, "  %s\n", d)
		}
		// Uniform summary shape (see summaryFormat below).
		fmt.Fprintf(deps.Stdout, summaryFormat, 0, len(entries), ui.FormatBytes(0), len(declined), 0)
		return 1
	}

	// Single bulk pull — go-ios's DownloadReports walks the device once and
	// pulls every match. No per-entry round-trips from this process.
	res, perr := deps.Client.Pull(ctx, udid, *patternFlag, *outFlag)
	if perr != nil {
		fmt.Fprintf(deps.Stderr, "pull crash logs: %v\n", perr)
		fmt.Fprintf(deps.Stdout, summaryFormat, 0, len(entries), ui.FormatBytes(0), 0, len(entries))
		return 1
	}

	fmt.Fprintf(deps.Stdout, summaryFormat,
		res.Pulled, len(entries), ui.FormatBytes(uint64(res.Bytes)), 0, len(res.Failures))
	for _, f := range res.Failures {
		fmt.Fprintf(deps.Stderr, "  failed: %s — %s\n", f.Path, f.ErrMsg)
	}
	if len(res.Failures) > 0 {
		return 1
	}
	return 0
}

// cleanFlags is the parsed flag set for `crashlogs clean`.
type cleanFlags struct {
	device  string
	pattern string
	dryRun  bool
	yes     bool
}

// parseCleanFlags parses the args for `crashlogs clean` and returns the
// populated cleanFlags or the flag-package error (which Parse has already
// printed to stderr via fs.SetOutput).
func parseCleanFlags(stderr io.Writer, args []string) (cleanFlags, error) {
	fs := flag.NewFlagSet("crashlogs clean", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var f cleanFlags
	fs.StringVar(&f.device, "device", "", "device UDID (required when more than one device is connected)")
	fs.StringVar(&f.pattern, "pattern", "*", "filepath.Match glob applied to entry basenames")
	fs.BoolVar(&f.dryRun, "dry-run", false, "list matching entries and total bytes without deleting")
	fs.BoolVar(&f.yes, "yes", false, "skip the interactive confirmation prompt (plan is still rendered)")
	if err := fs.Parse(args); err != nil {
		return cleanFlags{}, err
	}
	return f, nil
}

// runCrashlogsClean executes the crashlogs clean subcommand:
// parse flags → resolve device → list entries → empty-short-circuit →
// render plan → dry-run short-circuit → prompt or --yes → on proceed,
// call Client.Remove and report results.
func runCrashlogsClean(ctx context.Context, deps runDeps, args []string) int {
	f, err := parseCleanFlags(deps.Stderr, args)
	if err != nil {
		return 2
	}
	udid, err := resolveDevice(ctx, deps.Lister, f.device, deps.Stderr)
	if errors.Is(err, errNoDevicesAttached) {
		return 0
	}
	if err != nil {
		return 1
	}
	entries, err := deps.Client.List(ctx, udid, f.pattern)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "list crash logs: %v\n", err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(deps.Stderr, "No matching crash logs.")
		return 0
	}

	actions := make([]ui.Action, 0, len(entries))
	for _, e := range entries {
		actions = append(actions, ui.Action{Path: e.Path, Size: e.Size})
	}
	title := fmt.Sprintf("delete crash logs on %s (pattern %q)", udid, f.pattern)
	// Plan + summary + dry-run/abort notices go to stdout so they're
	// pipeable (e.g. `ios-tidy crashlogs clean --dry-run | tee plan.txt`);
	// only the interactive prompt and real errors land on stderr. Matches
	// the `apps clean` pattern.
	totalBytes := ui.RenderPlan(deps.Stdout, title, actions)

	if f.dryRun {
		fmt.Fprintln(deps.Stdout, "Dry run — no changes made.")
		return 0
	}

	// promptNoun selects between "file" and "files" so the prompt reads
	// naturally for n == 1.
	noun := "files"
	if len(actions) == 1 {
		noun = "file"
	}
	question := fmt.Sprintf("Delete %d %s (%s) from device %s? [y/N]",
		len(actions), noun, ui.FormatBytes(uint64(totalBytes)), udid)
	proceed := f.yes
	if !proceed {
		ok, err := deps.Prompter.Confirm(ctx, question)
		if err != nil {
			fmt.Fprintf(deps.Stderr, "prompt: %v\n", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(deps.Stdout, "Aborted.")
			return 0
		}
		proceed = true
	}
	// Destructive boundary. The `if proceed` is intentionally redundant with
	// the gate above (which already `return`s on every non-proceed path).
	// Keep both guards: the outer `if` makes the destructive call visually
	// distinct so a future refactor that flattens the prompt block cannot
	// silently invert the gate. Do not "simplify" by removing it.
	if proceed {
		if err := ctx.Err(); err != nil {
			fmt.Fprintf(deps.Stderr, "aborted: %v\n", err)
			return 1
		}
		res, err := deps.Client.Remove(ctx, udid, f.pattern)
		if err != nil {
			fmt.Fprintf(deps.Stderr, "remove crash logs: %v\n", err)
			return 1
		}
		fmt.Fprintf(deps.Stdout, "Deleted %d of %d files (%s freed). %d failures.\n",
			res.Removed, len(actions), ui.FormatBytes(uint64(res.Bytes)), len(res.Failures))
		for _, fl := range res.Failures {
			fmt.Fprintf(deps.Stderr, "  %s: %v\n", fl.Path, fl.Err)
		}
		if len(res.Failures) > 0 {
			return 1
		}
	}
	return 0
}
