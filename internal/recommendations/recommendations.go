// Package recommendations is a pure (no I/O) synthesis layer that turns
// the raw outputs of the other ios-tidy seams — storage info, app list,
// crashlog total, probe results — into a prioritized, human-actionable
// "what to free, how" plan.
//
// The package is deliberately I/O-free: the MCP handler fetches the
// inputs (via storage.Client, apps.Lister, crashlogs.Client,
// apps.ProbeStore) and passes them to Build. Determinism makes the
// behaviour table-testable and the JSON output stable across runs.
//
// Tone: the recommendations are addressed at an LLM caller that will
// then surface them to a human. They MUST be accurate about what
// ios-tidy can and cannot touch — the notTouchable section is the
// load-bearing UX bit that prevents the LLM from promising the user
// "I'll clean Photos / Safari / system caches" (we can't; nothing on
// the host side can reach those).
package recommendations

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anh-pham191/ios-tidy/internal/apps"
)

// Priority is the ordinal urgency of a recommendation. Encoded as a
// stringly-typed enum on the wire so MCP consumers can render it
// directly without translating ints.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// rank gives a sort key for priorities. Higher number = higher
// priority (so descending sort produces high → medium → low).
func (p Priority) rank() int {
	switch p {
	case PriorityHigh:
		return 3
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 1
	}
	return 0
}

// Action enumerates the (small, closed) set of remediations the
// synthesis layer can suggest. Closed-set on purpose: every Action
// MUST be reachable via either an existing ios-tidy MCP tool or a
// user-on-device step that we can describe accurately.
type Action string

const (
	ActionCleanCrashlogs  Action = "clean_crashlogs"
	ActionOffloadApp      Action = "offload_app"
	ActionUninstallApp    Action = "uninstall_app"
	ActionCleanAppSandbox Action = "clean_app_sandbox"
	ActionGenericReview   Action = "review_unused_apps"
)

// ViaTool names the MCP tool (if any) that performs the action.
// Empty string means "no tool — the human must do this on-device".
type ViaTool string

const (
	ViaCrashlogsClean         ViaTool = "crashlogs_clean"
	ViaAppsClean              ViaTool = "apps_clean"
	ViaOpenAppStorageSettings ViaTool = "open_app_storage_settings"
	ViaNone                   ViaTool = ""
)

// perAppRecThreshold is the minimum app size that justifies a per-app
// recommendation. Below 50 MiB an offload/clean rec is noise — even if
// you act on it, you free roughly nothing. The generic
// "review unused apps" fallback covers the small-apps-everywhere case
// instead.
const perAppRecThreshold int64 = 50 * 1024 * 1024

// crashlogThreshold is the minimum total crashlog byte count that
// triggers a high-priority recommendation. Below this it's noise and
// not worth pestering the user about. 5 MiB is arbitrary but empirically
// matches what a non-crashing device accumulates over a few months.
const crashlogThreshold int64 = 5 * 1024 * 1024

// topAppsByTotal is how many of the biggest user apps we will surface
// per category (offload vs sandbox-clean). Keeping it modest stops the
// recommendation list from drowning the LLM caller's context window.
const topAppsByTotal = 5

// largeAppThreshold is the "what counts as a big app" cutoff used by
// the no-big-apps fallback. 1 GiB. If no installed app is at least
// this large and the device is still low-free, we surface the generic
// "consider uninstalling unused apps" suggestion because there's no
// single fat target.
const largeAppThreshold uint64 = 1 * 1024 * 1024 * 1024

// Storage label thresholds. percentFree < lowThreshold means "low",
// percentFree < normalThreshold means "normal", otherwise "high".
//
// Boundaries are inclusive of the lower bound and exclusive of the
// upper: 9.999% is low, 10% is normal; 24.999% is normal, 25% is high.
// Documented in the test table.
const (
	lowThreshold    = 10.0
	normalThreshold = 25.0
)

// Device is the {udid, name} stamp echoed at the top of the
// recommendations payload so the MCP caller can correlate the plan
// with the device they targeted via devices_list.
type Device struct {
	UDID string `json:"udid"`
	Name string `json:"name"`
}

