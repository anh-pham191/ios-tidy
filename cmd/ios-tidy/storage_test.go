package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/storage"
)

// blockingStorage is a storage.Client that blocks on DeviceInfo until ctx is
// done. Used by TestStorageCmd_contextCancellation_aborts to prove the
// parallel fetch honours cancellation.
type blockingStorage struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockingStorage) DeviceInfo(ctx context.Context, _ string) (storage.DeviceInfo, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return storage.DeviceInfo{}, ctx.Err()
}

// blockingApps is the apps.Lister twin — also blocks on ctx.
type blockingApps struct{}

func (b *blockingApps) UserApps(ctx context.Context, _ string) ([]apps.App, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func newFixture() (*device.FakeLister, *storage.FakeClient, *apps.FakeLister) {
	dev := &device.FakeLister{Devices: []device.Device{
		{UDID: "udid-A", Name: "Phone", Model: "iPhone15,3", IOSVersion: "18.4"},
	}}
	st := &storage.FakeClient{Info: storage.DeviceInfo{
		Model:      "iPhone15,3",
		TotalBytes: 500_000_000_000,
		FreeBytes:  120_000_000_000,
		BlockSize:  4096,
	}}
	ap := &apps.FakeLister{Apps: []apps.App{
		{BundleID: "com.foo", Name: "Foo", Version: "1.0", Container: "/var/mobile/Containers/Data/Application/AAA", DynamicBytes: 200_000_000, StaticBytes: 100_000_000, FileSharingEnabled: true, ApplicationType: "User"},
		{BundleID: "com.bar", Name: "Bar", Version: "2.0", Container: "/var/mobile/Containers/Data/Application/BBB", DynamicBytes: 50_000_000, StaticBytes: 10_000_000, ApplicationType: "User"},
	}}
	return dev, st, ap
}

func TestStorageCmd_zeroDevices_returnsZeroExitWithStderr(t *testing.T) {
	dev := &device.FakeLister{Devices: nil}
	st, ap := &storage.FakeClient{}, &apps.FakeLister{}
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if !strings.Contains(stderr.String(), "no devices") {
		t.Fatalf("stderr = %q, want it to mention 'no devices'", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty, got %q", stdout.String())
	}
}

func TestStorageCmd_multipleDevices_missingFlag_errors(t *testing.T) {
	dev := &device.FakeLister{Devices: []device.Device{
		{UDID: "udid-A", Name: "PhoneA"},
		{UDID: "udid-B", Name: "PhoneB"},
	}}
	st, ap := &storage.FakeClient{}, &apps.FakeLister{}
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{}, dev, st, ap, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "udid-A") || !strings.Contains(stderr.String(), "udid-B") {
		t.Fatalf("stderr should list both UDIDs, got %q", stderr.String())
	}
	if len(st.Calls) != 0 || len(ap.Calls) != 0 {
		t.Fatalf("should not call storage/apps when device is ambiguous")
	}
}

func TestStorageCmd_deviceFlag_noMatch_errors(t *testing.T) {
	dev, st, ap := newFixture()
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{Device: "udid-Z"}, dev, st, ap, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "udid-Z") {
		t.Fatalf("stderr should mention the missing UDID, got %q", stderr.String())
	}
}

// TestStorageCmd_unknownDeviceExitsNonZeroWithNotAttached pins the
// post-refactor (backlog #33) contract: passing --device <unknown> when a
// device list IS reachable validates the override against that list and
// fails fast with the "not attached" wording shared by every other
// subcommand. Pre-refactor, storage used selectDevice with the "not
// connected" wording; this test would have passed with the loose substring
// match in TestStorageCmd_deviceFlag_noMatch_errors but is documented
// separately here so the wording shift cannot regress silently.
func TestStorageCmd_unknownDeviceExitsNonZeroWithNotAttached(t *testing.T) {
	dev := &device.FakeLister{Devices: []device.Device{{UDID: "U1", Name: "OnePhone"}}}
	st, ap := &storage.FakeClient{}, &apps.FakeLister{}
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{Device: "U9"}, dev, st, ap, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero for unknown --device override")
	}
	if !strings.Contains(stderr.String(), "not attached") {
		t.Fatalf("stderr should use the 'not attached' wording shared with every other subcommand, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "U9") {
		t.Fatalf("stderr should echo the unknown UDID, got %q", stderr.String())
	}
}

func TestStorageCmd_textOutput_singleDevice_rendersHeaderAndSortedTable(t *testing.T) {
	dev, st, ap := newFixture()
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "iPhone15,3") {
		t.Fatalf("missing model in header: %q", out)
	}
	if !strings.Contains(out, "120.0 GB") || !strings.Contains(out, "500.0 GB") {
		t.Fatalf("missing free/total in header: %q", out)
	}
	if !strings.Contains(out, "24.0%") {
		t.Fatalf("missing percentage in header: %q", out)
	}
	fooIdx := strings.Index(out, "com.foo")
	barIdx := strings.Index(out, "com.bar")
	if fooIdx == -1 || barIdx == -1 || fooIdx > barIdx {
		t.Fatalf("expected com.foo before com.bar in table:\n%s", out)
	}
	if !strings.Contains(out, "yes") {
		t.Fatalf("file-sharing column should render 'yes' for com.foo:\n%s", out)
	}
}

