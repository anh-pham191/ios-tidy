package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

// TestAppsProbeMCP_rejectsHomoglyph pins H-1 parity with apps_clean on the
// probe surface. The shared probe store is the bridge between MCP and CLI;
// a homoglyph saved here would defeat the CLI's typed-bundle-ID gate
// downstream. The handler MUST refuse the whole probe run on first
// offender, BEFORE any device I/O or Save() call.
func TestAppsProbeMCP_rejectsHomoglyph(t *testing.T) {
	fakeSb := sandbox.NewFakeSandbox()
	store := &fakeProbeStore{}
	deps := serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:       &apps.FakeLister{Apps: []apps.App{{BundleID: "com.example.app"}}},
		Sandbox:    fakeSb,
		Prober:     apps.NewProber(fakeSb),
		ProbeStore: store,
	}
	h := newAppsProbeHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		// "com.exаmple.app" — Cyrillic 'а' (U+0430).
		"bundles": []any{"com.exаmple.app"},
		"timeout": "1s",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for non-ASCII bundle; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "non-printable-ASCII") {
		t.Errorf("expected error to mention non-printable-ASCII; got: %s", extractText(res))
	}
	if len(store.saved) != 0 {
		t.Errorf("ProbeStore.Save must not be called when ASCII gate refuses; saved=%+v", store.saved)
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
// absolute path inside the allow-root that exists, the handler dispatches
// to Client.Pull and returns counts + dest. Uses IOS_TIDY_MCP_PULL_ROOT
// to point the allow-root at t.TempDir() rather than $HOME so the test
// is hermetic.
func TestCrashLogsPull_happyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", tmp)
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
// crashlogs_pull — path-write hardening (H-2 + M-2)
// ----------------------------------------------------------------------

// TestCrashLogsPull_rejectsOutsideHome pins that a path outside the
// allow-root is refused. Test uses a tmpdir root and asks to write to a
// sibling directory (also under /var/folders but NOT under the root) so
// the assertion is hermetic.
func TestCrashLogsPull_rejectsOutsideHome(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // sibling, not under root
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)

	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": outside,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for out outside allow-root; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "outside") {
		t.Errorf("error must mention 'outside': %s", extractText(res))
	}
}

// TestCrashLogsPull_rejectsSshDir pins that the deny-list catches the
// .ssh subpath even when the path is otherwise inside the allow-root.
// SSH keys live under ~/.ssh; a crashlog dump landing there could
// overwrite or shadow them.
func TestCrashLogsPull_rejectsSshDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)
	sshDir := filepath.Join(root, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}

	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": sshDir,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for .ssh; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "sensitive") {
		t.Errorf("error must mention 'sensitive': %s", extractText(res))
	}
}

// TestCrashLogsPull_rejectsLaunchAgentsDir pins that the LaunchAgents
// deny-list entry fires. A crashlog dump that overwrites a LaunchAgent
// plist with arbitrary content is a persistence-mechanism foothold.
func TestCrashLogsPull_rejectsLaunchAgentsDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)
	laDir := filepath.Join(root, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o700); err != nil {
		t.Fatalf("mkdir LaunchAgents: %v", err)
	}

	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": laDir,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for LaunchAgents; got: %s", extractText(res))
	}
}

// TestCrashLogsPull_rejectsSensitiveNestedSubpath pins prefix-match
// semantics: a deeper directory inside a sensitive root (e.g.
// .ssh/old-keys) is just as forbidden as the root itself.
func TestCrashLogsPull_rejectsSensitiveNestedSubpath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)
	nested := filepath.Join(root, "Library", "Keychains", "backup")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": nested,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for nested Keychains subpath; got: %s", extractText(res))
	}
}

// TestCrashLogsPull_rejectsSymlink pins that a symlink (even one whose
// target is also inside the allow-root) is refused. Lstat catches it;
// Stat would have followed the link and let it through.
func TestCrashLogsPull_rejectsSymlink(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)
	target := filepath.Join(root, "real-target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "symlink-to-target")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": link,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for symlink; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "symlink") {
		t.Errorf("error must mention 'symlink': %s", extractText(res))
	}
}

