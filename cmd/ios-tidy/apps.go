// cmd/ios-tidy/apps.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/sandbox"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// appsDeps groups the injected seam interfaces for the `apps` subcommand
// family so tests can wire fakes without touching globals. The list
// subcommand uses only Lister/Devices/Stdout/Stderr; probe additionally
// needs a Sandbox seam and a ProbeStore.
type appsDeps struct {
	Lister   apps.Lister
	Devices  device.Lister
	Sandbox  sandbox.Sandbox
	Store    apps.ProbeStore
	Prompter ui.Prompter
	Stdout   io.Writer
	Stderr   io.Writer
}

// defaults fills in nil writers with io.Discard so subcommands that don't
// take an explicit Stdout/Stderr (e.g. validation-only test paths) still
// have a safe writer to point flag.FlagSet output at.
func (d *appsDeps) defaults() {
	if d.Stdout == nil {
		d.Stdout = io.Discard
	}
	if d.Stderr == nil {
		d.Stderr = io.Discard
	}
}

// runApps is the top-level dispatcher for `ios-tidy apps {list|probe|clean} ...`.
// Mirrors runCrashLogs in crashlogs.go: parse the sub-subcommand, route to
// the appropriate handler, return the process exit code.
func runApps(ctx context.Context, deps appsDeps, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy apps {list|probe|clean} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runAppsList(ctx, deps, args[1:])
	case "probe":
		cmd := newAppsProbeCmd(deps)
		if err := cmd.run(ctx, args[1:]); err != nil {
			// Zero-devices is a clean no-op (M1 spec): exit 0. resolveDevice
			// already wrote "no devices attached" to stderr. The probe path
			// wraps that error as `fmt.Errorf("%w: %w", errDeviceResolution,
			// err)` — double-%w wraps BOTH sentinels, so errors.Is traverses
			// to the inner errNoDevicesAttached and we still detect it here.
			// Other errDeviceResolution causes (e.g. "multiple devices
			// attached") fall through to the suppression branch and exit 1.
			if errors.Is(err, errNoDevicesAttached) {
				return 0
			}
			// resolveDevice already wrote to stderr — suppress to avoid a
			// duplicate "no devices attached"-style line. All other error
			// paths produce messages that haven't been printed yet.
			if !errors.Is(err, errDeviceResolution) {
				fmt.Fprintln(deps.Stderr, err)
			}
			return 1
		}
		return 0
	case "clean":
		return runAppsClean(ctx, deps, args[1:])
	default:
		fmt.Fprintf(deps.Stderr, "unknown apps subcommand: %q\n", args[0])
		return 2
	}
}

