// cmd/ios-tidy-mcp/tools.go
//
// MCP tool handlers for the read-only subset of ios-tidy:
//   - devices_list
//   - storage
//   - crashlogs_list
//   - apps_list
//   - apps_probe
//
// Every handler follows the same shape:
//  1. Resolve the target UDID via resolveDeviceForTool. That helper
//     captures resolveDevice's stderr into a buffer and translates the
//     three outcomes (success / no devices / other failure) into
//     ready-to-return MCP results.
//  2. Call the relevant seam (storage.Client, apps.Lister, ...) with
//     the deps stamped at server startup.
//  3. Marshal the result with the SAME Go types the CLI marshals so
//     the wire-level JSON shape stays identical to `ios-tidy ... --json`.
//
// Tool descriptions are deliberately verbose — the LLM caller cannot
// read RESEARCH.md, so the platform caveats it needs to plan retries
// (iOS 17.1 house_arrest flakiness, AFC zero-sized cold apps, macOS
// Tahoe TCC trust-check failures) live in the description text.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/cmdutil"
	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/sandbox"
	"github.com/anh-pham191/ios-tidy/internal/storage"
)

// serverDeps is the wired-once dependency set the MCP handlers close
// over. main constructs the production seams once at startup; tests
// substitute fakes via the per-field zero values.
//
// Only the read-only seams need to be populated for the tools in this
// file (Lister, TrustChecker, Storage, Apps, CrashLogs, Prober,
// ProbeStore). Sandbox is included here for tool 6+ (apps_clean etc.)
// in the next commit, but is also used by the apps_probe handler — the
// prober dials Sandbox.Open under the hood, so populating Prober via
// apps.NewProber(Sandbox) is the standard wiring.
type serverDeps struct {
	Lister       device.Lister
	TrustChecker device.TrustChecker
	Storage      storage.Client
	Apps         apps.Lister
	CrashLogs    crashlogs.Client
	Prober       apps.Prober
	ProbeStore   apps.ProbeStore
	Sandbox      sandbox.Sandbox

	// Now is the clock the destructive MCP handlers consult when
	// evaluating probe freshness (see probeTTLForMCPClean). Production
	// wires this to time.Now; tests override with a fixed value so the
	// TTL boundary can be exercised deterministically. nil means
	// "default to time.Now()".
	Now func() time.Time
}

// probeTTLForMCPClean is how recent a Vended probe must be for the MCP
// apps_clean handler to accept it. Five minutes is short enough that an
// LLM caller cannot run apps_probe in turn N and consume the result in
// turn N+M after the user has corrected its course, but long enough
// that a normal probe→inspect→clean workflow doesn't trip the gate.
//
// Intentionally const, NOT a flag or env var: the value is a safety
// contract, not a tuning knob. Changing it requires a code change so
// reviewers see the diff.
//
// CLI path is unaffected — the CLI runs interactively with a human in
// the loop, and the typed-bundle-ID gate + apps_probe re-run prompt
// already handle the same risk there.
const probeTTLForMCPClean = 5 * time.Minute

// probeVendedAndFresh searches results for the most-recent ProbeResult
// matching bundleID and reports whether it is (a) Vended AND (b) within
// probeTTLForMCPClean of now.
//
// Return signature: (fresh bool, age time.Duration, found bool). age is
// meaningful only when found is true. The caller uses age to build the
// "N minutes old" diagnostic message.
//
// Iteration order: newest-first by .At so the freshest matching entry
// wins. apps.ProbeStore on-disk results are sorted by BundleID, NOT
// timestamp; the per-bundle ordering is therefore arbitrary, which
// means we MUST scan by .At rather than assume positional newness.
func probeVendedAndFresh(results []apps.ProbeResult, bundleID string, now time.Time) (fresh bool, age time.Duration, found bool) {
	var newest apps.ProbeResult
	for _, r := range results {
		if r.BundleID != bundleID {
			continue
		}
		if !found || r.At.After(newest.At) {
			newest = r
			found = true
		}
	}
	if !found {
		return false, 0, false
	}
	if newest.Outcome != apps.ProbeVended {
		return false, now.Sub(newest.At), true
	}
	age = now.Sub(newest.At)
	return age < probeTTLForMCPClean, age, true
}

// deviceRow is the on-the-wire shape returned by devices_list. Mirrors
// the private deviceRow in cmd/ios-tidy/devices.go so MCP and CLI emit
// the same JSON keys for the same data.
type deviceRow struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	IOSVersion string `json:"iosVersion"`
	Trusted    bool   `json:"trusted"`
}

