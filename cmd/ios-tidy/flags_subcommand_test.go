// cmd/ios-tidy/flags_subcommand_test.go
//
// Characterization + flag-after-positional tests for every subcommand
// flag-parse site. The helper splitFlagsAndPositionals was originally
// only applied to `apps clean` (the only subcommand with a positional
// at the time). Applying it everywhere is defensive: today no other
// subcommand accepts a positional, so the trap doesn't bite — but the
// next person to add one (e.g. `crashlogs pull DIR`) would otherwise
// introduce silent flag-drop. Tests below confirm:
//
//  1. Existing flag parsing still works the same way (characterization).
//  2. Unknown positionals produce a usage error (so a future positional
//     can't slip in without an explicit decision).
//
// Coverage: storage, crashlogs list/pull/clean, apps list/probe.
package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/storage"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// ----------------------------------------------------------------------
// storage
// ----------------------------------------------------------------------

func TestStorageCmd_flagParseStillWorks(t *testing.T) {
	// dispatch wires the real iosbackend for storage; characterize the
	// flag-parse behaviour through runStorage directly with fakes.
	dev := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	st := &storage.FakeClient{Info: storage.DeviceInfo{Model: "iPhoneX,1"}}
	ap := &apps.FakeLister{}
	var stdout, stderr bytes.Buffer
	exit := runStorage(context.Background(),
		storageOpts{Device: "U1"}, dev, st, ap, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(st.Calls) == 0 {
		t.Errorf("storage.DeviceInfo should have been called")
	}
}

func TestStorageCmd_rejectsUnknownPositional(t *testing.T) {
	dev := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	var stdout, stderr bytes.Buffer
	exit := dispatch(context.Background(), &stdout, &stderr,
		[]string{"storage", "some-extra-arg", "--device", "U1"},
		dev, &device.FakeTrustChecker{})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (unknown positional must error)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should mention positional; got: %q", stderr.String())
	}
}

// ----------------------------------------------------------------------
// crashlogs list
// ----------------------------------------------------------------------

func TestCrashLogsList_flagParseStillWorks(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashLogsList(context.Background(), deps, []string{"--device", "U1", "--pattern", "*"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(cc.ListCalls) != 1 {
		t.Errorf("expected one List call; got %d", len(cc.ListCalls))
	}
}

func TestCrashLogsList_rejectsUnknownPositional(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashLogsList(context.Background(), deps,
		[]string{"some-extra-arg", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (unknown positional must error)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should mention positional; got: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Errorf("List should not have been called when positional is rejected; got %d calls", len(cc.ListCalls))
	}
}

// ----------------------------------------------------------------------
// crashlogs pull
// ----------------------------------------------------------------------

func TestCrashLogsPull_flagParseStillWorks(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/a.ips", Size: 1}},
		PullResult:  crashlogs.PullResult{Pulled: 1, Bytes: 1},
	}
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashLogsPull(context.Background(), deps,
		[]string{"--device", "U1", "--out", outDir, "--force"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
}

func TestCrashLogsPull_rejectsUnknownPositional(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	outDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashLogsPull(context.Background(), deps,
		[]string{"some-extra-arg", "--device", "U1", "--out", outDir})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (unknown positional must error)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should mention positional; got: %q", stderr.String())
	}
}

// ----------------------------------------------------------------------
// crashlogs clean
// ----------------------------------------------------------------------

func TestCrashLogsClean_flagParseStillWorks(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/a.ips", Size: 1}},
	}
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashlogsClean(context.Background(), deps,
		[]string{"--device", "U1", "--dry-run"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
}

func TestCrashLogsClean_rejectsUnknownPositional(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	var stdout, stderr bytes.Buffer
	deps := runDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil), Stdout: &stdout, Stderr: &stderr}
	exit := runCrashlogsClean(context.Background(), deps,
		[]string{"some-extra-arg", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (unknown positional must error)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should mention positional; got: %q", stderr.String())
	}
}

// ----------------------------------------------------------------------
// apps list
// ----------------------------------------------------------------------

func TestAppsList_flagParseStillWorks(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a", Name: "A"}}}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	var stdout, stderr bytes.Buffer
	exit := runAppsList(context.Background(),
		appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr},
		[]string{"--device", "U1"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "com.a") {
		t.Errorf("stdout should contain com.a; got: %q", stdout.String())
	}
}

func TestAppsList_rejectsUnknownPositional(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	var stdout, stderr bytes.Buffer
	exit := runAppsList(context.Background(),
		appsDeps{Lister: lister, Devices: devs, Stdout: &stdout, Stderr: &stderr},
		[]string{"some-extra-arg", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (unknown positional must error)")
	}
	if !strings.Contains(stderr.String(), "positional") {
		t.Errorf("stderr should mention positional; got: %q", stderr.String())
	}
}

// ----------------------------------------------------------------------
// apps probe
// ----------------------------------------------------------------------

func TestAppsProbe_flagParseStillWorks(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	store := &fakeProbeStore{}
	cmd := newAppsProbeCmd(appsDeps{
		Lister:  lister,
		Devices: devs,
		Sandbox: nil, // not consulted for non-installed bundle
		Store:   store,
		Stdout:  &bytes.Buffer{},
	})
	// --bundle of a non-installed ID yields ProbeUnknown without any
	// Sandbox.Open dial — proves flag parsing + the rest of the pipeline.
	if err := cmd.run(context.Background(),
		[]string{"--device", "U1", "--bundle", "com.ghost"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestAppsProbe_rejectsUnknownPositional(t *testing.T) {
	lister := &apps.FakeLister{Apps: []apps.App{{BundleID: "com.a"}}}
	devs := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cmd := newAppsProbeCmd(appsDeps{
		Lister:  lister,
		Devices: devs,
		Stdout:  &bytes.Buffer{},
	})
	err := cmd.run(context.Background(),
		[]string{"some-extra-arg", "--device", "U1", "--bundle", "com.a"})
	if err == nil {
		t.Fatal("expected error for unknown positional; got nil")
	}
	if !strings.Contains(err.Error(), "positional") {
		t.Errorf("err = %q; want it to mention 'positional'", err.Error())
	}
}
