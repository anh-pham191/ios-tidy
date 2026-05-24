package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

// fakeProbeStore is an in-memory ProbeStore for command-level tests.
type fakeProbeStore struct {
	Saved     map[string][]apps.ProbeResult
	SaveCalls int
}

func (f *fakeProbeStore) Save(udid string, r []apps.ProbeResult) error {
	if f.Saved == nil {
		f.Saved = map[string][]apps.ProbeResult{}
	}
	f.Saved[udid] = append([]apps.ProbeResult(nil), r...)
	f.SaveCalls++
	return nil
}
func (f *fakeProbeStore) Load(_ string) ([]apps.ProbeResult, error) { return nil, nil }

func TestAppsProbe_requiresAllOrBundle(t *testing.T) {
	cmd := newAppsProbeCmd(appsDeps{})
	err := cmd.run(context.Background(), []string{"--device", "UDID"})
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("want error about missing --all/--bundle, got %v", err)
	}
}

func TestAppsProbe_rejectsBothAllAndBundle(t *testing.T) {
	cmd := newAppsProbeCmd(appsDeps{})
	err := cmd.run(context.Background(), []string{"--device", "UDID", "--all", "--bundle", "com.foo"})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("want mutually-exclusive error, got %v", err)
	}
}

func TestAppsProbe_allProbesEveryUserApp(t *testing.T) {
	lister := &apps.FakeLister{
		Apps: []apps.App{
			{BundleID: "com.a", Name: "A"},
			{BundleID: "com.b", Name: "B"},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})
	sb.SetResponse("com.b", sandbox.FakeResponse{Err: errors.New("VendContainer failed: denied")})
	store := &fakeProbeStore{}

	var out bytes.Buffer
	cmd := newAppsProbeCmd(appsDeps{
		Lister:  lister,
		Sandbox: sb,
		Store:   store,
		Stdout:  &out,
	})
	if err := cmd.run(context.Background(), []string{"--device", "UDID", "--all"}); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := sb.OpenCalls(); !slices.Equal(got, []string{"com.a", "com.b"}) {
		t.Errorf("Open calls = %v, want [com.a com.b]", got)
	}
	if store.SaveCalls != 1 {
		t.Errorf("SaveCalls = %d, want 1", store.SaveCalls)
	}
	saved := store.Saved["UDID"]
	if len(saved) != 2 {
		t.Fatalf("len(saved) = %d, want 2", len(saved))
	}
	gotByID := map[string]apps.ProbeOutcome{}
	for _, r := range saved {
		gotByID[r.BundleID] = r.Outcome
	}
	if gotByID["com.a"] != apps.ProbeVended {
		t.Errorf("com.a outcome = %v, want Vended", gotByID["com.a"])
	}
	if gotByID["com.b"] != apps.ProbeRefused {
		t.Errorf("com.b outcome = %v, want Refused", gotByID["com.b"])
	}

	tbl := out.String()
	if !strings.Contains(tbl, "com.a") || !strings.Contains(tbl, "vended") {
		t.Errorf("table missing com.a / vended:\n%s", tbl)
	}
	if !strings.Contains(tbl, "com.b") || !strings.Contains(tbl, "refused") {
		t.Errorf("table missing com.b / refused:\n%s", tbl)
	}
}

func TestAppsProbe_bundleFlagProbesExactlyThoseInOrder(t *testing.T) {
	lister := &apps.FakeLister{
		Apps: []apps.App{
			{BundleID: "com.a"}, {BundleID: "com.b"}, {BundleID: "com.c"},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.c", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})
	store := &fakeProbeStore{}

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Store: store, Stdout: &bytes.Buffer{}})
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--bundle", "com.c", "--bundle", "com.a"},
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := sb.OpenCalls(); !slices.Equal(got, []string{"com.c", "com.a"}) {
		t.Errorf("Open calls = %v, want [com.c com.a]", got)
	}
}

func TestAppsProbe_bundleNotInstalledYieldsUnknown(t *testing.T) {
	lister := &apps.FakeLister{
		Apps: []apps.App{{BundleID: "com.installed"}},
	}
	sb := sandbox.NewFakeSandbox()
	store := &fakeProbeStore{}

	var out bytes.Buffer
	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Store: store, Stdout: &out})
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--bundle", "com.ghost"},
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if len(sb.OpenCalls()) != 0 {
		t.Errorf("Open should not have been called for non-installed bundle; calls = %v", sb.OpenCalls())
	}
	saved := store.Saved["UDID"]
	if len(saved) != 1 || saved[0].Outcome != apps.ProbeUnknown {
		t.Errorf("saved = %v, want one ProbeUnknown row", saved)
	}
	if !strings.Contains(saved[0].Detail, "not installed") {
		t.Errorf("Detail = %q, want it to mention 'not installed'", saved[0].Detail)
	}
}

