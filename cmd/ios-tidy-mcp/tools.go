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
