package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/sandbox"
	"github.com/anh-pham191/ios-tidy/internal/storage"
)

// callToolRequestWithArgs builds a CallToolRequest whose Params.Arguments
// carry the given map. mark3labs/mcp-go's typed accessors (GetString,
// GetBool, GetInt) read from this map; constructing the request this way
// is the supported way to test handlers in-process.
func callToolRequestWithArgs(args map[string]any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Arguments = args
	return r
}

// resultIsError reports whether a CallToolResult was constructed via
// mcp.NewToolResultError. We check IsError because that is the public
// flag every MCP client inspects.
func resultIsError(r *mcp.CallToolResult) bool {
	return r != nil && r.IsError
}

func TestDevicesListTool_returnsJSONArray(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "iPhone One", Model: "iPhone14,5", IOSVersion: "17.3"},
		}},
		TrustChecker: &device.FakeTrustChecker{Trusts: map[string]bool{"U1": true}},
	}
	h := newDevicesListHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"udid": "U1"`) {
		t.Errorf("missing udid: %s", text)
	}
	if !strings.Contains(text, `"trusted": true`) {
		t.Errorf("missing trusted: %s", text)
	}
}

func TestStorageTool_returnsDevicePlusApps(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Storage: &storage.FakeClient{Info: storage.DeviceInfo{
			Model:      "iPhone14,5",
			TotalBytes: 256_000_000_000,
			FreeBytes:  64_000_000_000,
		}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.b", DynamicBytes: 50, StaticBytes: 50},
			{BundleID: "com.a", DynamicBytes: 1000, StaticBytes: 500},
		}},
	}
	h := newStorageHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"device"`) || !strings.Contains(text, `"apps"`) {
		t.Errorf("expected device + apps keys: %s", text)
	}
	// com.a (1500 total) must come before com.b (100 total)
	idxA := strings.Index(text, "com.a")
	idxB := strings.Index(text, "com.b")
	if idxA < 0 || idxB < 0 || idxA > idxB {
		t.Errorf("expected com.a sorted before com.b: %s", text)
	}
}

func TestStorageTool_honoursLimit(t *testing.T) {
	deps := serverDeps{
		Lister:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Storage: &storage.FakeClient{},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.a", DynamicBytes: 100},
			{BundleID: "com.b", DynamicBytes: 90},
			{BundleID: "com.c", DynamicBytes: 80},
		}},
	}
	h := newStorageHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{"limit": 1}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	text := extractText(res)
	if !strings.Contains(text, "com.a") || strings.Contains(text, "com.b") || strings.Contains(text, "com.c") {
		t.Errorf("limit=1 should keep only com.a: %s", text)
	}
}

func TestCrashLogsListTool_returnsEntries(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{ListEntries: []crashlogs.Entry{
			{Path: "/foo.ips", Size: 12, ModTime: time.Unix(1700000000, 0).UTC()},
		}},
	}
	h := newCrashLogsListHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"/foo.ips"`) {
		t.Errorf("missing path: %s", text)
	}
}

func TestAppsListTool_returnsJSONArray(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.b", DynamicBytes: 50, StaticBytes: 50},
			{BundleID: "com.a", DynamicBytes: 1000, StaticBytes: 0},
		}},
	}
	h := newAppsListHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"bundleID": "com.a"`) {
		t.Errorf("missing bundle com.a: %s", text)
	}
	idxA := strings.Index(text, "com.a")
	idxB := strings.Index(text, "com.b")
	if idxA < 0 || idxB < 0 || idxA > idxB {
		t.Errorf("expected com.a (1000) before com.b (100): %s", text)
	}
}

func TestAppsListTool_noDevicesAttached(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: nil},
		Apps:   &apps.FakeLister{},
	}
	h := newAppsListHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("no-devices should not be an MCP error result: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "no devices attached") {
		t.Errorf("expected 'no devices attached' content: %s", extractText(res))
	}
}

func TestAppsListTool_listerError(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:   &apps.FakeLister{Err: errors.New("ip-proxy down")},
	}
	h := newAppsListHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result, got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "ip-proxy down") {
		t.Errorf("expected wrapped lister error in text: %s", extractText(res))
	}
}