// runAppsClean implements `ios-tidy apps clean BUNDLE_ID [flags]`.
//
// This subcommand is the only destructive one in the `apps` family. It is
// gated by a Vended probe result (Task 9) so we never attempt to open a
// sandbox the daemon already told us it would refuse — that gate is the
// difference between "polite question" and "wasted house_arrest dial".
//
// args is the slice AFTER "clean" — i.e. [BUNDLE_ID, flags...].
func runAppsClean(ctx context.Context, deps appsDeps, args []string) int {
	deps.defaults()
	fs := flag.NewFlagSet("apps clean", flag.ContinueOnError)
	fs.SetOutput(deps.Stderr)
	var (
		deviceFlag   = fs.String("device", "", "UDID of the target device (required if multiple connected)")
		dryRun       = fs.Bool("dry-run", false, "Show what would be deleted; do not delete")
		yes          = fs.Bool("yes", false, "Skip the basic y/N prompt (does NOT bypass the Documents typed-bundle-ID gate)")
		includeDocs  = fs.Bool("include-documents", false, "Include the app's Documents/ folder (user data — requires typed-bundle-ID confirmation)")
		includeTmp   = fs.Bool("include-tmp", false, "Include the app's tmp/ folder")
		includeCache = fs.Bool("include-caches", false, "Include the app's Library/Caches/ folder")
		storeDir     = fs.String("store-dir", "", "Override probe-store directory (mainly for tests)")
	)
	// Use splitFlagsAndPositionals to fix the flag.Parse-stops-at-positional
	// trap: `ios-tidy apps clean BUNDLE --dry-run` MUST honour --dry-run.
	// See flags.go for the gory details.
	flagArgs, positionals := splitFlagsAndPositionals(fs, args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positionals) < 1 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy apps clean BUNDLE_ID [flags]")
		return 2
	}
	bundleID := positionals[0]

	// Default include-flag combo: tmp + caches when none of --include-* set.
	// Any explicit --include-* REPLACES the default (so passing only
	// --include-documents means "Documents only" — exactly the contract the
	// plan calls for in Task 8 step 3).
	if !*includeTmp && !*includeCache && !*includeDocs {
		*includeTmp = true
		*includeCache = true
	}

	udid, err := resolveDevice(ctx, deps.Devices, *deviceFlag, deps.Stderr)
	if errors.Is(err, errNoDevicesAttached) {
		return 0
	}
	if err != nil {
		return 1
	}

	// Task 9: probe gate. Load the per-UDID probe cache and refuse cleanly
	// unless this bundle has a Vended outcome on record. The Store seam is
	// lazily constructed (mirroring runAppsProbe) so `apps list` doesn't
	// pay for a probes/ mkdir it doesn't need.
	store := deps.Store
	if store == nil {
		dir := *storeDir
		if dir == "" {
			dir, err = defaultStoreDir()
			if err != nil {
				fmt.Fprintln(deps.Stderr, err)
				return 1
			}
		} else if err := validateStoreDir(dir); err != nil {
			fmt.Fprintln(deps.Stderr, err)
			return 1
		}
		store = apps.NewFileProbeStore(dir)
	}
	results, err := store.Load(udid)
	if err != nil {
		fmt.Fprintf(deps.Stderr, "load probe store: %v\n", err)
		return 1
	}
	if !probeVended(results, bundleID) {
		fmt.Fprintf(deps.Stderr,
			"error: bundle %q has not been confirmed as vended on device %s.\n"+
				"Run `ios-tidy apps probe --bundle %s` first to check whether\n"+
				"the device will let us touch this app's sandbox.\n",
			bundleID, udid, bundleID)
		return 1
	}

	// Task 10: open the sandbox and build per-target plans. The probe said
	// Vended at some point in the past; if the daemon now refuses we treat
	// the cached probe as stale and tell the user how to refresh it.
	fsHandle, err := deps.Sandbox.Open(ctx, udid, bundleID)
	if err != nil {
		fmt.Fprintf(deps.Stderr,
			"error: open sandbox for %q on %s: %v\n"+
				"The probe store says this bundle was vended, but the daemon\n"+
				"now refuses. The probe result may be stale; re-run\n"+
				"  ios-tidy apps probe --bundle %s\n"+
				"to refresh.\n",
			bundleID, udid, err, bundleID)
		return 1
	}
	defer fsHandle.Close()

	var plans []sandbox.CleanPlan
	addPlan := func(target sandbox.Target) bool {
		p, err := sandbox.BuildPlan(ctx, fsHandle, target)
		if err != nil {
			fmt.Fprintf(deps.Stderr, "build plan for %s: %v\n", target.Name, err)
			return false
		}
		plans = append(plans, p)
		return true
	}
	if *includeTmp {
		if !addPlan(sandbox.TargetTmp) {
			return 1
		}
	}
	if *includeCache {
		if !addPlan(sandbox.TargetCaches) {
			return 1
		}
	}
	if *includeDocs {
		if !addPlan(sandbox.TargetDocuments) {
			return 1
		}
	}

	ui.RenderCleanPlan(deps.Stdout, bundleID, plans)

	// Task 11: dry-run short-circuit. This placement is load-bearing — the
	// check MUST sit between RenderCleanPlan and the Documents-or-basic
	// prompt branch (Task 13) so the strict typed-bundle-ID prompt is
	// unreachable under --dry-run. The Documents safety net depends on this
	// ordering.
	if *dryRun {
		fmt.Fprintln(deps.Stdout, "Dry run — no changes made.")
		return 0
	}

	// Task 13: strict typed-bundle-ID gate for --include-documents. The user
	// MUST type the bundle ID exactly (case-sensitive, TrimSpace applied) to
	// confirm. --yes does NOT bypass this gate — Documents/ deletion is the
	// only path that erases user data we can't recover, so the safety contract
	// is "make the destructive intent impossible to fat-finger".
	//
	// Task 12: for the non-Documents flow, a basic y/N prompt suffices and
	// --yes may skip it.
	if *includeDocs {
		fmt.Fprintf(deps.Stdout,
			"WARNING: this will delete user data in %s's Documents folder. Files are NOT recoverable.\n",
			bundleID)
		typed, err := deps.Prompter.ReadLine(ctx,
			fmt.Sprintf("Type the bundle ID (%s) to confirm:", bundleID))
		if err != nil {
			fmt.Fprintln(deps.Stderr, "error:", err)
			return 1
		}
		if strings.TrimSpace(typed) != bundleID {
			fmt.Fprintln(deps.Stdout, "Bundle ID did not match. Aborted.")
			return 0
		}
		// Strict gate cleared. --yes does NOT bypass this gate, so no
		// further Confirm() is issued on the Documents path.
	} else if !*yes {
		var totalBytes int64
		for _, p := range plans {
			totalBytes += p.TotalBytes
		}
		question := fmt.Sprintf(
			"Delete %s across %d target(s) in %s?",
			ui.FormatBytes(uint64(totalBytes)), len(plans), bundleID)
		ok, err := deps.Prompter.Confirm(ctx, question)
		if err != nil {
			fmt.Fprintln(deps.Stderr, "error:", err)
			return 1
		}
		if !ok {
			fmt.Fprintln(deps.Stdout, "Aborted.")
			return 0
		}
	}

	cleanResults := executePlans(ctx, fsHandle, plans)
	return reportResults(deps.Stdout, deps.Stderr, cleanResults)
}