// Summary is the at-a-glance numeric block. percentFree is computed
// against TotalBytes so a zero-Total device (impossible in production,
// possible in tests) produces percentFree=0 and label="low" rather
// than NaN.
type Summary struct {
	FreeBytes   uint64  `json:"freeBytes"`
	TotalBytes  uint64  `json:"totalBytes"`
	PercentFree float64 `json:"percentFree"`
	Label       string  `json:"label"`
}

// Recommendation is one row of the action plan.
//
// EstimatedRecoverBytes is conservative — it's the upper bound of what
// the action could free if the user follows through. The synthesis
// layer does not know how much of an app's bytes are caches vs user
// data, so the estimate equals the app's total bytes. The accompanying
// Rationale calls out the uncertainty.
type Recommendation struct {
	Priority              Priority `json:"priority"`
	Action                Action   `json:"action"`
	BundleID              string   `json:"bundleID,omitempty"`
	AppName               string   `json:"appName,omitempty"`
	EstimatedRecoverBytes int64    `json:"estimatedRecoverBytes"`
	Rationale             string   `json:"rationale"`
	ViaTool               ViaTool  `json:"viaTool,omitempty"`
}

// NotTouchable is the "things this tool CANNOT reach" disclosure. The
// LLM caller is meant to surface this to the user so it doesn't make
// false promises about Photos.app, Safari caches, the iOS "Other"
// bucket, etc. — none of which any USB-attached host tool can clean.
//
// Constant text (no per-device customisation) so the wording stays
// reviewed and consistent. If we discover a new untouchable category,
// add a field here so it's surfaced uniformly.
type NotTouchable struct {
	SystemData       string `json:"systemData"`
	Photos           string `json:"photos"`
	MusicAndPodcasts string `json:"musicAndPodcasts"`
}

// Payload is the top-level shape returned by Build (and ultimately
// marshalled as the storage_recommendations tool's JSON result).
type Payload struct {
	Device          Device           `json:"device"`
	Summary         Summary          `json:"summary"`
	Recommendations []Recommendation `json:"recommendations"`
	NotTouchable    NotTouchable     `json:"notTouchable"`
}

// Inputs bundles the raw data Build needs. Constructing this in the
// handler keeps Build itself pure and table-testable.
type Inputs struct {
	UDID          string
	DeviceName    string
	FreeBytes     uint64
	TotalBytes    uint64
	Apps          []apps.App
	CrashlogBytes int64
	ProbeResults  []apps.ProbeResult
}

// staticNotTouchable returns the constant disclosure block. Function
// (not a package-level var) so callers can't mutate it.
func staticNotTouchable() NotTouchable {
	return NotTouchable{
		SystemData: "iOS does not expose system caches, Safari/WebKit, Mail, or the 'Other' bucket to " +
			"USB-attached host tools. See Settings → General → iPhone Storage on the device itself.",
		Photos: "Photos cannot be safely deleted via this tool (would corrupt Photos.sqlite). " +
			"Use the Photos app's 'Optimize iPhone Storage' setting, or delete albums on-device.",
		MusicAndPodcasts: "Downloaded media is reachable on disk but is tied to a CoreData database " +
			"this tool cannot update — deletion would create orphaned references. Manage from " +
			"the Music or Podcasts app on-device.",
	}
}

// classifyLabel returns "low" / "normal" / "high" from percentFree
// using the documented boundaries.
func classifyLabel(percentFree float64) string {
	switch {
	case percentFree < lowThreshold:
		return "low"
	case percentFree < normalThreshold:
		return "normal"
	default:
		return "high"
	}
}

// isAppleSystem mirrors the cmd/ios-tidy-mcp/tools.go reject. Kept as a
// package-private helper here so this package stays I/O-free and has
// no cross-cmd imports. (Sharing would require lifting the helper into
// internal/cmdutil; not worth it for one one-liner.)
func isAppleSystem(bundleID string) bool {
	return strings.HasPrefix(bundleID, "com.apple.")
}