// TestCrashLogsPull_acceptsValidAllowRootSubdir pins the positive case:
// a real directory inside the allow-root proceeds.
func TestCrashLogsPull_acceptsValidAllowRootSubdir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)
	sub := filepath.Join(root, "crashes-out")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	fc := &crashlogs.FakeClient{
		PullResult: crashlogs.PullResult{Pulled: 1, Bytes: 1},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": sub,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	if len(fc.PullCalls) != 1 {
		t.Errorf("expected one Pull call; got: %v", fc.PullCalls)
	}
}

// TestCrashLogsPull_envOverrideAllowsTestRoot confirms the env override
// is honoured: without it, a /var/folders path would be outside $HOME on
// macOS and the validation would refuse.
func TestCrashLogsPull_envOverrideAllowsTestRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", root)

	fc := &crashlogs.FakeClient{
		PullResult: crashlogs.PullResult{Pulled: 0, Bytes: 0},
	}
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		CrashLogs: fc,
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": root,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("env override should allow root; got: %s", extractText(res))
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
		// ListResults seeds the top-level child enumeration used by
		// executeRemoveAll (which removes children rather than the
		// target node itself — see internal/sandbox/cleaner.go).
		ListResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Name: "a", Path: "tmp/a"}},
			"Library/Caches": {{Name: "c", Path: "Library/Caches/c"}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})
	// Stamp At=now so the probe is within the apps_clean TTL window.
	// Tests that need to exercise the TTL boundary set their own At
	// and inject a deps.Now() that fixes the comparison point.
	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: time.Now()}},
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

// ----------------------------------------------------------------------
// H-1: ASCII-only bundle_id / confirm_bundle_id
// ----------------------------------------------------------------------

// TestAppsClean_rejectsBundleIDWithNonASCII pins the homograph defence.
// The Cyrillic 'а' (U+0430) renders identically to ASCII 'a' but is
// byte-different; without a strict-ASCII check the typed-bundle-ID gate
// would pass on a homoglyph pair and the destructive op would target an
// app that doesn't exist (or, worse, a different one). The handler must
// refuse before any device I/O and never reach Sandbox.Open.
func TestAppsClean_rejectsBundleIDWithNonASCII(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	// Replace sandbox with a trap so a regression that bypasses the
	// ASCII gate is caught loudly instead of returning a misleading
	// "no plan" success.
	deps.Sandbox = &trapSandbox{t: t}
	h := newAppsCleanHandler(deps)

	// "com.exаmple.app" — the third character is Cyrillic 'а' (U+0430).
	cyrillicBundle := "com.exаmple.app"
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         cyrillicBundle,
		"confirm_bundle_id": cyrillicBundle,
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for non-ASCII bundle_id; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "non-printable-ASCII") {
		t.Errorf("expected error to mention non-printable-ASCII; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("Execute must not be called on non-ASCII bundle_id")
	}
}

// TestAppsClean_rejectsBundleIDWithControlChar pins that an embedded
// control character (NUL here) is also refused. Stops a caller from
// smuggling a null-terminator past a downstream consumer that does C
// string handling.
func TestAppsClean_rejectsBundleIDWithControlChar(t *testing.T) {
	_, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	h := newAppsCleanHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app\x00",
		"confirm_bundle_id": "com.example.app\x00",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for NUL byte in bundle_id; got: %s", extractText(res))
	}
}

// TestAppsClean_rejectsConfirmBundleIDWithNonASCII pins the other half of
// the homograph defence: only confirm_bundle_id contains the lookalike.
// The TrimSpace+equality check between bundle_id and confirm_bundle_id is
// reached only AFTER ASCII validation, so a homoglyph in just the confirm
// field is still refused at the ASCII layer.
func TestAppsClean_rejectsConfirmBundleIDWithNonASCII(t *testing.T) {
	_, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	h := newAppsCleanHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.exаmple.app",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result for non-ASCII confirm_bundle_id; got: %s", extractText(res))
	}
}

// TestAppsClean_acceptsValidASCII is the sanity check that ordinary
// reverse-DNS bundle IDs still proceed through the gate.
func TestAppsClean_acceptsValidASCII(t *testing.T) {
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
		t.Fatalf("ASCII bundle_id should proceed; got error: %s", extractText(res))
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("expected tmp+caches Execute under default include combo; got RemoveAllCalls=%v", fakeFS.RemoveAllCalls)
	}
}