func TestAppsProbe_timeoutFlagAppliedPerProbe(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.hang"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.hang", sandbox.FakeResponse{Hang: true})
	store := &fakeProbeStore{}

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Store: store, Stdout: &bytes.Buffer{}})
	start := time.Now()
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--all", "--timeout", "30ms"},
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("probe didn't honour --timeout 30ms; took %v", elapsed)
	}

	saved := store.Saved["UDID"]
	if len(saved) != 1 {
		t.Fatalf("saved len = %d, want 1", len(saved))
	}
	if saved[0].Outcome != apps.ProbeError {
		t.Errorf("Outcome = %v, want ProbeError", saved[0].Outcome)
	}
	if !strings.Contains(saved[0].Detail, "timeout") {
		t.Errorf("Detail = %q, want it to contain 'timeout'", saved[0].Detail)
	}
}

func TestAppsProbe_storeDirOverrideHonoured(t *testing.T) {
	// t.TempDir() returns a path under os.TempDir(), which is allow-listed
	// by validateStoreDir — no IOS_TIDY_ALLOW_STORE_DIR escape hatch needed.
	dir := t.TempDir()
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Stdout: &bytes.Buffer{}})
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--all", "--store-dir", dir},
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "UDID.json")); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestAppsProbe_storeDirRejectsUnsafePath(t *testing.T) {
	t.Setenv("IOS_TIDY_ALLOW_STORE_DIR", "")

	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Stdout: &bytes.Buffer{}})
	err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--all", "--store-dir", "/"},
	)
	if err == nil {
		t.Fatal("run: err = nil; want a --store-dir validation error")
	}
	if !strings.Contains(err.Error(), "--store-dir") {
		t.Errorf("err = %q; want mention of --store-dir", err.Error())
	}
}

func TestAppsProbe_storeDirEscapeHatchAllowsAnyPath(t *testing.T) {
	t.Setenv("IOS_TIDY_ALLOW_STORE_DIR", "1")
	dir := t.TempDir()
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Stdout: &bytes.Buffer{}})
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--all", "--store-dir", dir},
	); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestAppsProbe_jsonOutputShape(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a", Name: "A"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{FS: &sandbox.FakeFS{}})
	store := &fakeProbeStore{}

	var out bytes.Buffer
	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Store: store, Stdout: &out})
	if err := cmd.run(context.Background(),
		[]string{"--device", "UDID", "--all", "--json"},
	); err != nil {
		t.Fatalf("run: %v", err)
	}

	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, out.String())
	}
	if len(rows) != 1 || rows[0]["bundleID"] != "com.a" || rows[0]["outcome"] != "vended" {
		t.Errorf("rows = %v", rows)
	}
}

func TestAppsProbe_exitsZeroEvenIfAllRefused(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.a", sandbox.FakeResponse{Err: errors.New("VendContainer failed: denied")})
	store := &fakeProbeStore{}

	cmd := newAppsProbeCmd(appsDeps{Lister: lister, Sandbox: sb, Store: store, Stdout: &bytes.Buffer{}})
	if err := cmd.run(context.Background(), []string{"--device", "UDID", "--all"}); err != nil {
		t.Errorf("run returned error, want nil even for ProbeRefused: %v", err)
	}
}

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
	if rows[0]["bundleID"] != "com.foo" {
		t.Errorf("expected bundleID=com.foo (camelCase per SHARED_CONTEXT.md §11), got row=%v", rows[0])
	}
	// Sanity-check a couple of other camelCase JSON tags so a future refactor
	// that "fixes" them to snake_case or PascalCase would fail loudly.
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

	t.Run("probe routes to runAppsProbe", func(t *testing.T) {
		// With no --all / --bundle, probe must surface a validation error
		// (exit 1) — proving the dispatcher reached the real handler rather
		// than the old "not implemented" stub.
		var stdout, stderr bytes.Buffer
		deps := appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr}
		exit := runApps(context.Background(), deps, []string{"probe", "--device", "UDID"})
		if exit != 1 {
			t.Errorf("probe exit = %d, want 1 (validation error); stderr=%s", exit, stderr.String())
		}
		if !strings.Contains(stderr.String(), "--all") {
			t.Errorf("expected '--all' guidance in stderr, got: %q", stderr.String())
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