// isVendedInResults returns the most-recent Vended status for bundleID.
// Order-insensitive — scans the slice and tracks the freshest .At.
// Returns false when bundleID was never probed, or when the freshest
// probe is non-Vended.
func isVendedInResults(results []apps.ProbeResult, bundleID string) bool {
	var (
		found  bool
		vended bool
		bestAt = int64(-1)
	)
	for _, r := range results {
		if r.BundleID != bundleID {
			continue
		}
		ts := r.At.UnixNano()
		if !found || ts > bestAt {
			found = true
			vended = r.Outcome == apps.ProbeVended
			bestAt = ts
		}
	}
	return vended
}

// total returns DynamicBytes + StaticBytes as int64 for arithmetic with
// EstimatedRecoverBytes (int64 to match the JSON shape). The cast is
// safe in practice; an app larger than 2^63 bytes is implausible.
func total(a apps.App) int64 {
	return int64(a.DynamicBytes + a.StaticBytes)
}

// Build is the pure synthesis function. Given Inputs, it returns the
// fully-populated Payload. Deterministic: same inputs → same output.
//
// The function does NOT mutate Inputs.Apps. It copies internally before
// sorting so the caller's slice ordering is preserved (the storage
// tool sorts in-place via apps.Sort, which is a different downstream
// invariant we don't want to disturb here).
func Build(in Inputs) Payload {
	var percentFree float64
	if in.TotalBytes > 0 {
		percentFree = 100.0 * float64(in.FreeBytes) / float64(in.TotalBytes)
	}
	summary := Summary{
		FreeBytes:   in.FreeBytes,
		TotalBytes:  in.TotalBytes,
		PercentFree: percentFree,
		Label:       classifyLabel(percentFree),
	}

	// Defensive copy + sort by total bytes descending. We can't use
	// apps.Sort directly because it mutates in place; for a pure
	// function we have to copy first.
	sorted := make([]apps.App, len(in.Apps))
	copy(sorted, in.Apps)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := total(sorted[i])
		tj := total(sorted[j])
		if ti != tj {
			return ti > tj
		}
		return sorted[i].BundleID < sorted[j].BundleID
	})

	recs := make([]Recommendation, 0, 8)

	// Crashlogs first, regardless of free-space label. Crash reports
	// are pure overhead — there's no user-facing benefit to keeping
	// them on the device once they've been pulled. 5 MiB threshold
	// avoids nagging on a healthy device.
	if in.CrashlogBytes >= crashlogThreshold {
		recs = append(recs, Recommendation{
			Priority:              PriorityHigh,
			Action:                ActionCleanCrashlogs,
			EstimatedRecoverBytes: in.CrashlogBytes,
			Rationale: fmt.Sprintf(
				"%.1f MB of crash reports are stored on the device. These are diagnostics-only — "+
					"safe to delete after pulling any you want to keep.",
				float64(in.CrashlogBytes)/(1024.0*1024.0),
			),
			ViaTool: ViaCrashlogsClean,
		})
	}

	// Partition apps. We want the top N candidates per category. A
	// candidate must be a non-Apple bundle with at least 1 byte
	// reported — apps with 0 sizes are cold (installation_proxy
	// hasn't updated) and would generate noise recommendations.
	var (
		offloadCount int
		sandboxCount int
	)
	for _, a := range sorted {
		if isAppleSystem(a.BundleID) {
			continue
		}
		bytes := total(a)
		if bytes < perAppRecThreshold {
			continue
		}
		if isVendedInResults(in.ProbeResults, a.BundleID) {
			if sandboxCount >= topAppsByTotal {
				continue
			}
			sandboxCount++
			recs = append(recs, Recommendation{
				Priority:              priorityForAppBytes(uint64(bytes)),
				Action:                ActionCleanAppSandbox,
				BundleID:              a.BundleID,
				AppName:               a.Name,
				EstimatedRecoverBytes: bytes,
				Rationale: fmt.Sprintf(
					"%s is %.1f GB. Probe confirmed the sandbox is vendable, so apps_clean can "+
						"remove tmp/ and Library/Caches/ without touching Documents/.",
					displayName(a), float64(bytes)/(1024.0*1024.0*1024.0),
				),
				ViaTool: ViaAppsClean,
			})
			continue
		}
		if offloadCount >= topAppsByTotal {
			continue
		}
		offloadCount++
		recs = append(recs, Recommendation{
			Priority:              priorityForAppBytes(uint64(bytes)),
			Action:                ActionOffloadApp,
			BundleID:              a.BundleID,
			AppName:               a.Name,
			EstimatedRecoverBytes: bytes,
			Rationale: fmt.Sprintf(
				"%s is %.1f GB. Offload removes the binary + caches but preserves user data "+
					"and login state (Settings restores it from iCloud on reinstall).",
				displayName(a), float64(bytes)/(1024.0*1024.0*1024.0),
			),
			ViaTool: ViaOpenAppStorageSettings,
		})
	}

	// Generic fallback: storage info reported low AND no individual
	// big app to target AND no per-app recommendation already added.
	// We gate on TotalBytes>0 so the empty-inputs case (used in tests
	// and as a zero-state) doesn't synthesize a fallback rec from a
	// percentFree=0 default. We also skip the fallback when we have
	// already emitted at least one per-app rec — the user can act on
	// those first; adding a generic "review apps" alongside specifics
	// is noise.
	hasPerAppRec := false
	for _, r := range recs {
		if r.Action == ActionOffloadApp || r.Action == ActionCleanAppSandbox || r.Action == ActionUninstallApp {
			hasPerAppRec = true
			break
		}
	}
	if in.TotalBytes > 0 && summary.Label == "low" && !hasLargeApp(sorted) && !hasPerAppRec {
		recs = append(recs, Recommendation{
			Priority:              PriorityMedium,
			Action:                ActionGenericReview,
			EstimatedRecoverBytes: 0,
			Rationale: "Free space is low and no single app is large enough to target. " +
				"Review the apps list and uninstall any you haven't opened in months — " +
				"there's no in-tool way to identify those without launch-time data.",
			ViaTool: ViaNone,
		})
	}

	// Stable ordering: priority desc, then estimatedRecoverBytes desc,
	// then bundleID for determinism on ties.
	sort.SliceStable(recs, func(i, j int) bool {
		pi, pj := recs[i].Priority.rank(), recs[j].Priority.rank()
		if pi != pj {
			return pi > pj
		}
		if recs[i].EstimatedRecoverBytes != recs[j].EstimatedRecoverBytes {
			return recs[i].EstimatedRecoverBytes > recs[j].EstimatedRecoverBytes
		}
		return recs[i].BundleID < recs[j].BundleID
	})

	return Payload{
		Device:          Device{UDID: in.UDID, Name: in.DeviceName},
		Summary:         summary,
		Recommendations: recs,
		NotTouchable:    staticNotTouchable(),
	}
}