func TestAppsProbeTool_persistsAndReturnsResults(t *testing.T) {
	fakeSb := sandbox.NewFakeSandbox()
	fakeSb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})

	store := &fakeProbeStore{}
	deps := serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:       &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}},
		Sandbox:    fakeSb,
		Prober:     apps.NewProber(fakeSb),
		ProbeStore: store,
	}
	h := newAppsProbeHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundles": []any{"com.a"},
		"timeout": "1s",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"com.a"`) {
		t.Errorf("missing bundle id in output: %s", text)
	}
	if !strings.Contains(text, `"vended"`) {
		t.Errorf("expected vended outcome: %s", text)
	}
	if len(store.saved) != 1 || store.saved[0].udid != "U1" || len(store.saved[0].results) != 1 {
		t.Errorf("expected one Save(U1, [1 result]); got %+v", store.saved)
	}

	// Also confirm JSON parseable as the expected shape.
	var out []probeRow
	if jerr := json.Unmarshal([]byte(text), &out); jerr != nil {
		t.Fatalf("output is not parseable JSON array: %v", jerr)
	}
	if len(out) != 1 || out[0].BundleID != "com.a" || out[0].Outcome != "vended" {
		t.Errorf("unexpected parsed output: %+v", out)
	}
}

// fakeProbeStore is a local test double for apps.ProbeStore. The real
// FileProbeStore writes to disk, which is overkill for testing that the
// MCP handler calls Save with the right UDID + results.
type fakeProbeStore struct {
	saved []probeSaveCall
	load  []apps.ProbeResult
}

type probeSaveCall struct {
	udid    string
	results []apps.ProbeResult
}

func (f *fakeProbeStore) Save(udid string, results []apps.ProbeResult) error {
	f.saved = append(f.saved, probeSaveCall{udid: udid, results: results})
	return nil
}
func (f *fakeProbeStore) Load(_ string) ([]apps.ProbeResult, error) {
	return f.load, nil
}

// loadingProbeStore is the test double used by apps_clean tests. Save is a
// no-op; Load returns canned results keyed by UDID. Mirrors the same struct
// in cmd/ios-tidy/apps_clean_test.go intentionally — the MCP layer needs an
// equivalent fake to gate-test the probe contract independently.
type loadingProbeStore struct {
	Results map[string][]apps.ProbeResult
	LoadErr error
}

func (s *loadingProbeStore) Save(_ string, _ []apps.ProbeResult) error { return nil }
func (s *loadingProbeStore) Load(udid string) ([]apps.ProbeResult, error) {
	if s.LoadErr != nil {
		return nil, s.LoadErr
	}
	return s.Results[udid], nil
}

// ----------------------------------------------------------------------
// crashlogs_clean
// ----------------------------------------------------------------------

// TestCrashLogsClean_defaultIsDryRun pins the safety contract: with `confirm`
// absent, the destructive Client.Remove MUST NOT be called. The handler
// instead lists entries and returns a dry-run JSON shape.
func TestCrashLogsClean_defaultIsDryRun(t *testing.T) {
	fc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/a.ips", Size: 10},
			{Path: "/b.ips", Size: 20},
		},
		RemoveFn: func(_ context.Context, _, _ string) (crashlogs.RemoveResult, error) {
			t.Fatalf("Remove must not be called under default (no confirm)")
			return crashlogs.RemoveResult{}, nil
		},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"dryRun": true`) {
		t.Errorf("expected dryRun=true: %s", text)
	}
	if !strings.Contains(text, `"wouldDelete": 2`) {
		t.Errorf("expected wouldDelete=2: %s", text)
	}
	if !strings.Contains(text, `"bytes": 30`) {
		t.Errorf("expected bytes=30: %s", text)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Errorf("RemoveCalls should be empty on dry-run: %v", fc.RemoveCalls)
	}
}

// TestCrashLogsClean_confirmTrueCallsRemove pins that confirm=true actually
// fires Client.Remove and returns the deleted counts.
func TestCrashLogsClean_confirmTrueCallsRemove(t *testing.T) {
	fc := &crashlogs.FakeClient{
		ListEntries:  []crashlogs.Entry{{Path: "/a.ips", Size: 10}},
		RemoveResult: crashlogs.RemoveResult{Removed: 1, Bytes: 10},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"confirm": true,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	if len(fc.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %v, want 1", fc.RemoveCalls)
	}
	text := extractText(res)
	if !strings.Contains(text, `"dryRun": false`) {
		t.Errorf("expected dryRun=false: %s", text)
	}
	if !strings.Contains(text, `"deleted": 1`) {
		t.Errorf("expected deleted=1: %s", text)
	}
}

// TestCrashLogsClean_failurePropagates pins that per-entry failures returned
// by Client.Remove make it into the JSON output verbatim.
func TestCrashLogsClean_failurePropagates(t *testing.T) {
	fc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/a.ips", Size: 10}},
		RemoveResult: crashlogs.RemoveResult{
			Removed: 0,
			Bytes:   0,
			Failures: []crashlogs.Failure{
				{Path: "/a.ips", ErrMsg: "device disconnected"},
			},
		},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"confirm": true,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "device disconnected") {
		t.Errorf("expected failure detail in output: %s", text)
	}
	if !strings.Contains(text, `"/a.ips"`) {
		t.Errorf("expected failed path in output: %s", text)
	}
}

