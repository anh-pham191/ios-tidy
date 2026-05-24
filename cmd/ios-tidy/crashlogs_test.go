package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

func TestCrashLogsList_tableOutput_filtersByPatternAndPrintsBytes(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1", Name: "iPhone"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 1024, ModTime: time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)},
			{Path: "/Mail.crash", Size: 2048},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--pattern", "Chrome-*"}, &stdout, &stderr)

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "/Chrome-1.ips") {
		t.Fatalf("stdout missing /Chrome-1.ips: %q", out)
	}
	if strings.Contains(out, "/Mail.crash") {
		t.Fatalf("stdout should not include /Mail.crash (filtered out): %q", out)
	}
	if !strings.Contains(out, "1.0 KB") && !strings.Contains(out, "1 KB") {
		t.Fatalf("stdout should contain a human-readable size: %q", out)
	}
	// Pattern was applied client-side; the fake recorded the raw flag value.
	if len(cc.ListCalls) != 1 || cc.ListCalls[0].Pattern != "Chrome-*" {
		t.Fatalf("ListCalls = %+v, want one with Pattern=Chrome-*", cc.ListCalls)
	}
}

func TestCrashLogsList_jsonOutput_isCleanJSON(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10, ModTime: time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--json"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"path": "/A.ips"`) {
		t.Fatalf("stdout missing path JSON: %q", out)
	}
	if !strings.Contains(out, `"size": 10`) {
		t.Fatalf("stdout missing size JSON: %q", out)
	}
	// stderr should NOT contain the prompt or table chrome.
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty for clean JSON path, got %q", stderr.String())
	}
}

func TestCrashLogsList_errorsWhenMultipleDevicesAndNoFlag(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{
		{UDID: "U1", Name: "Phone1"},
		{UDID: "U2", Name: "Phone2"},
	}}
	cc := &crashlogs.FakeClient{}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "U1") || !strings.Contains(stderr.String(), "U2") {
		t.Fatalf("stderr should list both UDIDs: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Fatalf("client.List should not be called when selection fails")
	}
}

func TestCrashLogsList_propagatesClientError(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{ListErr: errors.New("boom")}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr should contain client error: %q", stderr.String())
	}
}

func TestCrashLogsPull_happyPath_singleBulkPullCallWhenNoConflicts(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 100},
			{Path: "/Chrome-2.ips", Size: 200},
		},
		PullResult: crashlogs.PullResult{Pulled: 2, Bytes: 300},
	}
	fp := ui.NewFakePrompter(nil) // no prompts expected — fresh out dir
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	args := []string{"--out", outDir, "--pattern", "Chrome-*"}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, args, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
	if got := cc.PullCalls[0]; got.UDID != "U1" || got.Pattern != "Chrome-*" || got.Dst != outDir {
		t.Fatalf("PullCalls[0] = %+v; want {U1, Chrome-*, %s}", got, outDir)
	}
	out := stdout.String()
	if !strings.Contains(out, "pulled 2 of 2") {
		t.Fatalf("stdout missing 'pulled 2 of 2': %q", out)
	}
	if !strings.Contains(out, "skipped 0") || !strings.Contains(out, "failed 0") {
		t.Fatalf("stdout summary shape wrong: %q", out)
	}
	if len(fp.Asked) != 0 {
		t.Fatalf("prompter should not have been asked; got %v", fp.Asked)
	}
}

func TestCrashLogsPull_overwritePromptNo_abortsEntireBulkPull(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 100},
			{Path: "/Chrome-2.ips", Size: 200},
		},
		PullResult: crashlogs.PullResult{Pulled: 2, Bytes: 300},
	}
	fp := ui.NewFakePrompter([]bool{false}) // user says no to the one conflict
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	// Pre-create one destination file so exactly one prompt fires.
	existing := filepath.Join(outDir, "Chrome-1.ips")
	if err := os.WriteFile(existing, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir}, &stdout, &stderr)

	if exit != 1 {
		t.Fatalf("exit = %d, want 1 (abort on declined overwrite)", exit)
	}
	if len(cc.PullCalls) != 0 {
		t.Fatalf("PullCalls = %d, want 0 (bulk pull must NOT start)", len(cc.PullCalls))
	}
	if len(fp.Asked) != 1 || !strings.Contains(fp.Asked[0], existing) {
		t.Fatalf("prompt should reference the dst path; got %v", fp.Asked)
	}
	if !strings.Contains(stderr.String(), "declined 1 overwrite") {
		t.Fatalf("stderr should report 'declined 1 overwrite': %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "pulled 0 of 2") || !strings.Contains(stdout.String(), "skipped 1") {
		t.Fatalf("stdout summary wrong: %q", stdout.String())
	}
	// The non-conflicting entry must NOT have been pulled to disk either,
	// because we aborted before calling Client.Pull at all.
	if _, err := os.Stat(filepath.Join(outDir, "Chrome-2.ips")); err == nil {
		t.Fatalf("Chrome-2.ips should not exist — bulk pull was aborted")
	}
}

func TestCrashLogsPull_forceFlag_bypassesOverwritePromptAndPullsOnce(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/Chrome-1.ips", Size: 100}},
		PullResult:  crashlogs.PullResult{Pulled: 1, Bytes: 100},
	}
	// FakePrompter with no answers: if Confirm is called, the test panics.
	fp := ui.NewFakePrompter(nil)
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: fp}

	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "Chrome-1.ips"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir, "--force"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.Asked) != 0 {
		t.Fatalf("Prompter should not have been called under --force; got %v", fp.Asked)
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
}

func TestCrashLogsPull_partialFailureFromAdapter_returnsNonZeroAndReportsCounts(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10},
			{Path: "/B.ips", Size: 20},
		},
		PullResult: crashlogs.PullResult{
			Pulled: 1,
			Bytes:  10,
			Failures: []crashlogs.Failure{
				{Path: "/B.ips", ErrMsg: "afc: permission denied"},
			},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", t.TempDir()}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1 (partial failure)", exit)
	}
	if len(cc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1 (single bulk pull)", len(cc.PullCalls))
	}
	if !strings.Contains(stdout.String(), "pulled 1 of 2") {
		t.Fatalf("stdout summary missing 'pulled 1 of 2': %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "failed 1") {
		t.Fatalf("stdout summary missing 'failed 1': %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "afc: permission denied") {
		t.Fatalf("stderr should detail the failure: %q", stderr.String())
	}
}

func TestCrashLogsPull_totalAdapterError_returnsNonZeroAndReportsAllAsFailed(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10},
			{Path: "/B.ips", Size: 20},
		},
		PullErr: errors.New("transport boom"),
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", t.TempDir()}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "transport boom") {
		t.Fatalf("stderr should contain transport error: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "pulled 0 of 2") || !strings.Contains(stdout.String(), "failed 2") {
		t.Fatalf("stdout summary wrong: %q", stdout.String())
	}
}

func TestCrashLogsPull_createsOutDirIfMissing(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{{Path: "/A.ips", Size: 10}},
		PullResult:  crashlogs.PullResult{Pulled: 1, Bytes: 10},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	parent := t.TempDir()
	outDir := filepath.Join(parent, "nested", "out") // does not exist
	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{"--out", outDir}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if info, err := os.Stat(outDir); err != nil || !info.IsDir() {
		t.Fatalf("out dir not created: stat err=%v", err)
	}
}

func TestCrashLogsPull_errorsWhenOutMissing(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsPull(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "--out") {
		t.Fatalf("stderr should mention --out: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Fatalf("List should not be called when --out missing")
	}
}
