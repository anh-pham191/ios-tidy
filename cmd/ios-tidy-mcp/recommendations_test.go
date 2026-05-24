package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/recommendations"
	"github.com/anh-pham191/ios-tidy/internal/storage"
)

// ----------------------------------------------------------------------
// storage_recommendations
// ----------------------------------------------------------------------

func TestStorageRecommendations_happyPath(t *testing.T) {
	store := &loadingProbeStore{Results: map[string][]apps.ProbeResult{
		"U1": {
			{BundleID: "com.foo.cleanable", Outcome: apps.ProbeVended, At: time.Now()},
		},
	}}
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "iPhone One"},
		}},
		Storage: &storage.FakeClient{Info: storage.DeviceInfo{
			TotalBytes: 100_000_000_000,
			FreeBytes:  4_000_000_000, // 4%
		}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.burbn.instagram", Name: "Instagram", DynamicBytes: 4 * 1024 * 1024 * 1024},
			{BundleID: "com.foo.cleanable", Name: "Cleanable", DynamicBytes: 900 * 1024 * 1024},
			{BundleID: "com.apple.mobilesafari", Name: "Safari", DynamicBytes: 8 * 1024 * 1024 * 1024},
		}},
		CrashLogs: &crashlogs.FakeClient{ListEntries: []crashlogs.Entry{
			{Path: "/a.ips", Size: 6 * 1024 * 1024},
			{Path: "/b.ips", Size: 5 * 1024 * 1024},
		}},
		ProbeStore: store,
	}
	h := newStorageRecommendationsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	var p recommendations.Payload
	if err := json.Unmarshal([]byte(extractText(res)), &p); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, extractText(res))
	}
	if p.Device.UDID != "U1" || p.Device.Name != "iPhone One" {
		t.Errorf("device echo wrong: %+v", p.Device)
	}
	if p.Summary.Label != "low" {
		t.Errorf("4%% free should label low, got %q", p.Summary.Label)
	}
	// notTouchable populated.
	if p.NotTouchable.SystemData == "" || p.NotTouchable.Photos == "" {
		t.Errorf("notTouchable disclosure missing: %+v", p.NotTouchable)
	}
	// No com.apple.* in recs.
	for _, r := range p.Recommendations {
		if strings.HasPrefix(r.BundleID, "com.apple.") {
			t.Errorf("apple bundle should never appear: %+v", r)
		}
	}
	// Crashlogs rec must be present (high priority).
	sawCrashlogs := false
	for _, r := range p.Recommendations {
		if r.Action == recommendations.ActionCleanCrashlogs && r.Priority == recommendations.PriorityHigh {
			sawCrashlogs = true
		}
	}
	if !sawCrashlogs {
		t.Errorf("expected high-priority crashlogs rec: %+v", p.Recommendations)
	}
	// Vended app should get clean_app_sandbox; unvended big app should get offload.
	var sawSandbox, sawOffload bool
	for _, r := range p.Recommendations {
		if r.Action == recommendations.ActionCleanAppSandbox && r.BundleID == "com.foo.cleanable" {
			sawSandbox = true
		}
		if r.Action == recommendations.ActionOffloadApp && r.BundleID == "com.burbn.instagram" {
			sawOffload = true
		}
	}
	if !sawSandbox {
		t.Errorf("expected sandbox-clean rec for vended bundle, recs=%+v", p.Recommendations)
	}
	if !sawOffload {
		t.Errorf("expected offload rec for unvended bundle, recs=%+v", p.Recommendations)
	}
}

func TestStorageRecommendations_noDevices(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: nil},
	}
	h := newStorageRecommendationsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("no-devices should not be an MCP error: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "no devices attached") {
		t.Errorf("expected 'no devices attached', got: %s", extractText(res))
	}
}