// ----------------------------------------------------------------------
// crashlogs_pull
// ----------------------------------------------------------------------

// TestCrashLogsPull_outRequired pins that the required `out` arg is enforced.
func TestCrashLogsPull_outRequired(t *testing.T) {
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for missing out; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "out") {
		t.Errorf("expected error message mentioning 'out': %s", extractText(res))
	}
}

// TestCrashLogsPull_relativePathRejected pins absolute-path enforcement: a
// relative `out` value is refused with a clear error.
func TestCrashLogsPull_relativePathRejected(t *testing.T) {
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	cases := []string{"./relative", "relative/dir", "/abs/../escape"}
	for _, out := range cases {
		t.Run(out, func(t *testing.T) {
			res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
				"out": out,
			}))
			if err != nil {
				t.Fatalf("handler err: %v", err)
			}
			if !resultIsError(res) {
				t.Fatalf("expected error result for out=%q; got: %s", out, extractText(res))
			}
		})
	}
}

// TestCrashLogsPull_happyPath pins the success contract: with a valid
// absolute path that exists, the handler dispatches to Client.Pull and
// returns counts + dest.
func TestCrashLogsPull_happyPath(t *testing.T) {
	tmp := t.TempDir()
	fc := &crashlogs.FakeClient{
		PullResult: crashlogs.PullResult{Pulled: 2, Bytes: 42},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": tmp,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"pulled": 2`) {
		t.Errorf("expected pulled=2: %s", text)
	}
	if !strings.Contains(text, `"bytes": 42`) {
		t.Errorf("expected bytes=42: %s", text)
	}
	if !strings.Contains(text, tmp) {
		t.Errorf("expected dest dir %q in output: %s", tmp, text)
	}
	if len(fc.PullCalls) != 1 || fc.PullCalls[0].Dst != tmp {
		t.Errorf("PullCalls = %v, want one call with dst=%q", fc.PullCalls, tmp)
	}
}

// ----------------------------------------------------------------------
// apps_clean — the safety-critical suite
// ----------------------------------------------------------------------

// appsCleanFixture builds a sandbox + FakeFS pre-seeded with Walk entries for
// every target so BuildPlan returns non-empty plans. probe store is wired
// with a Vended outcome for "com.example.app" on UDID "U1".
func appsCleanFixture() (*sandbox.FakeFS, *sandbox.FakeSandbox, *loadingProbeStore) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
			"Documents":      {{Path: "Documents/secret.txt", Size: 100}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})
	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	return fakeFS, sb, store
}

func newAppsCleanDeps(sb *sandbox.FakeSandbox, store *loadingProbeStore) serverDeps {
	return serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:    sb,
		ProbeStore: store,
	}
}

func TestAppsClean_defaultIsDryRun_noExecuteCalled(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.example.app",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("default should be dry-run; Remove=%v RemoveAll=%v",
			fakeFS.RemoveCalls, fakeFS.RemoveAllCalls)
	}
	text := extractText(res)
	if !strings.Contains(text, `"dryRun": true`) {
		t.Errorf("expected dryRun=true: %s", text)
	}
}

func TestAppsClean_explicitDryRunTrue_noExecuteCalled(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.example.app",
		"dry_run":   true,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("dry_run=true should not Execute; Remove=%v RemoveAll=%v",
			fakeFS.RemoveCalls, fakeFS.RemoveAllCalls)
	}
}

func TestAppsClean_missingConfirmBundleIDRefuses(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.example.app",
		"dry_run":   false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "confirm_bundle_id") {
		t.Errorf("error must mention confirm_bundle_id: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("Execute must not be called when confirm_bundle_id missing")
	}
}