// displayName prefers the human-readable Name; falls back to BundleID
// when the lister returned an empty name (which happens for some
// app-installation states).
func displayName(a apps.App) string {
	if a.Name != "" {
		return a.Name
	}
	return a.BundleID
}

// priorityForAppBytes scales a per-app recommendation's priority by
// its size. Cutoffs:
//   - >= 2 GiB: high
//   - >= 500 MiB: medium
//   - otherwise: low
//
// Picked to match the rough "single biggest thing the user could
// reclaim" intuition — anything sub-500-MiB rarely meaningfully
// changes an iPhone's free space.
func priorityForAppBytes(bytes uint64) Priority {
	switch {
	case bytes >= 2*1024*1024*1024:
		return PriorityHigh
	case bytes >= 500*1024*1024:
		return PriorityMedium
	default:
		return PriorityLow
	}
}

// hasLargeApp reports whether the (already-sorted) slice contains at
// least one non-Apple app over the largeAppThreshold. Used to gate the
// generic "consider uninstalling unused apps" fallback.
func hasLargeApp(sortedDesc []apps.App) bool {
	for _, a := range sortedDesc {
		if isAppleSystem(a.BundleID) {
			continue
		}
		if a.DynamicBytes+a.StaticBytes >= largeAppThreshold {
			return true
		}
	}
	return false
}