// ----------------------------------------------------------------------
// M-1: dry_run string coercion contract
// ----------------------------------------------------------------------

// TestAppsClean_dryRunStringFalseDoesNotDisarm pins the conservative
// contract: a JSON STRING "false" for dry_run MUST NOT activate the
// destructive path. mcp-go's GetBool coerces strings via
// strconv.ParseBool, so the handler reads dry_run with a typed-only
// reader (dryRunArgConservative) that treats anything other than a real
// bool as the safe default (true).
//
// If this test fails, the handler will have gone destructive on a string
// argument — a meaningful safety regression. The fix is to keep using
// dryRunArgConservative (NOT req.GetBool).
func TestAppsClean_dryRunStringFalseDoesNotDisarm(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)

	// Note: arguments map carries the JSON STRING "false", not bool false.
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           "false",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, `"dryRun": true`) {
		t.Errorf("dry_run string \"false\" must NOT disarm; expected dryRun=true. Got: %s", text)
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("Execute must not be called when dry_run is a string. Got Removes=%v RemoveAll=%v", fakeFS.RemoveCalls, fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_dryRunNumberZeroDoesNotDisarm pins the same contract for
// a numeric 0 — mcp-go's GetBool would coerce 0 → false; we explicitly
// reject any non-bool.
func TestAppsClean_dryRunNumberZeroDoesNotDisarm(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)

	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           float64(0), // JSON numbers decode to float64
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !strings.Contains(extractText(res), `"dryRun": true`) {
		t.Errorf("dry_run numeric 0 must NOT disarm; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("Execute must not be called when dry_run is a number")
	}
}

// TestAppsClean_dryRunBoolFalseDisarms is the positive control: a REAL
// bool false MUST still flip to destructive (assuming all other gates
// clear). Stops a paranoid implementation that just refuses every
// non-default dry_run.
func TestAppsClean_dryRunBoolFalseDisarms(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.example.app",
		"confirm_bundle_id": "com.example.app",
		"dry_run":           false, // real bool
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("real bool false must reach Execute; got RemoveAllCalls=%v", fakeFS.RemoveAllCalls)
	}
}

// ----------------------------------------------------------------------
// M-4: destructive tool results echo device.name
// ----------------------------------------------------------------------

func TestAppsClean_resultIncludesDeviceName(t *testing.T) {
	fakeFS, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	// Override the lister to populate Name as well as UDID.
	deps.Lister = &device.FakeLister{Devices: []device.Device{
		{UDID: "U1", Name: "Anh's iPhone 14 Pro"},
	}}
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
	text := extractText(res)
	if !strings.Contains(text, `"name": "Anh's iPhone 14 Pro"`) {
		t.Errorf("expected device.name in result JSON; got: %s", text)
	}
	if !strings.Contains(text, `"udid": "U1"`) {
		t.Errorf("expected device.udid in result JSON; got: %s", text)
	}
	_ = fakeFS // silence unused warning when only checking text
}

func TestAppsClean_dryRunResultIncludesDeviceName(t *testing.T) {
	_, sb, store := appsCleanFixture()
	deps := newAppsCleanDeps(sb, store)
	deps.Lister = &device.FakeLister{Devices: []device.Device{
		{UDID: "U1", Name: "My Phone"},
	}}
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.example.app",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	text := extractText(res)
	if !strings.Contains(text, `"name": "My Phone"`) {
		t.Errorf("dry-run result must include device.name; got: %s", text)
	}
}

func TestCrashLogsClean_resultIncludesDeviceName(t *testing.T) {
	fc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/a.ips", Size: 10}},
	}
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "Phone A"},
		}},
		CrashLogs: fc,
	}
	h := newCrashLogsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	text := extractText(res)
	if !strings.Contains(text, `"name": "Phone A"`) {
		t.Errorf("crashlogs_clean dry-run result must include device.name; got: %s", text)
	}
}

func TestCrashLogsClean_confirmedResultIncludesDeviceName(t *testing.T) {
	fc := &crashlogs.FakeClient{
		ListEntries:  []crashlogs.Entry{{Path: "/a.ips", Size: 10}},
		RemoveResult: crashlogs.RemoveResult{Removed: 1, Bytes: 10},
	}
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "Phone A"},
		}},
		CrashLogs: fc,
	}
	h := newCrashLogsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"confirm": true,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	text := extractText(res)
	if !strings.Contains(text, `"name": "Phone A"`) {
		t.Errorf("crashlogs_clean confirmed result must include device.name; got: %s", text)
	}
}

