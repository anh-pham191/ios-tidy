// cmd/ios-tidy/apps.go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// appsDeps groups the injected seam interfaces for the `apps` subcommand
// family so tests can wire fakes without touching globals. Only the fields
// `apps list` needs are present today; Task 12 (`apps probe`) will extend
// this struct with sandbox.Sandbox + apps.ProbeStore + a clock. Keeping the
// struct minimal here avoids forcing every test to populate fields that
// have no effect on `list`.
type appsDeps struct {
	Lister  apps.Lister
	Devices device.Lister
	Stdout  io.Writer
	Stderr  io.Writer
}

// runApps is the top-level dispatcher for `ios-tidy apps {list|probe} ...`.
// Mirrors runCrashLogs in crashlogs.go: parse the sub-subcommand, route to
// the appropriate handler, return the process exit code. `probe` is wired
// to a "not implemented yet" stub until Task 12 replaces it.
func runApps(ctx context.Context, deps appsDeps, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy apps {list|probe} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runAppsList(ctx, deps, args[1:])
	case "probe":
		fmt.Fprintln(deps.Stderr, "apps probe: not implemented yet (Task 12)")
		return 2
	default:
		fmt.Fprintf(deps.Stderr, "unknown apps subcommand: %q\n", args[0])
		return 2
	}
}

// runAppsList implements `ios-tidy apps list`. Lists every user-installed app
// on the target device sorted descending by total bytes
// (DynamicBytes + StaticBytes). Unlike `storage`, this command emits NO
// device-level summary header — it is the bare apps list, intended as the
// input source for `apps probe` / `apps clean` workflows that don't care
// about free-space context.
func runAppsList(ctx context.Context, deps appsDeps, args []string) int {
	fs := flag.NewFlagSet("apps list", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	var (
		udidFlag = fs.String("device", "", "UDID of the target device (required if multiple connected)")
		jsonFlag = fs.Bool("json", false, "emit JSON instead of a table")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	udid, err := resolveDevice(ctx, deps.Devices, *udidFlag, deps.Stderr)
	if err != nil {
		return 1
	}

	list, err := deps.Lister.UserApps(ctx, udid)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "list apps: %v\n", err)
		return 1
	}

	// Sort in place — apps.Sort orders by total bytes descending with a
	// bundle-ID tie-break, which is exactly the output contract for this
	// command. Reusing the helper keeps the ordering identical to `storage`.
	apps.Sort(list)

	if *jsonFlag {
		enc := json.NewEncoder(deps.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(list); err != nil {
			fmt.Fprintf(deps.Stderr, "encode json: %v\n", err)
			return 1
		}
		return 0
	}

	return renderAppsTable(deps.Stdout, list)
}

// renderAppsTable writes the table form of `apps list`. Columns mirror the
// per-app portion of `storage` (bundle id, name, version, dynamic, static,
// total, file-sharing) minus the device summary line. Right-aligning the
// byte columns keeps the magnitudes visually comparable when scanning.
func renderAppsTable(w io.Writer, list []apps.App) int {
	tbl := ui.NewTable("bundle id", "name", "version", "dynamic", "static", "total", "file-sharing")
	for _, a := range list {
		share := "no"
		if a.FileSharingEnabled {
			share = "yes"
		}
		tbl.AddRow(
			a.BundleID,
			a.Name,
			a.Version,
			ui.FormatBytes(a.DynamicBytes),
			ui.FormatBytes(a.StaticBytes),
			ui.FormatBytes(a.DynamicBytes+a.StaticBytes),
			share,
		)
	}
	fmt.Fprint(w, tbl.Render([]ui.Alignment{
		ui.AlignLeft, ui.AlignLeft, ui.AlignLeft,
		ui.AlignRight, ui.AlignRight, ui.AlignRight,
		ui.AlignLeft,
	}))
	return 0
}