// executePlans calls sandbox.Execute for each plan and returns the per-target
// results in input order. The sandbox FS is single-flight, so we deliberately
// loop sequentially rather than fanning out goroutines.
func executePlans(ctx context.Context, fs sandbox.FS, plans []sandbox.CleanPlan) []sandbox.CleanResult {
	out := make([]sandbox.CleanResult, 0, len(plans))
	for _, p := range plans {
		out = append(out, sandbox.Execute(ctx, fs, p))
	}
	return out
}

// reportResults writes a single summary line to stdout and one stderr line
// per failure. Exit code is non-zero iff any per-file failure occurred — the
// summary itself is always printed so the user gets feedback even when the
// destructive op partially succeeded.
func reportResults(stdout, stderr io.Writer, results []sandbox.CleanResult) int {
	var totalRemoved int
	var totalBytes int64
	var totalFailures int
	for _, r := range results {
		totalRemoved += r.Removed
		totalBytes += r.Bytes
		totalFailures += len(r.Failures)
	}
	fmt.Fprintf(stdout, "Deleted %d files (%s freed). %d failure(s).\n",
		totalRemoved, ui.FormatBytes(uint64(totalBytes)), totalFailures)
	for _, r := range results {
		for _, f := range r.Failures {
			fmt.Fprintf(stderr, "  fail: %s: %v\n", f.Path, f.Err)
		}
	}
	if totalFailures > 0 {
		return 1
	}
	return 0
}

// probeVended reports whether results contains a ProbeVended outcome for
// bundleID. The latest entry wins by iterating in order and tracking the
// last match — Save() sorts by bundle ID, not by timestamp, so two probe
// results for the same bundle won't co-exist in practice; we still scan
// linearly to keep the implementation obvious.
func probeVended(results []apps.ProbeResult, bundleID string) bool {
	vended := false
	for _, r := range results {
		if r.BundleID != bundleID {
			continue
		}
		vended = r.Outcome == apps.ProbeVended
	}
	return vended
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
	if errors.Is(err, errNoDevicesAttached) {
		return 0
	}
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

// errDeviceResolution wraps a resolveDevice failure. resolveDevice already
// prints a human-readable explanation to stderr, so the top-level dispatcher
// uses errors.Is(..., errDeviceResolution) to suppress its own stderr echo
// and avoid duplicate output.
var errDeviceResolution = errors.New("device resolution failed")

// appsProbeCmd holds the probe subcommand's deps. The clock lives here
// rather than on appsDeps because only probe writes timestamps.
type appsProbeCmd struct {
	deps appsDeps
	now  func() time.Time // injectable clock; defaults to time.Now in newAppsProbeCmd
}

func newAppsProbeCmd(deps appsDeps) *appsProbeCmd {
	deps.defaults()
	return &appsProbeCmd{deps: deps, now: time.Now}
}

// stringSliceFlag accumulates repeated --bundle values.
type stringSliceFlag []string

func (s *stringSliceFlag) String() string     { return fmt.Sprintf("%v", []string(*s)) }
func (s *stringSliceFlag) Set(v string) error { *s = append(*s, v); return nil }

func (c *appsProbeCmd) run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("apps probe", flag.ContinueOnError)
	fs.SetOutput(c.deps.Stderr)
	deviceFlag := fs.String("device", "", "UDID of the target device")
	all := fs.Bool("all", false, "Probe every user app")
	var bundles stringSliceFlag
	fs.Var(&bundles, "bundle", "Bundle ID to probe (may be repeated)")
	asJSON := fs.Bool("json", false, "Emit JSON instead of a table")
	timeout := fs.Duration("timeout", 5*time.Second, "Per-probe timeout")
	storeDir := fs.String("store-dir", "", "Override probe cache directory (default: user config dir). Mainly for tests.")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Validation: exactly one of --all / --bundle.
	if !*all && len(bundles) == 0 {
		return errors.New("apps probe: pass either --all or one or more --bundle FLAGS")
	}
	if *all && len(bundles) > 0 {
		return errors.New("apps probe: --all and --bundle are mutually exclusive")
	}

	udid, err := resolveDevice(ctx, c.deps.Devices, *deviceFlag, c.deps.Stderr)
	if err != nil {
		// Tag with errDeviceResolution so the runApps dispatcher can
		// suppress its own stderr echo — resolveDevice already wrote the
		// human-readable explanation.
		return fmt.Errorf("%w: %w", errDeviceResolution, err)
	}

	installed, err := c.deps.Lister.UserApps(ctx, udid)
	if err != nil {
		return fmt.Errorf("apps probe: list apps: %w", err)
	}
	installedByID := map[string]apps.App{}
	for _, a := range installed {
		installedByID[a.BundleID] = a
	}

	// Decide the probe list.
	var targets []string
	if *all {
		targets = make([]string, 0, len(installed))
		for _, a := range installed {
			targets = append(targets, a.BundleID)
		}
	} else {
		targets = append(targets, bundles...)
	}

	// Resolve the store. Tests may inject deps.Store directly; if not, build
	// one from --store-dir (validated) or the user config dir default.
	store := c.deps.Store
	if store == nil {
		dir := *storeDir
		if dir == "" {
			dir, err = defaultStoreDir()
			if err != nil {
				return err
			}
		} else if err := validateStoreDir(dir); err != nil {
			return err
		}
		store = apps.NewFileProbeStore(dir)
	}

	prober := apps.NewProber(c.deps.Sandbox)

	results := make([]apps.ProbeResult, 0, len(targets))
	for _, bid := range targets {
		// Not installed → ProbeUnknown, no Sandbox.Open call.
		if _, ok := installedByID[bid]; !ok {
			results = append(results, apps.ProbeResult{
				BundleID: bid,
				Outcome:  apps.ProbeUnknown,
				Detail:   "not installed",
				At:       c.now(),
			})
			continue
		}
		// One context per probe — house_arrest is single-flight per device,
		// so we MUST NOT run probes concurrently.
		pctx, cancel := context.WithTimeout(ctx, *timeout)
		res := prober.Probe(pctx, udid, bid)
		cancel()
		results = append(results, res)
	}

	if err := store.Save(udid, results); err != nil {
		return fmt.Errorf("apps probe: save results: %w", err)
	}

	if *asJSON {
		return writeProbeJSON(c.deps.Stdout, results, installedByID)
	}
	return writeProbeTable(c.deps.Stdout, results, installedByID)
}

