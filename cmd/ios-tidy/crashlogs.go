package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// crashLogsDeps groups the injected dependencies for the crashlogs subcommand
// so tests can wire fakes without touching globals.
type crashLogsDeps struct {
	Lister   device.Lister
	Client   crashlogs.Client
	Prompter ui.Prompter
}

// resolveDevice picks the target UDID. If override is non-empty, it's used
// verbatim. Otherwise: zero attached → error; one → that one; many → error
// listing UDIDs.
func resolveDevice(ctx context.Context, l device.Lister, override string, stderr io.Writer) (string, error) {
	if override != "" {
		return override, nil
	}
	devs, err := l.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list devices: %w", err)
	}
	switch len(devs) {
	case 0:
		fmt.Fprintln(stderr, "no devices attached")
		return "", errors.New("no devices attached")
	case 1:
		return devs[0].UDID, nil
	default:
		fmt.Fprintln(stderr, "multiple devices attached; use --device UDID:")
		for _, d := range devs {
			fmt.Fprintf(stderr, "  %s  %s\n", d.UDID, d.Name)
		}
		return "", errors.New("multiple devices attached")
	}
}

// runCrashLogsList implements `crashlogs list`. Returns the process exit code.
func runCrashLogsList(ctx context.Context, deps crashLogsDeps, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("crashlogs list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		udidFlag    = fs.String("device", "", "UDID of the target device")
		patternFlag = fs.String("pattern", "*", "filepath.Match glob applied to filepath.Base of each path")
		jsonFlag    = fs.Bool("json", false, "emit JSON instead of a table")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Lister, *udidFlag, stderr)
	if err != nil {
		return 1
	}

	entries, err := deps.Client.List(ctx, udid, *patternFlag)
	if err != nil {
		fmt.Fprintf(stderr, "list crash logs: %v\n", err)
		return 1
	}
	// Defense in depth: the real adapter pushes pattern matching down to
	// go-ios, but tests use fakes that don't filter. Apply MatchEntries
	// client-side so behaviour is consistent across seam implementations.
	entries, err = crashlogs.MatchEntries(entries, *patternFlag)
	if err != nil {
		fmt.Fprintf(stderr, "bad pattern: %v\n", err)
		return 2
	}

	if *jsonFlag {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return jsonExit(enc.Encode(entries), stderr)
	}

	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PATH\tSIZE\tMTIME")
	for _, e := range entries {
		mt := "-"
		if !e.ModTime.IsZero() {
			mt = e.ModTime.UTC().Format("2006-01-02 15:04:05Z")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", e.Path, ui.FormatBytes(uint64(e.Size)), mt)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintf(stderr, "render: %v\n", err)
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
// is called from main.go and routes to list/pull. `clean` is M4.
func runCrashLogs(ctx context.Context, deps crashLogsDeps, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: ios-tidy crashlogs {list|pull} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runCrashLogsList(ctx, deps, args[1:], stdout, stderr)
	case "pull":
		return runCrashLogsPull(ctx, deps, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown crashlogs subcommand: %q\n", args[0])
		return 2
	}
}

// runCrashLogsPull is implemented in Task 7. Stub keeps the dispatcher
// compiling for Task 6 — referenced only via the runCrashLogs switch above,
// so it needs no imports beyond what this file already uses.
func runCrashLogsPull(_ context.Context, _ crashLogsDeps, _ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "crashlogs pull: not implemented yet")
	return 2
}