// probeRow is the on-the-wire shape returned by apps_probe. Mirrors
// the anonymous struct used by writeProbeJSON in cmd/ios-tidy/apps.go
// minus the per-bundle Name field (which would require an extra
// Apps.UserApps lookup we'd then drop — keep the result minimal so the
// MCP caller can correlate by bundleID).
type probeRow struct {
	BundleID    string    `json:"bundleID"`
	Outcome     string    `json:"outcome"`
	ErrorClass  string    `json:"errorClass,omitempty"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
	At          time.Time `json:"at"`
}

// jsonResult marshals v with two-space indent and wraps the result as
// an MCP text result. Marshal failures are surfaced as MCP error
// results rather than Go errors — the handler still returns nil for
// its error slot so the transport doesn't crash the connection.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

// resolveDeviceForTool wraps cmdutil.ResolveDevice for MCP handlers.
//
// Returns:
//   - (udid, nil, nil) on success. Caller proceeds.
//   - ("", result, nil) when the result is ready to return — either
//     because the device list is empty (success result with the text
//     "no devices attached") or because resolveDevice failed (error
//     result with the captured stderr + error message). Caller MUST
//     return that result with no further work.
//
// Error never returned in the second slot; the third return is reserved
// for future panic-recovery extensions.
func resolveDeviceForTool(ctx context.Context, deps serverDeps, override string) (string, *mcp.CallToolResult) {
	var stderr bytes.Buffer
	udid, err := cmdutil.ResolveDevice(ctx, deps.Lister, override, &stderr)
	if err == nil {
		return udid, nil
	}
	if errors.Is(err, cmdutil.ErrNoDevicesAttached) {
		// Non-error result: the LLM should see the human-readable
		// reason and pick its next move (e.g. ask the user to plug in
		// a phone). Returning IsError=true here would be misleading;
		// nothing failed except the precondition.
		return "", mcp.NewToolResultText("no devices attached")
	}
	msg := stderr.String()
	if msg != "" {
		msg += ": "
	}
	msg += err.Error()
	return "", mcp.NewToolResultError(msg)
}

// addReadOnlyTools registers every tool defined in this file on s. main
// calls this once during server boot.
func addReadOnlyTools(s mcpToolHost, deps serverDeps) {
	s.AddTool(
		mcp.NewTool("devices_list",
			mcp.WithDescription(`List iPhones currently connected over USB.

Args: none.

Returns: JSON array. Each element has keys udid, name, model
(Apple's productType identifier, e.g. "iPhone14,5"), iosVersion, and
trusted (whether the host machine has been paired/trusted by the
device). Untrusted devices may have empty name/model/iosVersion —
lockdown values are unreadable without a pair record.

Caveat: on macOS 26 (Tahoe) the trust check may fail with a TCC
"pair-record path denied" error. That is a host-side permission
problem, not a device problem; see go-ios issue #710.`),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		newDevicesListHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("storage",
			mcp.WithDescription(`Report device free/used storage plus per-app sizes.

Args:
  udid (optional string): target device UDID. If omitted and exactly
    one iPhone is attached, that device is used. If omitted and
    multiple are attached, the tool errors with a usage hint.
  limit (optional integer): keep only the top N apps by total bytes
    after sorting; 0 or negative means "all".

Returns: JSON object with keys device (afcTotalBytes, afcFreeBytes,
afcBlockSize, model) and apps (array of {bundleID, name, version,
container, dynamicBytes, staticBytes, fileSharingEnabled,
applicationType}). The afc* numbers come from AFC's deviceInfo and may
differ from iOS Settings by a few hundred MB.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithNumber("limit", mcp.Description("top N apps by total bytes; 0 or negative means all"), mcp.DefaultNumber(0)),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		newStorageHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("crashlogs_list",
			mcp.WithDescription(`List crash reports stored on the device.

Args:
  udid (optional string): target device UDID. See devices_list for
    selection rules.
  pattern (optional string): filepath.Match glob applied to
    filepath.Base of each entry path. Defaults to "*".

Returns: JSON array of {path, size, mtime}. mtime may be the zero
value ("0001-01-01T00:00:00Z") when go-ios does not surface st_mtime
for that entry — treat zero-mtime as "unknown".`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithString("pattern", mcp.Description("filepath.Match glob"), mcp.DefaultString("*")),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		newCrashLogsListHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("apps_list",
			mcp.WithDescription(`List installed user apps on the target iPhone with their reported sizes.

Args:
  udid (optional string): target device UDID. See devices_list for
    selection rules.

Returns: JSON array. Each element has keys bundleID, name, version,
container, dynamicBytes (current disk usage; may be 0 for cold apps
that haven't been launched recently — installation_proxy reports zero
until the app runs once), staticBytes (binary + sandbox baseline),
fileSharingEnabled, and applicationType. Sorted by total bytes
descending, bundleID ascending as tie-break.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		newAppsListHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("apps_probe",
			mcp.WithDescription(`Probe whether the device's mobile_house_arrest daemon will vend the
sandboxes for one or more apps. This is the gate that the future
apps_clean tool consults — only Vended apps can be sandbox-cleaned.

Args:
  udid (optional string): target device UDID.
  bundles (optional array of strings): bundle IDs to probe. Required
    unless all=true.
  all (optional bool): probe every installed user app. Mutually
    exclusive with bundles.
  timeout (optional string, Go duration): per-probe timeout. Default
    "5s". Increase if you see "timeout" outcomes on a slow device.

Returns: JSON array of {bundleID, outcome, errorClass, errorDetail,
at}. outcome is one of:
  - "vended": house_arrest accepted; sandbox-cleanable.
  - "refused": daemon refused; user must clean from Settings on-device.
  - "error": transport / pairing failure; retryable.
  - "unknown": app not installed or InstallationLookupFailed; no
    conclusion about daemon policy possible.

Known flakiness: iOS 17.1 occasionally returns spurious refused
results for apps that vend fine on a second attempt — re-running
apps_probe is the standard remedy (see RESEARCH.md §3). Results are
persisted to the per-UDID probe cache shared with the CLI; the next
apps_clean call will read from the same cache.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithArray("bundles",
				mcp.Description("bundle IDs to probe"),
				mcp.WithStringItems(),
			),
			mcp.WithBoolean("all", mcp.Description("probe every installed user app"), mcp.DefaultBool(false)),
			mcp.WithString("timeout", mcp.Description("per-probe timeout (Go duration)"), mcp.DefaultString("5s")),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		newAppsProbeHandler(deps),
	)
}

// mcpToolHost is the subset of *server.MCPServer that addReadOnlyTools
// uses. Defined as an interface so the registration sequence can be
// unit-tested without standing up a full stdio server.
type mcpToolHost interface {
	AddTool(t mcp.Tool, h server.ToolHandlerFunc)
}

// newDevicesListHandler returns the handler for the devices_list tool.
func newDevicesListHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		devs, err := deps.Lister.List(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list devices: %v", err)), nil
		}
		rows := make([]deviceRow, 0, len(devs))
		for _, d := range devs {
			trusted := false
			if deps.TrustChecker != nil {
				t, err := deps.TrustChecker.Trusted(ctx, d.UDID)
				if err != nil {
					// Surface the per-device trust failure but keep
					// going — a single bad pair record shouldn't kill
					// the whole listing. The LLM caller sees the
					// summary error string AND the row with
					// trusted=false, which is the same fail-soft UX
					// the CLI's table uses (it prints "untrusted"
					// rather than aborting).
					return mcp.NewToolResultError(fmt.Sprintf("trust check %s: %v", d.UDID, err)), nil
				}
				trusted = t
			}
			rows = append(rows, deviceRow{
				UDID:       d.UDID,
				Name:       d.Name,
				Model:      d.Model,
				IOSVersion: d.IOSVersion,
				Trusted:    trusted,
			})
		}
		return jsonResult(rows)
	}
}

// storagePayload mirrors the {device, apps} shape that
// cmd/ios-tidy/storage.go renderJSON emits. Defined as a named struct
// (vs anonymous) so the MCP test's json.Unmarshal target stays
// readable.
type storagePayload struct {
	Device storage.DeviceInfo `json:"device"`
	Apps   []apps.App         `json:"apps"`
}

// newStorageHandler returns the handler for the storage tool.
func newStorageHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		limit := req.GetInt("limit", 0)

		udid, resolved := resolveDeviceForTool(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}

		info, appList, err := fetchStorageInParallel(ctx, udid, deps.Storage, deps.Apps)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		apps.Sort(appList)
		appList = apps.Limit(appList, limit)
		return jsonResult(storagePayload{Device: info, Apps: appList})
	}
}

// fetchStorageInParallel mirrors fetchInParallel in cmd/ios-tidy/storage.go
// — duplicating the small two-goroutine fan-out is cheaper than
// promoting it to a shared helper, since the only difference is the
// returned error shape (we return a flat error rather than the CLI's
// fmt.Fprintf-then-exit pattern).
func fetchStorageInParallel(ctx context.Context, udid string, sc storage.Client, al apps.Lister) (storage.DeviceInfo, []apps.App, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		info    storage.DeviceInfo
		list    []apps.App
		infoErr error
		appsErr error
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		info, infoErr = sc.DeviceInfo(child, udid)
		if infoErr != nil {
			cancel()
		}
	}()
	go func() {
		defer wg.Done()
		list, appsErr = al.UserApps(child, udid)
		if appsErr != nil {
			cancel()
		}
	}()
	wg.Wait()

	if infoErr != nil {
		return storage.DeviceInfo{}, nil, fmt.Errorf("device info: %w", infoErr)
	}
	if appsErr != nil {
		return storage.DeviceInfo{}, nil, fmt.Errorf("user apps: %w", appsErr)
	}
	return info, list, nil
}

// newCrashLogsListHandler returns the handler for the crashlogs_list tool.
func newCrashLogsListHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		pattern := req.GetString("pattern", "*")

		udid, resolved := resolveDeviceForTool(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}

		entries, err := deps.CrashLogs.List(ctx, udid, pattern)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list crash logs: %v", err)), nil
		}
		// Apply MatchEntries defensively. The production adapter pushes
		// matching down to go-ios, but the fakes used in tests do not
		// filter — running the helper here keeps behaviour consistent
		// across seam implementations.
		entries, err = crashlogs.MatchEntries(entries, pattern)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bad pattern: %v", err)), nil
		}
		return jsonResult(entries)
	}
}

// newAppsListHandler returns the handler for the apps_list tool.
func newAppsListHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")

		udid, resolved := resolveDeviceForTool(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}

		list, err := deps.Apps.UserApps(ctx, udid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list apps: %v", err)), nil
		}
		apps.Sort(list)
		return jsonResult(list)
	}
}

// newAppsProbeHandler returns the handler for the apps_probe tool.
//
// Mirrors cmd/ios-tidy/apps.go runAppsProbe: validate args, resolve
// device, enumerate installed apps to filter probe targets, run
// probes sequentially (house_arrest is single-flight per device), and
// persist the results via the SAME ProbeStore the CLI uses (so the
// downstream apps_clean tool sees consistent cache).
func newAppsProbeHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		all := req.GetBool("all", false)
		bundles := req.GetStringSlice("bundles", nil)
		timeoutStr := req.GetString("timeout", "5s")

		timeout, terr := time.ParseDuration(timeoutStr)
		if terr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("bad timeout %q: %v", timeoutStr, terr)), nil
		}
		if !all && len(bundles) == 0 {
			return mcp.NewToolResultError("apps_probe: pass either all=true or a non-empty bundles array"), nil
		}
		if all && len(bundles) > 0 {
			return mcp.NewToolResultError("apps_probe: all and bundles are mutually exclusive"), nil
		}
		if deps.Prober == nil {
			return mcp.NewToolResultError("apps_probe: server has no Prober wired"), nil
		}

		udid, resolved := resolveDeviceForTool(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}

		installed, err := deps.Apps.UserApps(ctx, udid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("list apps: %v", err)), nil
		}
		installedByID := map[string]apps.App{}
		for _, a := range installed {
			installedByID[a.BundleID] = a
		}

		var targets []string
		if all {
			targets = make([]string, 0, len(installed))
			for _, a := range installed {
				targets = append(targets, a.BundleID)
			}
		} else {
			targets = append(targets, bundles...)
		}

		results := make([]apps.ProbeResult, 0, len(targets))
		now := time.Now().UTC()
		for _, bid := range targets {
			if _, ok := installedByID[bid]; !ok {
				results = append(results, apps.ProbeResult{
					BundleID: bid,
					Outcome:  apps.ProbeUnknown,
					Detail:   "not installed",
					At:       now,
				})
				continue
			}
			pctx, cancel := context.WithTimeout(ctx, timeout)
			res := deps.Prober.Probe(pctx, udid, bid)
			cancel()
			results = append(results, res)
		}

		// Persist via the SAME ProbeStore the CLI uses — apps_clean
		// (future) consults this cache to gate destructive ops. If
		// no store was wired (only happens in tests), skip the save
		// silently rather than erroring; the in-memory results are
		// still returned.
		if deps.ProbeStore != nil {
			if err := deps.ProbeStore.Save(udid, results); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("save probe results: %v", err)), nil
			}
		}

		rows := make([]probeRow, len(results))
		for i, r := range results {
			row := probeRow{
				BundleID: r.BundleID,
				Outcome:  r.Outcome.String(),
				At:       r.At,
			}
			// errorClass / errorDetail are only meaningful for
			// non-vended outcomes. Keep them omitempty so the wire
			// shape stays compact for the happy path.
			if r.Outcome != apps.ProbeVended && r.Detail != "" {
				row.ErrorClass = r.Outcome.String()
				row.ErrorDetail = r.Detail
			}
			rows[i] = row
		}
		return jsonResult(rows)
	}
}

// addDestructiveTools registers crashlogs_pull, crashlogs_clean, and
// apps_clean on s. Every destructive tool defaults to the SAFE path
// (dry-run / "would write" / refusal) and requires explicit arg-level
// confirmation to perform any state-changing operation. There is no
// --yes shortcut over the MCP transport: each safety gate is independently
// non-bypassable from the args.
func addDestructiveTools(s mcpToolHost, deps serverDeps) {
	s.AddTool(
		mcp.NewTool("crashlogs_pull",
			mcp.WithDescription(`Pull crash reports from the device to a directory on the HOST machine.

This is non-destructive on the device (it does NOT delete anything) but
DOES create files on the host machine running this MCP server. Confirm
the destination is acceptable before calling.

Args:
  udid (optional string): target device UDID. See devices_list rules.
  pattern (optional string): filepath.Match glob applied to filepath.Base
    of each entry. Default "*".
  out (REQUIRED string): destination directory on the host. Hard rules:
    - MUST be an absolute path.
    - MUST be a real directory (NOT a symlink — symlinks are refused to
      avoid TOCTOU / symlink-target confusion).
    - MUST live inside $HOME (or the IOS_TIDY_MCP_PULL_ROOT override).
    - MUST NOT be inside .ssh, .gnupg, Library/LaunchAgents,
      Library/LaunchDaemons, Library/Keychains, or Library/Cookies.
    The directory must already exist; this tool does not mkdir for you.
  force (optional bool): overwrite existing files at dst. Default false;
    matching files that already exist will surface as Pull failures.

Returns: JSON {pulled, bytes, dest, device} on success. The on-disk
layout preserves the device's relative paths under dest.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithString("pattern", mcp.Description("filepath.Match glob"), mcp.DefaultString("*")),
			mcp.WithString("out", mcp.Description("destination directory on the host (REQUIRED, absolute path inside $HOME, no symlinks, no sensitive subpaths)")),
			mcp.WithBoolean("force", mcp.Description("overwrite existing files"), mcp.DefaultBool(false)),
		),
		newCrashLogsPullHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("crashlogs_clean",
			mcp.WithDescription(`Delete crash reports on the device.

DESTRUCTIVE: this tool removes files from the device.

Safety: default behaviour is DRY-RUN. Pass confirm=true to actually
delete. Without confirm, the tool lists what would be deleted and
returns counts; no Remove call hits the device.

Args:
  udid (optional string): target device UDID.
  pattern (optional string): filepath.Match glob applied to filepath.Base
    of each entry. Default "*".
  confirm (optional bool, default false): MUST be explicitly true to
    actually delete. Any other value (omitted, false) yields a dry-run
    response.

Returns:
  dry-run: JSON {dryRun: true, wouldDelete, bytes, sample: [paths...]}
    where sample contains up to 10 representative paths.
  confirmed: JSON {dryRun: false, deleted, bytes, failures: [...]}.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithString("pattern", mcp.Description("filepath.Match glob"), mcp.DefaultString("*")),
			mcp.WithBoolean("confirm", mcp.Description("MUST be true to actually delete; otherwise dry-run"), mcp.DefaultBool(false)),
			mcp.WithDestructiveHintAnnotation(true),
		),
		newCrashLogsCleanHandler(deps),
	)

	s.AddTool(
		mcp.NewTool("apps_clean",
			mcp.WithDescription(`Delete sandbox files for one app on the device.

DESTRUCTIVE: this tool removes files inside an app's container on the
device. Three independent, non-bypassable safety gates:

  1. PROBE GATE: the bundle MUST have a Vended probe outcome on record
     (see apps_probe). If not, the tool refuses and tells you to run
     apps_probe first.

  2. TYPED-BUNDLE-ID GATE: to actually delete (dry_run=false), the caller
     MUST pass confirm_bundle_id equal to bundle_id (case-sensitive after
     TrimSpace). There is no --yes equivalent that bypasses this.

  3. DOCUMENTS ACKNOWLEDGMENT: include_documents=true requires BOTH the
     typed-bundle-ID match AND
     i_understand_documents_are_unrecoverable=true. User data deleted
     from Documents/ is not recoverable.

Default include combo: if none of include_tmp/include_caches/include_documents
are set, the tool defaults to tmp + caches (Documents is NEVER
auto-enabled).

Args:
  udid (optional string): target device UDID.
  bundle_id (REQUIRED string): bundle ID of the app whose sandbox to clean.
  confirm_bundle_id (string): re-state bundle_id; required to delete when
    dry_run is false.
  include_tmp (optional bool): include tmp/.
  include_caches (optional bool): include Library/Caches/.
  include_documents (optional bool): include Documents/. Requires extra ack.
  i_understand_documents_are_unrecoverable (optional bool): MUST be true
    to actually delete Documents/.
  dry_run (optional bool, default true): when true, returns plans only;
    no Sandbox.Open / no file deletion.

Returns:
  dry-run: JSON {dryRun: true, bundleID, plans: [{target, totalBytes,
    fileCount, sample}], totalBytes}.
  confirmed: JSON {dryRun: false, bundleID, results: [{target, deleted,
    bytes, failures}], totalBytesFreed}.`),
			mcp.WithString("udid", mcp.Description("target device UDID")),
			mcp.WithString("bundle_id", mcp.Description("bundle ID of the app (REQUIRED)")),
			mcp.WithString("confirm_bundle_id", mcp.Description("must equal bundle_id to delete (case-sensitive after TrimSpace)")),
			mcp.WithBoolean("include_tmp", mcp.Description("include tmp/"), mcp.DefaultBool(false)),
			mcp.WithBoolean("include_caches", mcp.Description("include Library/Caches/"), mcp.DefaultBool(false)),
			mcp.WithBoolean("include_documents", mcp.Description("include Documents/; requires ack"), mcp.DefaultBool(false)),
			mcp.WithBoolean("i_understand_documents_are_unrecoverable", mcp.Description("explicit acknowledgement for include_documents"), mcp.DefaultBool(false)),
			mcp.WithBoolean("dry_run", mcp.Description("default true; pass false to delete"), mcp.DefaultBool(true)),
			mcp.WithDestructiveHintAnnotation(true),
		),
		newAppsCleanHandler(deps),
	)
}

// crashlogsCleanResult is the wire-level JSON shape returned by
// crashlogs_clean. Two modes share one struct via the DryRun field:
//   - dry-run: DryRun=true; WouldDelete/Bytes/Sample populated; Deleted/Failures zero.
//   - confirmed: DryRun=false; Deleted/Bytes/Failures populated; WouldDelete/Sample zero.
type crashlogsCleanResult struct {
	DryRun      bool                `json:"dryRun"`
	Device      deviceRef           `json:"device"`
	WouldDelete int                 `json:"wouldDelete,omitempty"`
	Sample      []string            `json:"sample,omitempty"`
	Deleted     int                 `json:"deleted,omitempty"`
	Bytes       int64               `json:"bytes"`
	Failures    []crashlogs.Failure `json:"failures,omitempty"`
}

// deviceRef is a compact UDID+name pair stamped into every destructive
// tool's JSON result. The name comes from the same lookup the device
// picker would surface; if the lister is unable to recover it (e.g. an
// untrusted device with no readable lockdown values), Name is the empty
// string and the caller still sees the UDID.
//
// Echoing the friendly name closes a confused-deputy gap: an LLM caller
// that resolves "Anh's iPhone 14 Pro" to a UDID via devices_list can now
// confirm that the destructive op landed on the same device, not a
// substituted one a prompt-injection has slipped into a tool argument.
type deviceRef struct {
	UDID string `json:"udid"`
	Name string `json:"name"`
}

// resolveDeviceRef returns (udid, name, resolved). It mirrors
// resolveDeviceForTool but also looks up the friendly name from the
// lister's List output. The MCP layer pays the cost of a second List
// call so that destructive tool results can echo the device name without
// changing cmdutil.ResolveDevice's signature (CLI does not need this).
//
// When the lister cannot enumerate (the override-path fallback in
// cmdutil.ResolveDevice), Name is left empty — UDID is still returned so
// the caller can correlate by the canonical identifier.
func resolveDeviceRef(ctx context.Context, deps serverDeps, override string) (string, string, *mcp.CallToolResult) {
	udid, resolved := resolveDeviceForTool(ctx, deps, override)
	if resolved != nil {
		return "", "", resolved
	}
	name := ""
	if deps.Lister != nil {
		if devs, err := deps.Lister.List(ctx); err == nil {
			for _, d := range devs {
				if d.UDID == udid {
					name = d.Name
					break
				}
			}
		}
	}
	return udid, name, nil
}

// isAppleSystemBundle reports whether bundleID is a com.apple.* system
// app (case-sensitive prefix match including the trailing dot). A bare
// "com.apple" is NOT matched — Apple's real bundles always have a third
// reverse-DNS segment. Used as a defense-in-depth reject independent
// of (and redundant with) the probe gate.
func isAppleSystemBundle(bundleID string) bool {
	return strings.HasPrefix(bundleID, "com.apple.")
}

// isPrintableASCII reports whether every rune in s is in the printable
// ASCII range [0x20, 0x7E]. Apple bundle IDs are reverse-DNS; the rule
// is "ASCII letters, digits, hyphen, and dot". Anything outside that
// strict range (Cyrillic homoglyphs, NULs, RTL overrides, smart quotes)
// is either a typo or an injection attempt — refuse before any device
// I/O so the gate cannot be tricked by a Unicode lookalike of the
// expected bundle ID.
func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// firstNonASCIIRune returns the first non-printable-ASCII rune in s,
// suitable for embedding in an error message with %U so the caller (and
// auditor) sees exactly which codepoint triggered the refusal.
func firstNonASCIIRune(s string) rune {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return r
		}
	}
	return 0
}

// dryRunArgConservative reads dry_run from the request without using
// mcp-go's GetBool, which coerces the JSON string "false" to bool false
// via strconv.ParseBool. For a destructive tool the safe behaviour is:
// missing → true, real bool → its value, anything else (string, number,
// null) → true. There is no scenario where a non-bool dry_run should
// silently disarm the default.
func dryRunArgConservative(req mcp.CallToolRequest) bool {
	v, ok := req.GetArguments()["dry_run"]
	if !ok {
		return true
	}
	if b, isBool := v.(bool); isBool {
		return b
	}
	return true
}

// newCrashLogsCleanHandler returns the handler for crashlogs_clean. Default
// behaviour is dry-run; only confirm=true reaches Client.Remove.
func newCrashLogsCleanHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		pattern := req.GetString("pattern", "*")
		confirm := req.GetBool("confirm", false)

		udid, devName, resolved := resolveDeviceRef(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}
		devRef := deviceRef{UDID: udid, Name: devName}

		if !confirm {
			entries, err := deps.CrashLogs.List(ctx, udid, pattern)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("list crash logs: %v", err)), nil
			}
			entries, err = crashlogs.MatchEntries(entries, pattern)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("bad pattern: %v", err)), nil
			}
			var bytes int64
			sample := make([]string, 0, 10)
			for i, e := range entries {
				bytes += e.Size
				if i < 10 {
					sample = append(sample, e.Path)
				}
			}
			return jsonResult(crashlogsCleanResult{
				DryRun:      true,
				Device:      devRef,
				WouldDelete: len(entries),
				Bytes:       bytes,
				Sample:      sample,
			})
		}

		res, err := deps.CrashLogs.Remove(ctx, udid, pattern)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("remove crash logs: %v", err)), nil
		}
		return jsonResult(crashlogsCleanResult{
			DryRun:   false,
			Device:   devRef,
			Deleted:  res.Removed,
			Bytes:    res.Bytes,
			Failures: res.Failures,
		})
	}
}

// crashlogsPullResult is the wire-level JSON shape returned by crashlogs_pull.
type crashlogsPullResult struct {
	Pulled   int                 `json:"pulled"`
	Bytes    int64               `json:"bytes"`
	Dest     string              `json:"dest"`
	Device   deviceRef           `json:"device"`
	Failures []crashlogs.Failure `json:"failures,omitempty"`
}

// pullOutSensitiveSubpaths is the deny-list of $HOME-relative paths
// crashlogs_pull will refuse to write into even when the allow-root
// check would otherwise accept them. Each entry is a Clean'd path with
// forward-slash separators; the runtime check uses filepath.ToSlash on
// the candidate so the comparison works on Windows too even though the
// production target is macOS.
//
// Order doesn't matter — the candidate is matched against all entries
// with prefix-or-exact semantics. Add new entries as new attack surface
// is identified (e.g. ".aws", "Library/Mobile Documents" for iCloud).
var pullOutSensitiveSubpaths = []string{
	".ssh",
	".gnupg",
	"Library/LaunchAgents",
	"Library/LaunchDaemons",
	"Library/Keychains",
	"Library/Cookies",
}

// validatePullOutPath enforces a strict allow-root + deny-list contract:
//
//  1. out must be non-empty and absolute (filepath.IsAbs).
//  2. out, after Clean, must contain no ".." segment (defensive — Clean
//     already resolves these, but a literal ".." in the input is a sign
//     the caller constructed the path incorrectly).
//  3. out must live inside the allow-root. The root is $HOME by default,
//     overridable via the IOS_TIDY_MCP_PULL_ROOT env var (tests use the
//     override to point at t.TempDir() so they never depend on $HOME's
//     real layout).
//  4. out must not be (or be inside) one of the sensitive subpaths under
//     the allow-root: .ssh, .gnupg, Library/LaunchAgents, etc.
//  5. out must exist and be a real directory. Lstat (NOT Stat) is used
//     so a symlink IS detected and rejected; following it via Stat would
//     mask a symlink-to-/etc that bypasses the deny-list.
//
// Symlink rejection is intentionally absolute: even a symlink whose
// target is also inside the allow-root is refused, because a TOCTOU
// adversary can swap the target between the check and the downstream
// Pull. Real directories don't have this problem.
func validatePullOutPath(out string) error {
	if out == "" {
		return errors.New("crashlogs_pull: out is required (absolute path inside $HOME)")
	}
	if !filepath.IsAbs(out) {
		return fmt.Errorf("crashlogs_pull: out %q must be an absolute path", out)
	}
	cleaned := filepath.Clean(out)
	for _, seg := range strings.Split(cleaned, string(filepath.Separator)) {
		if seg == ".." {
			return fmt.Errorf("crashlogs_pull: out %q contains '..' segment", out)
		}
	}

	root, err := pullOutAllowRoot()
	if err != nil {
		return fmt.Errorf("crashlogs_pull: %w", err)
	}
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("crashlogs_pull: out %q is outside the allow-root %q", cleaned, root)
	}

	relSlash := filepath.ToSlash(rel)
	for _, deny := range pullOutSensitiveSubpaths {
		if relSlash == deny || strings.HasPrefix(relSlash, deny+"/") {
			return fmt.Errorf(
				"crashlogs_pull: out %q is inside a sensitive subpath (%s); refusing",
				cleaned, deny,
			)
		}
	}

	info, err := os.Lstat(cleaned)
	if err != nil {
		return fmt.Errorf("crashlogs_pull: out %q: %w", cleaned, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"crashlogs_pull: out %q is a symlink; refusing to avoid TOCTOU / "+
				"symlink-target confusion. Pass a real directory.",
			cleaned,
		)
	}
	if !info.IsDir() {
		return fmt.Errorf("crashlogs_pull: out %q is not a directory", cleaned)
	}
	return nil
}

// pullOutAllowRoot returns the directory crashlogs_pull's out must live
// inside. Production: $HOME. Tests can override via the
// IOS_TIDY_MCP_PULL_ROOT env var, which avoids both depending on the
// developer's actual $HOME contents and accidentally writing to it.
//
// The env override is intentionally NOT documented as a user-facing
// feature — it exists solely so the tests can exercise the allow-root
// logic without polluting $HOME, and so power users running the server
// inside a sandbox can point it at the sandbox-writable root.
func pullOutAllowRoot() (string, error) {
	if root := os.Getenv("IOS_TIDY_MCP_PULL_ROOT"); root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve $HOME: %w", err)
	}
	return home, nil
}

// newCrashLogsPullHandler returns the handler for crashlogs_pull.
func newCrashLogsPullHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		pattern := req.GetString("pattern", "*")
		out := req.GetString("out", "")
		// `force` is accepted for parity with the CLI flag; go-ios's
		// DownloadReports does not currently expose a "force overwrite"
		// option, so the value is recorded in the description for forward
		// compatibility and otherwise ignored — same effective behaviour
		// as the CLI's --force when go-ios reports overwrite failures.
		_ = req.GetBool("force", false)

		if err := validatePullOutPath(out); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		udid, devName, resolved := resolveDeviceRef(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}

		res, err := deps.CrashLogs.Pull(ctx, udid, pattern, out)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("pull crash logs: %v", err)), nil
		}
		return jsonResult(crashlogsPullResult{
			Pulled:   res.Pulled,
			Bytes:    res.Bytes,
			Dest:     out,
			Device:   deviceRef{UDID: udid, Name: devName},
			Failures: res.Failures,
		})
	}
}

// appsCleanPlanRow is one element in the dry-run plans array.
type appsCleanPlanRow struct {
	Target     string   `json:"target"`
	TotalBytes int64    `json:"totalBytes"`
	FileCount  int      `json:"fileCount"`
	Sample     []string `json:"sample,omitempty"`
}

// appsCleanDryRunResult is the wire-level shape for dry-run apps_clean output.
type appsCleanDryRunResult struct {
	DryRun     bool               `json:"dryRun"`
	Device     deviceRef          `json:"device"`
	BundleID   string             `json:"bundleID"`
	Plans      []appsCleanPlanRow `json:"plans"`
	TotalBytes int64              `json:"totalBytes"`
}

// appsCleanResultRow is one element in the confirmed-run results array.
type appsCleanResultRow struct {
	Target   string            `json:"target"`
	Deleted  int               `json:"deleted"`
	Bytes    int64             `json:"bytes"`
	Failures []sandbox.Failure `json:"failures,omitempty"`
}

// appsCleanConfirmedResult is the wire-level shape for confirmed apps_clean output.
type appsCleanConfirmedResult struct {
	DryRun          bool                 `json:"dryRun"`
	Device          deviceRef            `json:"device"`
	BundleID        string               `json:"bundleID"`
	Results         []appsCleanResultRow `json:"results"`
	TotalBytesFreed int64                `json:"totalBytesFreed"`
}

// probeVendedInResults reports whether results contains a Vended outcome for
// bundleID. The latest entry wins by iterating in order — same semantics as
// the CLI's probeVended helper.
func probeVendedInResults(results []apps.ProbeResult, bundleID string) bool {
	vended := false
	for _, r := range results {
		if r.BundleID != bundleID {
			continue
		}
		vended = r.Outcome == apps.ProbeVended
	}
	return vended
}

// newAppsCleanHandler returns the handler for apps_clean.
//
// Order of operations (each gate independent and non-bypassable):
//  1. Validate args. bundle_id required.
//  2. Compute the include-target set (default tmp+caches if none set).
//  3. Resolve the target device UDID.
//  4. Probe-gate: refuse unless ProbeStore reports Vended for this bundle.
//  5. When dry_run=false:
//     - require confirm_bundle_id == bundle_id (case-sensitive after TrimSpace),
//     - if include_documents, additionally require
//     i_understand_documents_are_unrecoverable == true.
//  6. Sandbox.Open, BuildPlan per target.
//  7. If dry_run: return plans, no Execute.
//  8. Otherwise: Execute per target, aggregate results.
func newAppsCleanHandler(deps serverDeps) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		override := req.GetString("udid", "")
		bundleID := req.GetString("bundle_id", "")
		confirmBundleID := req.GetString("confirm_bundle_id", "")
		includeTmp := req.GetBool("include_tmp", false)
		includeCaches := req.GetBool("include_caches", false)
		includeDocs := req.GetBool("include_documents", false)
		ackDocs := req.GetBool("i_understand_documents_are_unrecoverable", false)
		// dry_run defaults to TRUE — the safe default. mcp-go's GetBool
		// coerces JSON strings via strconv.ParseBool (verified against
		// v0.54.0/mcp/tools.go), so dry_run: "false" (string) would
		// silently disarm via the typed accessor. Use a conservative
		// reader that ONLY accepts a real bool; anything else (string,
		// number, null) falls back to the safe default of true.
		dryRun := dryRunArgConservative(req)

		if bundleID == "" {
			return mcp.NewToolResultError("apps_clean: bundle_id is required"), nil
		}

		// H-2 defense in depth: hard-reject com.apple.* before anything
		// else. ios-tidy is for third-party apps only; a probe-cache
		// race / hand-edited file / test bug must never let an MCP
		// caller delete a system app's sandbox files.
		if isAppleSystemBundle(bundleID) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: refusing to clean system app sandbox: %q. ios-tidy is for third-party apps only.",
				bundleID,
			)), nil
		}

		// H-1: refuse non-printable-ASCII in bundle_id and confirm_bundle_id.
		// A Cyrillic 'а' (U+0430) is byte-different from ASCII 'a' but renders
		// identically; if both bundle_id and confirm_bundle_id share the same
		// homoglyph the typed-bundle-ID gate passes by string equality while
		// the destructive op targets a different (or non-existent) bundle.
		// Apple bundle IDs are reverse-DNS, always printable ASCII.
		if !isPrintableASCII(bundleID) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: bundle_id contains non-printable-ASCII rune %U; refusing for safety. "+
					"Apple bundle IDs are reverse-DNS (ASCII letters, digits, '-', '.').",
				firstNonASCIIRune(bundleID),
			)), nil
		}
		if confirmBundleID != "" && !isPrintableASCII(confirmBundleID) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: confirm_bundle_id contains non-printable-ASCII rune %U; refusing for safety.",
				firstNonASCIIRune(confirmBundleID),
			)), nil
		}

		// Default include-flag combo: tmp + caches when none of include_* set.
		if !includeTmp && !includeCaches && !includeDocs {
			includeTmp = true
			includeCaches = true
		}

		// Args-first validation for the destructive path. We MUST refuse
		// before any device I/O so the test's trap-sandbox / unset deps
		// don't get exercised on the abort branches.
		if !dryRun {
			if strings.TrimSpace(confirmBundleID) != bundleID {
				return mcp.NewToolResultError(
					"apps_clean: confirm_bundle_id must match bundle_id exactly to delete.",
				), nil
			}
			if includeDocs && !ackDocs {
				return mcp.NewToolResultError(
					"apps_clean: include_documents requires i_understand_documents_are_unrecoverable=true. " +
						"Documents/ contents are NOT recoverable.",
				), nil
			}
		}

		udid, devName, resolved := resolveDeviceRef(ctx, deps, override)
		if resolved != nil {
			return resolved, nil
		}
		devRef := deviceRef{UDID: udid, Name: devName}

		// Probe gate. Refuse unless the bundle has a Vended probe on record
		// AND that probe is within the freshness TTL. Same-turn (or near
		// same-turn) probe→clean is the legitimate workflow; a stale Vended
		// result indicates the LLM has been steered to skip a re-probe a
		// human would have run, or that the device's daemon policy may have
		// changed since the probe (iOS 17 sporadic-refusal territory, see
		// RESEARCH.md §3).
		if deps.ProbeStore == nil {
			return mcp.NewToolResultError("apps_clean: server has no ProbeStore wired"), nil
		}
		results, err := deps.ProbeStore.Load(udid)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("load probe store: %v", err)), nil
		}
		// Belt-and-suspenders: re-check the system-app reject after the
		// probe store loads. Defends against a stale or hand-crafted
		// probe file that claimed a com.apple.* bundle was Vended.
		if isAppleSystemBundle(bundleID) {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: refusing to clean system app sandbox: %q. ios-tidy is for third-party apps only.",
				bundleID,
			)), nil
		}
		now := time.Now()
		if deps.Now != nil {
			now = deps.Now()
		}
		fresh, age, found := probeVendedAndFresh(results, bundleID, now)
		if !found {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: bundle %q has not been confirmed as vended on device %s. "+
					"Run the apps_probe tool with bundles=[%q] first.",
				bundleID, udid, bundleID,
			)), nil
		}
		if !fresh {
			// Found a matching result, but either non-vended or stale.
			// Distinguish the two so the LLM caller knows whether to
			// re-probe (stale Vended) or stop trying (Refused/Error).
			if !probeVendedInResults(results, bundleID) {
				return mcp.NewToolResultError(fmt.Sprintf(
					"apps_clean: bundle %q has not been confirmed as vended on device %s. "+
						"Run the apps_probe tool with bundles=[%q] first.",
					bundleID, udid, bundleID,
				)), nil
			}
			// Stale Vended.
			ageMin := int(age / time.Minute)
			limitMin := int(probeTTLForMCPClean / time.Minute)
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: probe result for %q is %d minutes old (>=%d minute limit). "+
					"Re-run apps_probe with bundles=[%q] first.",
				bundleID, ageMin, limitMin, bundleID,
			)), nil
		}

		if deps.Sandbox == nil {
			return mcp.NewToolResultError("apps_clean: server has no Sandbox wired"), nil
		}
		fsHandle, err := deps.Sandbox.Open(ctx, udid, bundleID)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf(
				"apps_clean: open sandbox for %q on %s: %v. "+
					"The probe store reports vended, but the daemon now refuses; "+
					"re-run apps_probe to refresh.",
				bundleID, udid, err,
			)), nil
		}
		defer fsHandle.Close()

		// Build the target list in a stable order so the JSON output is
		// deterministic for callers and tests.
		var targets []sandbox.Target
		if includeTmp {
			targets = append(targets, sandbox.TargetTmp)
		}
		if includeCaches {
			targets = append(targets, sandbox.TargetCaches)
		}
		if includeDocs {
			targets = append(targets, sandbox.TargetDocuments)
		}

		plans := make([]sandbox.CleanPlan, 0, len(targets))
		for _, tg := range targets {
			p, err := sandbox.BuildPlan(ctx, fsHandle, tg)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("build plan for %s: %v", tg.Name, err)), nil
			}
			plans = append(plans, p)
		}

		if dryRun {
			rows := make([]appsCleanPlanRow, 0, len(plans))
			var total int64
			for _, p := range plans {
				sample := make([]string, 0, 10)
				for i, fi := range p.Files {
					if i >= 10 {
						break
					}
					sample = append(sample, fi.Path)
				}
				rows = append(rows, appsCleanPlanRow{
					Target:     p.Target.Name,
					TotalBytes: p.TotalBytes,
					FileCount:  len(p.Files),
					Sample:     sample,
				})
				total += p.TotalBytes
			}
			return jsonResult(appsCleanDryRunResult{
				DryRun:     true,
				Device:     devRef,
				BundleID:   bundleID,
				Plans:      rows,
				TotalBytes: total,
			})
		}

		// Destructive boundary reached. All three gates have cleared
		// (probe, typed-bundle-ID, documents-ack-if-applicable).
		resultRows := make([]appsCleanResultRow, 0, len(plans))
		var totalFreed int64
		for _, p := range plans {
			r := sandbox.Execute(ctx, fsHandle, p)
			resultRows = append(resultRows, appsCleanResultRow{
				Target:   r.Target.Name,
				Deleted:  r.Removed,
				Bytes:    r.Bytes,
				Failures: r.Failures,
			})
			totalFreed += r.Bytes
		}
		return jsonResult(appsCleanConfirmedResult{
			DryRun:          false,
			Device:          devRef,
			BundleID:        bundleID,
			Results:         resultRows,
			TotalBytesFreed: totalFreed,
		})
	}
}

// defaultProbeStoreDir returns the same default location used by the
// CLI's `apps probe` subcommand. Sharing the path means CLI and MCP
// see the same per-UDID probe cache files.
func defaultProbeStoreDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "ios-tidy", "probes"), nil
}