func TestCrashLogsPull_resultIncludesDeviceName(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("IOS_TIDY_MCP_PULL_ROOT", tmp)
	fc := &crashlogs.FakeClient{
		PullResult: crashlogs.PullResult{Pulled: 1, Bytes: 1},
	}
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "Pulled Phone"},
		}},
		CrashLogs: fc,
	}
	h := newCrashLogsPullHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"out": tmp,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	text := extractText(res)
	if !strings.Contains(text, `"name": "Pulled Phone"`) {
		t.Errorf("crashlogs_pull result must include device.name; got: %s", text)
	}
}

// ----------------------------------------------------------------------
// H-2: hard-reject com.apple.* (defense in depth)
// ----------------------------------------------------------------------

// TestAppsClean_rejectsAppleSystemBundle pins that an MCP caller cannot
// reach apps_clean's destructive path on a system bundle even if the
// probe store has a Vended entry for it. The reject must run BEFORE
// Sandbox.Open / ProbeStore.Load / etc.
func TestAppsClean_rejectsAppleSystemBundle(t *testing.T) {
	cases := []string{
		"com.apple.mobilemail",
		"com.apple.Music",
		"com.apple.mobilesafari",
	}
	for _, bundle := range cases {
		t.Run(bundle, func(t *testing.T) {
			// Pre-seed a fresh Vended probe so the only thing standing
			// between the caller and Sandbox.Open is the system-app
			// reject.
			fakeFS, sb, store := appsCleanFixture()
			store.Results["U1"] = []apps.ProbeResult{
				{BundleID: bundle, Outcome: apps.ProbeVended, At: time.Now()},
			}
			deps := newAppsCleanDeps(sb, store)
			deps.Sandbox = &trapSandbox{t: t} // any Sandbox.Open fails the test
			h := newAppsCleanHandler(deps)

			res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
				"bundle_id":         bundle,
				"confirm_bundle_id": bundle,
				"dry_run":           false,
			}))
			if err != nil {
				t.Fatalf("handler err: %v", err)
			}
			if !resultIsError(res) {
				t.Fatalf("expected error result for system bundle %q; got: %s", bundle, extractText(res))
			}
			if !strings.Contains(extractText(res), "system app sandbox") {
				t.Errorf("error must explain system-app refusal; got: %s", extractText(res))
			}
			if !strings.Contains(extractText(res), bundle) {
				t.Errorf("error must echo the offending bundle; got: %s", extractText(res))
			}
			if len(fakeFS.RemoveCalls) != 0 || len(fakeFS.RemoveAllCalls) != 0 {
				t.Errorf("Execute must not be called on system bundle")
			}
		})
	}
}

// TestAppsClean_doesNotMatchComAppleWithoutDotSuffix pins the boundary:
// only com.apple.<something> is rejected. A bare "com.apple" (no dot)
// or a third-party "com.applesauce.*" must NOT be treated as system.
func TestAppsClean_doesNotMatchComAppleWithoutDotSuffix(t *testing.T) {
	_, sb, store := appsCleanFixture()
	// Empty probe store so the only refusal we'll see is the probe gate,
	// proving the system-app reject did NOT fire on plain "com.apple".
	store.Results = map[string][]apps.ProbeResult{}
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.apple",
		"confirm_bundle_id": "com.apple",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result; got: %s", extractText(res))
	}
	if strings.Contains(extractText(res), "system app sandbox") {
		t.Errorf("plain 'com.apple' must NOT match the system reject; got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "apps_probe") {
		t.Errorf("expected probe-gate refusal pointing at apps_probe; got: %s", extractText(res))
	}
}

