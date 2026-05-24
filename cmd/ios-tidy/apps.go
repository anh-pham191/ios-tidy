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
	Lister  apps.Lister
	Devices device.Lister
	Sandbox sandbox.Sandbox
	Store   apps.ProbeStore
	Stdout  io.Writer
	Stderr  io.Writer
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

// runApps is the top-level dispatcher for `ios-tidy apps {list|probe} ...`.
// Mirrors runCrashLogs in crashlogs.go: parse the sub-subcommand, route to
// the appropriate handler, return the process exit code.
func runApps(ctx context.Context, deps appsDeps, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(deps.Stderr, "usage: ios-tidy apps {list|probe} [flags]")
		return 2
	}
	switch args[0] {
	case "list":
		return runAppsList(ctx, deps, args[1:])
	case "probe":
		cmd := newAppsProbeCmd(deps)
		if err := cmd.run(ctx, args[1:]); err != nil {
			// resolveDevice already wrote to stderr — suppress to avoid a
			// duplicate "no devices attached" line. All other error paths
			// produce messages that haven't been printed yet.
			if !errors.Is(err, errDeviceResolution) {
				fmt.Fprintln(deps.Stderr, err)
			}
			return 1
		}
		return 0
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
