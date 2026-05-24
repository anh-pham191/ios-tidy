package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

// trapSandbox fails the test if Open is ever called. Used by probe-gate
// tests to assert that a refused / missing probe MUST NOT result in a
// sandbox Open attempt — the gate has to short-circuit BEFORE that.
type trapSandbox struct {
	t *testing.T
}

func (s *trapSandbox) Open(_ context.Context, _, _ string) (sandbox.FS, error) {
	s.t.Fatalf("Sandbox.Open must not be called when probe gate refuses")
	return nil, errors.New("unreachable")
}

// loadingProbeStore is an in-memory ProbeStore that returns canned Load
// results. Save is a no-op for clean tests (clean never writes the probe
// store).
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

func TestAppsClean_missingBundleIDPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{Stdout: &stdout, Stderr: &stderr}, []string{})
	if exit == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "usage: ios-tidy apps clean BUNDLE_ID") {
		t.Errorf("stderr = %q, want usage line", stderr.String())
	}
}

func TestAppsClean_refusesWhenNoProbeResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox: &trapSandbox{t: t},
		Store:   &loadingProbeStore{}, // empty — no probe for any UDID
	}
	exit := runAppsClean(context.Background(), deps,
		[]string{"com.example.app", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "ios-tidy apps probe --bundle com.example.app") {
		t.Errorf("stderr should hint at probe; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "not been confirmed as vended") {
		t.Errorf("stderr should explain refusal; got: %q", stderr.String())
	}
}

func TestAppsClean_opensSandboxAfterProbeGate(t *testing.T) {
	// FakeFS has WalkResults seeded for tmp and Library/Caches (the default
	// targets when no --include-* flag is set). Documents/ is intentionally
	// empty so we can assert that flag-default = "tmp + caches, NOT docs".
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}

	var stdout, stderr bytes.Buffer
	deps := appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox: sb,
		Store:   store,
	}
	exit := runAppsClean(context.Background(), deps,
		[]string{"com.example.app", "--device", "U1"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if calls := sb.OpenCalls(); len(calls) != 1 || calls[0] != "com.example.app" {
		t.Fatalf("Sandbox.Open calls = %v, want [com.example.app]", calls)
	}
	if !fakeFS.Closed() {
		t.Errorf("FakeFS.Close was not called — handle leaked")
	}
	out := stdout.String()
	if !strings.Contains(out, "tmp") || !strings.Contains(out, "Library/Caches") {
		t.Errorf("stdout should render both default target names; got: %q", out)
	}
	if strings.Contains(out, "Documents") {
		t.Errorf("stdout should NOT include Documents when --include-documents was not passed; got: %q", out)
	}
	// Both files should appear in the totals (30 bytes across 2 targets).
	if !strings.Contains(out, "30") {
		t.Errorf("stdout should report 30-byte total; got: %q", out)
	}
}

func TestAppsClean_refusesWhenProbeIsRefused(t *testing.T) {
	var stdout, stderr bytes.Buffer
	deps := appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox: &trapSandbox{t: t},
		Store: &loadingProbeStore{
			Results: map[string][]apps.ProbeResult{
				"U1": {{
					BundleID: "com.example.app",
					Outcome:  apps.ProbeRefused,
					Detail:   "daemon refused",
				}},
			},
		},
	}
	exit := runAppsClean(context.Background(), deps,
		[]string{"com.example.app", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "not been confirmed as vended") {
		t.Errorf("stderr = %q, want refusal explanation", stderr.String())
	}
}