// TestAppsClean_allowsApplesauceBundle pins that "com.applesauce.<app>"
// is a third-party domain that must pass through the system reject.
func TestAppsClean_allowsApplesauceBundle(t *testing.T) {
	_, sb, store := appsCleanFixture()
	store.Results = map[string][]apps.ProbeResult{}
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id":         "com.applesauce.app",
		"confirm_bundle_id": "com.applesauce.app",
		"dry_run":           false,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result; got: %s", extractText(res))
	}
	if strings.Contains(extractText(res), "system app sandbox") {
		t.Errorf("'com.applesauce.app' must NOT match the system reject; got: %s", extractText(res))
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

// ----------------------------------------------------------------------
// H-3: probe TTL on Vended results consumed by apps_clean
// ----------------------------------------------------------------------

// TestAppsClean_acceptsRecentVendedProbe pins the positive case: a probe
// stamped at "now" is fresh and the handler proceeds.
func TestAppsClean_acceptsRecentVendedProbe(t *testing.T) {
	now := time.Now()
	fakeFS, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Now = func() time.Time { return now }
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
		t.Fatalf("fresh probe must proceed; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("expected Execute to run for tmp+caches; RemoveAll=%v", fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_refusesStaleVendedProbe pins the TTL gate: a probe 10
// minutes old (twice the 5-minute window) is refused with a message
// pointing back at apps_probe.
func TestAppsClean_refusesStaleVendedProbe(t *testing.T) {
	now := time.Now()
	_, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-10 * time.Minute)},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	deps.Now = func() time.Time { return now }
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
		t.Fatalf("stale probe must be refused; got: %s", extractText(res))
	}
	text := extractText(res)
	if !strings.Contains(text, "apps_probe") {
		t.Errorf("error must steer caller to re-run apps_probe; got: %s", text)
	}
	if !strings.Contains(text, "minutes old") {
		t.Errorf("error must explain the staleness; got: %s", text)
	}
}

// TestAppsClean_refusesProbeAtTTLBoundary pins the boundary inclusive:
// a probe (5 min + 1 sec) old is just past the limit and refused. Uses
// a deterministic injected clock so the assertion is not flaky.
func TestAppsClean_refusesProbeAtTTLBoundary(t *testing.T) {
	now := time.Now()
	_, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-5*time.Minute - 1*time.Second)},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	deps.Now = func() time.Time { return now }
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
		t.Fatalf("probe at TTL boundary + 1s must be refused; got: %s", extractText(res))
	}
}

// TestAppsClean_acceptsProbeJustUnderTTL is the positive control for the
// boundary: a probe 4m59s old (just inside the 5-minute window) proceeds.
func TestAppsClean_acceptsProbeJustUnderTTL(t *testing.T) {
	now := time.Now()
	fakeFS, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-4*time.Minute - 59*time.Second)},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Now = func() time.Time { return now }
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
		t.Fatalf("probe 4m59s old must proceed; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("expected Execute; RemoveAll=%v", fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_freshestProbeWins ensures that when both a stale and a
// fresh result exist for the same bundle, the newer one is used. The
// store sort order is by bundleID, not timestamp, so the iteration MUST
// consult .At rather than positional newness.
func TestAppsClean_freshestProbeWins(t *testing.T) {
	now := time.Now()
	fakeFS, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		// Order: stale first, fresh second.
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-1 * time.Hour)},
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-30 * time.Second)},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Now = func() time.Time { return now }
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
		t.Fatalf("freshest probe should win; got: %s", extractText(res))
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("expected Execute; RemoveAll=%v", fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_dryRunBypassesTTL pins that dry_run does NOT exercise the
// TTL gate any differently — a stale probe still allows dry-run planning
// because dry-run is itself safe (no Execute call). The TTL exists to gate
// the destructive boundary, not the read-only one.
//
// Wait — the current implementation DOES apply the probe gate even for
// dry-run. That is intentional: returning a plan implies the bundle is
// touchable, which an LLM caller could use as a probe signal. So a stale
// probe should also refuse dry-run, even though it's safe. Pin that.
func TestAppsClean_dryRunRefusedOnStaleProbe(t *testing.T) {
	now := time.Now()
	_, sb, store := appsCleanFixture()
	store.Results["U1"] = []apps.ProbeResult{
		{BundleID: "com.example.app", Outcome: apps.ProbeVended, At: now.Add(-10 * time.Minute)},
	}
	deps := newAppsCleanDeps(sb, store)
	deps.Sandbox = &trapSandbox{t: t}
	deps.Now = func() time.Time { return now }
	h := newAppsCleanHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.example.app",
		// dry_run omitted → default true
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("stale probe must refuse dry-run as well; got: %s", extractText(res))
	}
}
