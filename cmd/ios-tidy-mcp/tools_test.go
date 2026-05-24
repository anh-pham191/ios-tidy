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