// defaultStoreDir returns the default on-disk location for the probe cache:
// $UserConfigDir/ios-tidy/probes. This honours platform conventions
// (Library/Application Support on macOS, %AppData% on Windows, $XDG_CONFIG_HOME
// on Linux) without depending on the user's $HOME directly.
func defaultStoreDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("apps probe: resolve user config dir: %w", err)
	}
	return filepath.Join(base, "ios-tidy", "probes"), nil
}

// validateStoreDir refuses --store-dir values outside the allow-list of
// (UserConfigDir, TempDir). IOS_TIDY_ALLOW_STORE_DIR=1 bypasses the check
// for emergencies / power users — documented in the error message itself
// so a user hitting the guard knows the escape hatch.
func validateStoreDir(dir string) error {
	if os.Getenv("IOS_TIDY_ALLOW_STORE_DIR") == "1" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("apps probe: --store-dir %q: %w", dir, err)
	}
	allowed := []string{}
	if d, err := os.UserConfigDir(); err == nil {
		allowed = append(allowed, d)
	}
	allowed = append(allowed, os.TempDir())
	for _, root := range allowed {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == rootAbs {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return nil
		}
	}
	return fmt.Errorf(
		"apps probe: --store-dir %q is not under os.UserConfigDir or os.TempDir; "+
			"set IOS_TIDY_ALLOW_STORE_DIR=1 to override",
		dir,
	)
}

const detailColumnWidth = 60

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func writeProbeTable(w io.Writer, rs []apps.ProbeResult, byID map[string]apps.App) error {
	fmt.Fprintf(w, "%-40s  %-30s  %-8s  %s\n", "BUNDLE ID", "NAME", "OUTCOME", "DETAIL")
	for _, r := range rs {
		name := byID[r.BundleID].Name
		fmt.Fprintf(w, "%-40s  %-30s  %-8s  %s\n",
			r.BundleID, truncate(name, 30), r.Outcome.String(), truncate(r.Detail, detailColumnWidth))
	}
	return nil
}

func writeProbeJSON(w io.Writer, rs []apps.ProbeResult, byID map[string]apps.App) error {
	type row struct {
		BundleID string    `json:"bundleID"`
		Name     string    `json:"name"`
		Outcome  string    `json:"outcome"`
		Detail   string    `json:"detail"`
		At       time.Time `json:"at"`
	}
	rows := make([]row, len(rs))
	for i, r := range rs {
		rows[i] = row{
			BundleID: r.BundleID,
			Name:     byID[r.BundleID].Name,
			Outcome:  r.Outcome.String(),
			Detail:   r.Detail,
			At:       r.At,
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