func TestStorageCmd_jsonOutput_singleDevice_fullShape(t *testing.T) {
	dev, st, ap := newFixture()
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{JSON: true}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", exit, stderr.String())
	}
	var parsed struct {
		Device struct {
			Model         string `json:"model"`
			AFCTotalBytes uint64 `json:"afcTotalBytes"`
			AFCFreeBytes  uint64 `json:"afcFreeBytes"`
			AFCBlockSize  uint64 `json:"afcBlockSize"`
		} `json:"device"`
		Apps []struct {
			BundleID           string `json:"bundleID"`
			Name               string `json:"name"`
			Version            string `json:"version"`
			Container          string `json:"container"`
			DynamicBytes       uint64 `json:"dynamicBytes"`
			StaticBytes        uint64 `json:"staticBytes"`
			FileSharingEnabled bool   `json:"fileSharingEnabled"`
			ApplicationType    string `json:"applicationType"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, stdout.String())
	}
	// Device fields.
	if parsed.Device.Model != "iPhone15,3" {
		t.Fatalf("device.model = %q, want iPhone15,3", parsed.Device.Model)
	}
	if parsed.Device.AFCTotalBytes != 500_000_000_000 {
		t.Fatalf("afcTotalBytes = %d, want 500_000_000_000", parsed.Device.AFCTotalBytes)
	}
	if parsed.Device.AFCFreeBytes != 120_000_000_000 {
		t.Fatalf("afcFreeBytes = %d, want 120_000_000_000", parsed.Device.AFCFreeBytes)
	}
	if parsed.Device.AFCBlockSize != 4096 {
		t.Fatalf("afcBlockSize = %d, want 4096", parsed.Device.AFCBlockSize)
	}
	// Apps slice: 2 entries, sorted (com.foo first by total bytes).
	if len(parsed.Apps) != 2 {
		t.Fatalf("len(apps) = %d, want 2", len(parsed.Apps))
	}
	a := parsed.Apps[0]
	if a.BundleID != "com.foo" {
		t.Fatalf("apps[0].bundleId = %q, want com.foo", a.BundleID)
	}
	if a.Name != "Foo" {
		t.Fatalf("apps[0].name = %q, want Foo", a.Name)
	}
	if a.Version != "1.0" {
		t.Fatalf("apps[0].version = %q, want 1.0", a.Version)
	}
	if a.Container != "/var/mobile/Containers/Data/Application/AAA" {
		t.Fatalf("apps[0].container = %q, want /var/mobile/Containers/Data/Application/AAA", a.Container)
	}
	if a.DynamicBytes != 200_000_000 {
		t.Fatalf("apps[0].dynamicBytes = %d, want 200_000_000", a.DynamicBytes)
	}
	if a.StaticBytes != 100_000_000 {
		t.Fatalf("apps[0].staticBytes = %d, want 100_000_000", a.StaticBytes)
	}
	if !a.FileSharingEnabled {
		t.Fatalf("apps[0].fileSharingEnabled = false, want true")
	}
	if a.ApplicationType != "User" {
		t.Fatalf("apps[0].applicationType = %q, want User", a.ApplicationType)
	}
	// Sanity-check apps[1].
	if parsed.Apps[1].BundleID != "com.bar" {
		t.Fatalf("apps[1].bundleId = %q, want com.bar", parsed.Apps[1].BundleID)
	}
}

func TestStorageCmd_limit_truncatesTable(t *testing.T) {
	dev, st, ap := newFixture()
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{Limit: 1}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	out := stdout.String()
	if !strings.Contains(out, "com.foo") {
		t.Fatalf("top-1 should include com.foo, got %q", out)
	}
	if strings.Contains(out, "com.bar") {
		t.Fatalf("top-1 should exclude com.bar, got %q", out)
	}
}

func TestStorageCmd_limit_greaterThanLen_returnsAll(t *testing.T) {
	dev, st, ap := newFixture()
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{Limit: 99}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	out := stdout.String()
	if !strings.Contains(out, "com.foo") || !strings.Contains(out, "com.bar") {
		t.Fatalf("--limit 99 should keep both rows, got %q", out)
	}
}

func TestStorageCmd_storageError_propagates(t *testing.T) {
	dev, st, ap := newFixture()
	st.Err = errors.New("afc closed")
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{}, dev, st, ap, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "afc closed") {
		t.Fatalf("stderr should include underlying error, got %q", stderr.String())
	}
}

func TestStorageCmd_appsError_propagates(t *testing.T) {
	dev, st, ap := newFixture()
	ap.Err = errors.New("proxy refused")
	var stdout, stderr bytes.Buffer

	exit := runStorage(context.Background(), storageOpts{}, dev, st, ap, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "proxy refused") {
		t.Fatalf("stderr should include underlying error, got %q", stderr.String())
	}
}

func TestStorageCmd_contextCancellation_aborts(t *testing.T) {
	dev := &device.FakeLister{Devices: []device.Device{
		{UDID: "udid-A", Name: "Phone", Model: "iPhone15,3"},
	}}
	st := &blockingStorage{started: make(chan struct{})}
	ap := &blockingApps{}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr bytes.Buffer

	done := make(chan int, 1)
	go func() {
		done <- runStorage(ctx, storageOpts{}, dev, st, ap, &stdout, &stderr)
	}()

	// Wait for the storage goroutine to actually be blocking inside
	// DeviceInfo, then cancel.
	select {
	case <-st.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("storage DeviceInfo never started")
	}
	cancel()

	select {
	case exit := <-done:
		if exit != 1 {
			t.Fatalf("exit = %d, want 1 (ctx cancelled)", exit)
		}
		if !strings.Contains(stderr.String(), "context canceled") {
			t.Fatalf("stderr should mention cancellation, got %q", stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("runStorage did not return after ctx cancellation — fetchInParallel not honouring ctx")
	}
}
