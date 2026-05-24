package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
)

// TestAppsList_tableSortedDesc verifies the table output is sorted descending
// by total bytes (DynamicBytes + StaticBytes) and contains NO device summary
// header (the bare apps list is the contract for `apps list`; the device
// "free of total" line belongs to the `storage` subcommand).
func TestAppsList_tableSortedDesc(t *testing.T) {
	lister := &apps.FakeLister{
		Apps: []apps.App{
			{BundleID: "com.small.app", Name: "Small", DynamicBytes: 1, StaticBytes: 1},
			{BundleID: "com.big.app", Name: "Big", DynamicBytes: 1_000_000_000, StaticBytes: 0},
			{BundleID: "com.mid.app", Name: "Mid", DynamicBytes: 500_000_000, StaticBytes: 0},
		},
	}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "UDID", Name: "Phone"}}}
	var stdout, stderr bytes.Buffer
	deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}

	if exit := runAppsList(context.Background(), deps, []string{"--device", "UDID"}); exit != 0 {
		t.Fatalf("runAppsList exit = %d, stderr=%s", exit, stderr.String())
	}

	got := stdout.String()
	bigIdx := strings.Index(got, "com.big.app")
	midIdx := strings.Index(got, "com.mid.app")
	smallIdx := strings.Index(got, "com.small.app")
	if bigIdx < 0 || midIdx < 0 || smallIdx < 0 {
		t.Fatalf("missing rows in output:\n%s", got)
	}
	if !(bigIdx < midIdx && midIdx < smallIdx) {
		t.Errorf("table not sorted desc by total bytes:\n%s", got)
	}
	// `apps list` is the bare app list — the device summary belongs to
	// `storage`. Guard against accidentally reintroducing it.
	if strings.Contains(got, "free of") || strings.Contains(got, "Free:") || strings.Contains(got, "Total:") {
		t.Errorf("table should NOT have device summary header:\n%s", got)
	}
}

// TestAppsList_jsonShape verifies --json emits a JSON array of App objects
// using the camelCase JSON tags defined on apps.App (bundleId, not bundleID).
func TestAppsList_jsonShape(t *testing.T) {
	lister := &apps.FakeLister{
		Apps: []apps.App{
			{BundleID: "com.foo", Name: "Foo", Version: "1.0", DynamicBytes: 10, StaticBytes: 20},
		},
	}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "UDID", Name: "Phone"}}}
	var stdout, stderr bytes.Buffer
	deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}

	if exit := runAppsList(context.Background(), deps, []string{"--device", "UDID", "--json"}); exit != 0 {
		t.Fatalf("runAppsList exit = %d, stderr=%s", exit, stderr.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d: %v", len(rows), rows)
	}
	if rows[0]["bundleId"] != "com.foo" {
		t.Errorf("expected bundleId=com.foo (camelCase), got row=%v", rows[0])
	}
	// Sanity-check a couple of other camelCase JSON tags so a future refactor
	// that "fixes" them to bundleID/dynamicBytes-snake would fail loudly.
	if _, ok := rows[0]["dynamicBytes"]; !ok {
		t.Errorf("missing dynamicBytes key in JSON row: %v", rows[0])
	}
}

// TestRunApps_dispatchListAndProbe asserts the apps top-level dispatcher
// routes `list` into runAppsList and rejects `probe` (Task 12) with exit 2
// and a "not implemented" stderr message.
func TestRunApps_dispatchListAndProbe(t *testing.T) {
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "UDID", Name: "Phone"}}}
	lister := &apps.FakeLister{Apps: []apps.App{
		{BundleID: "com.foo", Name: "Foo"},
	}}

	t.Run("list routes to runAppsList", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}
		if exit := runApps(context.Background(), deps, []string{"list", "--device", "UDID"}); exit != 0 {
			t.Fatalf("runApps list exit = %d, stderr=%s", exit, stderr.String())
		}
		if !strings.Contains(stdout.String(), "com.foo") {
			t.Errorf("expected com.foo in stdout, got:\n%s", stdout.String())
		}
	})

	t.Run("probe not implemented yet", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}
		exit := runApps(context.Background(), deps, []string{"probe"})
		if exit != 2 {
			t.Errorf("probe exit = %d, want 2 (not implemented yet); stderr=%s", exit, stderr.String())
		}
		if !strings.Contains(stderr.String(), "not implemented") {
			t.Errorf("expected 'not implemented' in stderr, got: %q", stderr.String())
		}
	})

	t.Run("missing subcommand prints usage exit 2", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}
		if exit := runApps(context.Background(), deps, nil); exit != 2 {
			t.Errorf("empty args exit = %d, want 2", exit)
		}
	})
}