func TestStorageRecommendations_emptyDevice_noRecs(t *testing.T) {
	deps := serverDeps{
		Lister:     &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Storage:    &storage.FakeClient{Info: storage.DeviceInfo{TotalBytes: 100, FreeBytes: 90}},
		Apps:       &apps.FakeLister{Apps: nil},
		CrashLogs:  &crashlogs.FakeClient{ListEntries: nil},
		ProbeStore: &loadingProbeStore{},
	}
	h := newStorageRecommendationsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected err: %s", extractText(res))
	}
	var p recommendations.Payload
	if err := json.Unmarshal([]byte(extractText(res)), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Summary.Label != "high" {
		t.Errorf("90%% free should label high, got %q", p.Summary.Label)
	}
	if len(p.Recommendations) != 0 {
		t.Errorf("empty device with high free should produce no recs, got %+v", p.Recommendations)
	}
}

func TestStorageRecommendations_propagatesStorageError(t *testing.T) {
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Storage:   &storage.FakeClient{Err: errors.New("afc down")},
		Apps:      &apps.FakeLister{},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newStorageRecommendationsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("expected error result, got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "afc down") {
		t.Errorf("expected wrapped error, got: %s", extractText(res))
	}
}

func TestStorageRecommendations_handlesNilProbeStore(t *testing.T) {
	// ProbeStore nil should not panic — handler treats as "no probes recorded".
	deps := serverDeps{
		Lister:    &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Storage:   &storage.FakeClient{Info: storage.DeviceInfo{TotalBytes: 100, FreeBytes: 50}},
		Apps:      &apps.FakeLister{},
		CrashLogs: &crashlogs.FakeClient{},
	}
	h := newStorageRecommendationsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error: %s", extractText(res))
	}
}

// ----------------------------------------------------------------------
// apps_offload_candidates
// ----------------------------------------------------------------------

func TestAppsOffloadCandidates_sortedDescending(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.small", DynamicBytes: 100_000_000},
			{BundleID: "com.large", DynamicBytes: 5_000_000_000},
			{BundleID: "com.medium", DynamicBytes: 1_000_000_000},
			{BundleID: "com.apple.mobilesafari", DynamicBytes: 9_000_000_000},
		}},
	}
	h := newAppsOffloadCandidatesHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected err: %s", extractText(res))
	}
	var out []offloadCandidateRow
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, extractText(res))
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 candidates (com.apple.* filtered), got %d: %+v", len(out), out)
	}
	if out[0].BundleID != "com.large" || out[1].BundleID != "com.medium" || out[2].BundleID != "com.small" {
		t.Errorf("not sorted descending: %+v", out)
	}
	for _, c := range out {
		if strings.HasPrefix(c.BundleID, "com.apple.") {
			t.Errorf("com.apple.* must be filtered: %+v", c)
		}
		if !c.Offloadable {
			t.Errorf("third-party app must be offloadable: %+v", c)
		}
	}
}

func TestAppsOffloadCandidates_honoursLimit(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.a", DynamicBytes: 300},
			{BundleID: "com.b", DynamicBytes: 200},
			{BundleID: "com.c", DynamicBytes: 100},
		}},
	}
	h := newAppsOffloadCandidatesHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{"limit": 2}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	var out []offloadCandidateRow
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("limit=2 should produce 2 rows, got %d: %+v", len(out), out)
	}
}

func TestAppsOffloadCandidates_honoursMinBytes(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.big", DynamicBytes: 1_000_000_000},
			{BundleID: "com.small", DynamicBytes: 1_000_000},
		}},
	}
	h := newAppsOffloadCandidatesHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{"min_bytes": 500_000_000}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	var out []offloadCandidateRow
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].BundleID != "com.big" {
		t.Errorf("min_bytes filter wrong: %+v", out)
	}
}

func TestAppsOffloadCandidates_allZeroBytes_fallsBackToNameSort(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.z", Name: "Zed"},
			{BundleID: "com.a", Name: "Alpha"},
		}},
	}
	h := newAppsOffloadCandidatesHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{"min_bytes": 0}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	var out []offloadCandidateRow
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, extractText(res))
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(out), out)
	}
	// With all-zero bytes, ordering falls back to bundleID ascending.
	if out[0].BundleID != "com.a" || out[1].BundleID != "com.z" {
		t.Errorf("expected name-fallback order when all bytes zero: %+v", out)
	}
}