func TestAppsClean_mismatchedConfirmBundleIDRefuses(t *testing.T) {
	cases := []struct {
		name    string
		confirm string
	}{
		{"typo", "com.example.ap"},
		{"empty", ""},
		{"different", "com.other.app"},
		{"case_mismatch", "COM.EXAMPLE.APP"},
		{"extra_char", "com.example.appx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeFS, sb, store := appsCleanFixture()
			deps := newAppsCleanDeps(sb, store)
			h := newAppsCleanHandler(deps)
			res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
				"bundle_id":         "com.example.app",
				"confirm_bundle_id": tc.confirm,
				"dry_run":           false,
			}))
			if err != nil {
				t.Fatalf("handler err: %v", err)
			}
			if !resultIsError(res) {
				t.Fatalf("expected error result for confirm=%q; got: %s", tc.confirm, extractText(res))
			}
			if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
				t.Errorf("Execute must not be called on confirm mismatch")
			}
		})
	}
}

func TestAppsClean_matchedConfirmBundleIDProceeds_defaultIncludes(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	// Default targets are tmp + caches → 2 RemoveAll calls.
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("RemoveAllCalls = %v, want 2 (tmp + Library/Caches)", fakeFS.RemoveAllCalls)
	}
	// Documents must NOT have been touched (per-file path).
	for _, p := range fakeFS.RemoveCalls {
		if strings.HasPrefix(p, "Documents") {
			t.Errorf("Documents must NOT be touched without include_documents; got Remove(%q)", p)
		}
	}
}

func TestAppsClean_includeDocumentsWithoutAcknowledgmentRefuses(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
	}{
		{
			"ack_missing",
			map[string]any{
				"bundle_id":         "com.example.app",
				"confirm_bundle_id": "com.example.app",
				"include_documents": true,
				"dry_run":           false,
			},
		},
		{
			"ack_false",
			map[string]any{
				"bundle_id":         "com.example.app",
				"confirm_bundle_id": "com.example.app",
				"include_documents": true,
				"i_understand_documents_are_unrecoverable": false,
				"dry_run": false,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeFS, sb, store := appsCleanFixture()
			deps := newAppsCleanDeps(sb, store)
			h := newAppsCleanHandler(deps)
			res, err := h(context.Background(), callToolRequestWithArgs(tc.args))
			if err != nil {
				t.Fatalf("handler err: %v", err)
			}
			if !resultIsError(res) {
				t.Fatalf("expected error result; got: %s", extractText(res))
			}
			if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
				t.Errorf("Execute must not be called on Documents without ack")
			}
		})
	}
}

func TestAppsClean_includeDocumentsWithAcknowledgmentProceeds(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"include_documents": true,
		"i_understand_documents_are_unrecoverable": true,
		"dry_run": false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	// Documents uses per-file Remove; the fixture has 1 file under Documents.
	found := false
	for _, p := range fakeFS.RemoveCalls {
		if strings.HasPrefix(p, "Documents") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Documents target must have been executed via per-file Remove; got Removes=%v", fakeFS.RemoveCalls)
	}
}

func TestAppsClean_includeDocumentsCaseSensitiveMismatchRefuses(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "COM.EXAMPLE.APP",
		"include_documents": true,
		"i_understand_documents_are_unrecoverable": true,
		"dry_run": false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for case-mismatched confirm; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("Execute must not be called on case-mismatched confirm")
	}
}

// trapSandbox fails the test loudly if Open is called.
type trapSandbox struct{ t *testing.T }

func (s *trapSandbox) Open(_ context.Context, _, _ string) (sandbox.FS, error) {
	s.t.Fatalf("Sandbox.Open must not be called when probe gate refuses")
	return nil, errors.New("unreachable")
}

func TestAppsClean_probeGate_noVended(t *testing.T) {
	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeRefused, Detail: "daemon refused"}},
		},
	}
	deps := serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:    &trapSandbox{t: t},
		ProbeStore: store,
	}
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for non-vended probe; got: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "apps_probe") {
		t.Errorf("error must point at apps_probe: %s", text)
	}
	if !strings.Contains(text, "com.example.app") {
		t.Errorf("error must name the bundle: %s", text)
	}
}

func TestAppsClean_probeGate_neverProbed(t *testing.T) {
	store := &loadingProbeStore{Results: map[string][]apps.ProbeResult{}}
	deps := serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:    &trapSandbox{t: t},
		ProbeStore: store,
	}
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result when no probe exists; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "apps_probe") {
		t.Errorf("error must point at apps_probe: %s", extractText(res))
	}
}
