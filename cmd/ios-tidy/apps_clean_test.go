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
	"github.com/anh-pham191/ios-tidy/internal/ui"
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
		// --yes skips the prompt so this test stays focused on the
		// sandbox-open path; the prompt itself is covered by the
		// basic-prompt tests below.
		Prompter: ui.NewFakePrompter(nil),
	}
	exit := runAppsClean(context.Background(), deps,
		[]string{"--device", "U1", "--yes", "com.example.app"})
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

// TestAppsClean_dryRunNeverCallsRemoveOrPrompter is the highest-value test in
// M6: it pins the safety net that says --dry-run reaches NEITHER the
// destructive FS calls (Remove/RemoveAll) NOR the Prompter. The FakePrompter
// has a ConfirmFn that t.Fatalf's, so any Confirm call would fail the test
// loudly rather than silently consuming a queued answer.
func TestAppsClean_dryRunNeverCallsRemoveOrPrompter(t *testing.T) {
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
	fp := &ui.FakePrompter{
		ConfirmFn: func(_ context.Context, _ string) (bool, error) {
			t.Fatalf("Prompter must not be called on --dry-run")
			return false, nil
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--dry-run", "com.example.app"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove was called under --dry-run: %v", fakeFS.RemoveCalls)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called under --dry-run: %v", fakeFS.RemoveAllCalls)
	}
	if len(fp.Asked) != 0 {
		t.Errorf("Prompter was asked under --dry-run: %v", fp.Asked)
	}
	if !strings.Contains(stdout.String(), "Dry run") {
		t.Errorf("stdout should announce dry run; got: %q", stdout.String())
	}
}

// TestAppsClean_dryRunWithDocumentsNeverCallsRemoveOrPrompter pins the
// dry-run guarantee for the Documents path specifically. The strict
// typed-bundle-ID gate (Task 13) MUST sit BELOW the dry-run short-circuit;
// this test catches any future refactor that accidentally inverts the order.
func TestAppsClean_dryRunWithDocumentsNeverCallsRemoveOrPrompter(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"Documents": {
				{Path: "Documents/secret.txt", Size: 100},
				{Path: "Documents/photos/img.jpg", Size: 2048},
			},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fp := &ui.FakePrompter{
		ConfirmFn: func(_ context.Context, _ string) (bool, error) {
			t.Fatalf("Prompter must not be called on --dry-run --include-documents")
			return false, nil
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-documents", "--dry-run", "com.example.app"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove was called under --dry-run --include-documents: %v", fakeFS.RemoveCalls)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called under --dry-run --include-documents: %v", fakeFS.RemoveAllCalls)
	}
	if len(fp.Asked) != 0 {
		t.Errorf("Prompter was reached under --dry-run: %v", fp.Asked)
	}
	if !strings.Contains(stdout.String(), "Documents") {
		t.Errorf("stdout should render the Documents target in the plan; got: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Dry run") {
		t.Errorf("stdout should announce dry run; got: %q", stdout.String())
	}
}

// TestAppsClean_basicPromptNoAborts pins the contract: when the user answers
// "no" at the basic y/N prompt, NO RemoveAll/Remove calls happen and exit is
// 0 (clean abort, not error).
func TestAppsClean_basicPromptNoAborts(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp": {{Path: "tmp/a", Size: 10}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fp := ui.NewFakePrompter([]bool{false})

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-tmp", "com.example.app"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0 (clean abort); stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll called after user said no: %v", fakeFS.RemoveAllCalls)
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove called after user said no: %v", fakeFS.RemoveCalls)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Errorf("stdout should announce abort; got: %q", stdout.String())
	}
}

// TestAppsClean_basicPromptYesProceeds pins the other side: when the user
// answers "yes", RemoveAll fires for every enabled non-Documents target and
// the summary line lands in stdout.
func TestAppsClean_basicPromptYesProceeds(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
		ListResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Name: "a", Path: "tmp/a"}},
			"Library/Caches": {{Name: "c", Path: "Library/Caches/c"}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fp := ui.NewFakePrompter([]bool{true})

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "com.example.app"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Fatalf("RemoveAllCalls = %v, want 2 (tmp + Library/Caches)", fakeFS.RemoveAllCalls)
	}
	if !strings.Contains(stdout.String(), "Deleted") {
		t.Errorf("stdout should contain summary; got: %q", stdout.String())
	}
	// Total bytes = 30 across both targets.
	if !strings.Contains(stdout.String(), "30") {
		t.Errorf("stdout should report 30 bytes freed; got: %q", stdout.String())
	}
}

// TestAppsClean_yesFlagSkipsBasicPrompt pins the --yes contract: the basic
// y/N prompt is bypassed. The FakePrompter has no queued answers, so any
// Confirm call would panic with "exhausted".
func TestAppsClean_yesFlagSkipsBasicPrompt(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp": {{Path: "tmp/a", Size: 1}},
		},
		ListResults: map[string][]sandbox.FileInfo{
			"tmp": {{Name: "a", Path: "tmp/a"}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fp := ui.NewFakePrompter(nil)

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-tmp", "--yes", "com.example.app"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.Asked) != 0 {
		t.Errorf("Prompter was asked even with --yes: %v", fp.Asked)
	}
	if len(fakeFS.RemoveAllCalls) != 1 {
		t.Errorf("RemoveAll calls = %v, want 1", fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_partialFailureReportsAndExitsNonZero pins the summary path
// when RemoveAll succeeds for one target but fails for another: stdout
// gets the summary, stderr gets per-failure lines, exit is 1.
func TestAppsClean_partialFailureReportsAndExitsNonZero(t *testing.T) {
	tmpFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 10}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 20}},
		},
		ListResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Name: "a", Path: "tmp/a"}},
			"Library/Caches": {{Name: "c", Path: "Library/Caches/c"}},
		},
		// Both targets share one FakeFS, so we can't fail just caches via
		// per-path Remove errors (those only apply to file-by-file Remove).
		// Use a function-style failure via RemoveAllErr instead — every
		// per-child RemoveAll fails, so we exercise the "all targets failed"
		// branch as a representative non-zero exit path.
		RemoveAllErr: errors.New("device disconnected"),
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: tmpFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: ui.NewFakePrompter(nil),
	}, []string{"--device", "U1", "--yes", "com.example.app"})

	if exit != 1 {
		t.Errorf("exit = %d, want 1 (failures present); stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "failure") {
		t.Errorf("stdout should mention failures in summary; got: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "device disconnected") {
		t.Errorf("stderr should include the underlying error; got: %q", stderr.String())
	}
}

// docsFixture returns a fake FS/sandbox/store wired for the Documents tests.
// The FakeFS seeds Documents/ Walk entries so BuildPlan finds files; the store
// is pre-loaded with a ProbeVended result for "com.example.app" on "U1".
func docsFixture() (*sandbox.FakeFS, *sandbox.FakeSandbox, *loadingProbeStore) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"Documents": {
				{Path: "Documents/secret.txt", Size: 100},
				{Path: "Documents/photos/img.jpg", Size: 2048},
			},
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

// TestAppsClean_documentsExactBundleMatchProceeds pins the happy path of the
// Task 13 strict typed-bundle-ID gate: when the user types the bundle ID
// exactly, the destructive RemoveAll on Documents/ fires and exit is 0.
func TestAppsClean_documentsExactBundleMatchProceeds(t *testing.T) {
	fakeFS, sb, store := docsFixture()
	fp := &ui.FakePrompter{Lines: []string{"com.example.app"}}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-documents", "com.example.app"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	// Documents uses per-file Remove (sandbox.Execute branches by target), so
	// every walked entry must appear in RemoveCalls.
	if len(fakeFS.RemoveCalls) != 2 {
		t.Errorf("RemoveCalls = %v, want 2 entries from Documents walk", fakeFS.RemoveCalls)
	}
	out := stdout.String()
	if !strings.Contains(out, "user data") {
		t.Errorf("stdout should warn about user data; got: %q", out)
	}
	if !strings.Contains(out, "NOT recoverable") {
		t.Errorf("stdout should warn files are NOT recoverable; got: %q", out)
	}
	if len(fp.AskedLines) != 1 {
		t.Errorf("AskedLines = %v, want exactly one strict-gate prompt", fp.AskedLines)
	}
	// The Documents path must NOT also fall through to the basic y/N Confirm.
	if len(fp.Asked) != 0 {
		t.Errorf("Confirm was called on Documents path: %v", fp.Asked)
	}
}

// TestAppsClean_documentsBundleMismatchAborts pins the abort path. Any typed
// value that isn't an exact case-sensitive match (after TrimSpace) aborts
// cleanly with exit 0, no destructive calls, and a "did not match" message.
func TestAppsClean_documentsBundleMismatchAborts(t *testing.T) {
	cases := []struct {
		name  string
		typed string
	}{
		{"typo", "com.example.ap"},
		{"empty", ""},
		{"different bundle", "com.example.other"},
		{"case mismatch", "COM.EXAMPLE.APP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fakeFS, sb, store := docsFixture()
			fp := &ui.FakePrompter{Lines: []string{tc.typed}}

			var stdout, stderr bytes.Buffer
			exit := runAppsClean(context.Background(), appsDeps{
				Stdout:   &stdout,
				Stderr:   &stderr,
				Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
				Sandbox:  sb,
				Store:    store,
				Prompter: fp,
			}, []string{"--device", "U1", "--include-documents", "com.example.app"})

			if exit != 0 {
				t.Errorf("exit = %d, want 0 (clean abort); stderr=%q", exit, stderr.String())
			}
			if len(fakeFS.RemoveAllCalls) != 0 {
				t.Errorf("RemoveAll was called after mismatch: %v", fakeFS.RemoveAllCalls)
			}
			if len(fakeFS.RemoveCalls) != 0 {
				t.Errorf("Remove was called after mismatch: %v", fakeFS.RemoveCalls)
			}
			if !strings.Contains(stdout.String(), "did not match") {
				t.Errorf("stdout should say 'did not match'; got: %q", stdout.String())
			}
		})
	}
}

// TestAppsClean_documentsTrailingNewlineMatches pins TrimSpace behavior: a
// trailing newline (as a real terminal would deliver) must not defeat the
// match.
func TestAppsClean_documentsTrailingNewlineMatches(t *testing.T) {
	fakeFS, sb, store := docsFixture()
	fp := &ui.FakePrompter{Lines: []string{"com.example.app\n"}}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-documents", "com.example.app"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveCalls) != 2 {
		t.Errorf("RemoveCalls = %v, want 2 after trimmed match", fakeFS.RemoveCalls)
	}
}

// TestAppsClean_yesDoesNotBypassDocumentsStrictGate is the safety pin: --yes
// MUST NOT skip the typed-bundle-ID gate. The Prompter has a queued line; we
// assert ReadLine was actually called once (gate ran) and that destructive
// calls only happened because the line matched.
func TestAppsClean_yesDoesNotBypassDocumentsStrictGate(t *testing.T) {
	fakeFS, sb, store := docsFixture()
	// Confirm must NEVER be called on the Documents path; ConfirmFn fails the
	// test if it is.
	fp := &ui.FakePrompter{
		Lines: []string{"com.example.app"},
		ConfirmFn: func(_ context.Context, q string) (bool, error) {
			t.Fatalf("Confirm must not be called on --include-documents path; got %q", q)
			return false, nil
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"--device", "U1", "--include-documents", "--yes", "com.example.app"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.AskedLines) != 1 {
		t.Errorf("AskedLines = %v, want exactly one strict-gate prompt (--yes must NOT skip it)", fp.AskedLines)
	}
	if len(fakeFS.RemoveCalls) != 2 {
		t.Errorf("RemoveCalls = %v, want 2 (gate cleared, per-file Remove fired)", fakeFS.RemoveCalls)
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

// TestAppsClean_openErrorHintsStaleProbe pins the safety hint emitted when the
// probe gate has approved a bundle ID (Outcome == ProbeVended) but the daemon
// nonetheless refuses to vend its sandbox at Open time. The most likely cause
// is a stale probe — the user installed/uninstalled or re-signed the app
// between `apps probe` and `apps clean` — so the error message MUST point them
// back at `apps probe` and explain the staleness possibility. Production code
// for this path lives in Task 10's Sandbox.Open error block; this test is a
// characterization lock so a future copy-edit of the hint wording can't
// silently regress the user-visible safety net.
// TestAppsClean_dryRunFlagAfterBundleIDIsHonored pins the fix for the
// HIGH-severity bug where flag.Parse stops at the first non-flag argument,
// silently dropping flags placed after the BUNDLE_ID positional. Before the
// fix, `ios-tidy apps clean com.example.app --dry-run` would IGNORE
// --dry-run and proceed to a real deletion after the y/N prompt — a
// data-loss-adjacent footgun. After the fix, --dry-run is honoured
// regardless of position.
func TestAppsClean_dryRunFlagAfterBundleIDIsHonored(t *testing.T) {
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
	fp := &ui.FakePrompter{
		ConfirmFn: func(_ context.Context, _ string) (bool, error) {
			t.Fatalf("Prompter must not be called on --dry-run")
			return false, nil
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"com.example.app", "--dry-run"})

	if exit != 0 {
		t.Errorf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fakeFS.RemoveCalls) != 0 {
		t.Errorf("Remove was called even though --dry-run came after BUNDLE_ID: %v", fakeFS.RemoveCalls)
	}
	if len(fakeFS.RemoveAllCalls) != 0 {
		t.Errorf("RemoveAll was called even though --dry-run came after BUNDLE_ID: %v", fakeFS.RemoveAllCalls)
	}
	if !strings.Contains(stdout.String(), "Dry run") {
		t.Errorf("stdout should announce dry run; got: %q", stdout.String())
	}
}

// TestAppsClean_deviceFlagAfterBundleIDIsHonored pins the same fix for
// --device. With two devices attached and --device placed AFTER the bundle
// ID, the old flag.Parse would silently drop the flag and produce an
// "ambiguous device" error. After the fix the named UDID is selected.
func TestAppsClean_deviceFlagAfterBundleIDIsHonored(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp": {{Path: "tmp/a", Size: 1}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U2": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout: &stdout,
		Stderr: &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1"}, {UDID: "U2"},
		}},
		Sandbox:  sb,
		Store:    store,
		Prompter: ui.NewFakePrompter(nil),
	}, []string{"com.example.app", "--device", "U2", "--yes"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (--device U2 should have disambiguated); stderr=%q", exit, stderr.String())
	}
	// Confirm the Sandbox.Open targeted the bundle on the selected device path.
	if calls := sb.OpenCalls(); len(calls) != 1 || calls[0] != "com.example.app" {
		t.Errorf("Sandbox.Open calls = %v, want [com.example.app]", calls)
	}
}

// TestAppsClean_includeDocumentsFlagAfterBundleIDIsHonored pins the fix for
// --include-documents placed after the positional. The strict typed-bundle-ID
// gate (AskedLines == 1) is the observable proof that the Documents path was
// activated.
func TestAppsClean_includeDocumentsFlagAfterBundleIDIsHonored(t *testing.T) {
	fakeFS, sb, store := docsFixture()
	fp := &ui.FakePrompter{Lines: []string{"com.example.app"}}

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"com.example.app", "--include-documents", "--yes"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.AskedLines) != 1 {
		t.Errorf("AskedLines = %v, want 1 (strict gate must run — proves --include-documents was honoured)", fp.AskedLines)
	}
	if len(fakeFS.RemoveCalls) != 2 {
		t.Errorf("RemoveCalls = %v, want 2 (Documents/ entries)", fakeFS.RemoveCalls)
	}
}

// TestAppsClean_yesFlagAfterBundleIDIsHonored pins the fix for --yes placed
// after the positional. The FakePrompter has no queued answers; if --yes
// were silently dropped, the basic Confirm would fire and panic with
// "exhausted".
func TestAppsClean_yesFlagAfterBundleIDIsHonored(t *testing.T) {
	fakeFS := &sandbox.FakeFS{
		WalkResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Path: "tmp/a", Size: 1}},
			"Library/Caches": {{Path: "Library/Caches/c", Size: 2}},
		},
		ListResults: map[string][]sandbox.FileInfo{
			"tmp":            {{Name: "a", Path: "tmp/a"}},
			"Library/Caches": {{Name: "c", Path: "Library/Caches/c"}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{FS: fakeFS})

	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	fp := ui.NewFakePrompter(nil) // empty queue — any Confirm call would panic

	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   &stdout,
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox:  sb,
		Store:    store,
		Prompter: fp,
	}, []string{"com.example.app", "--yes"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	if len(fp.Asked) != 0 {
		t.Errorf("Confirm was called even though --yes was passed (after positional): %v", fp.Asked)
	}
	// Default include-flag combo is tmp + caches, so two RemoveAll calls.
	if len(fakeFS.RemoveAllCalls) != 2 {
		t.Errorf("RemoveAllCalls = %v, want 2 (tmp + Library/Caches default targets)", fakeFS.RemoveAllCalls)
	}
}

// TestAppsClean_bogusFlagAfterBundleIDStillErrors pins the negative case:
// after the helper splits flags from positionals, an unknown flag must still
// produce a usage error (exit 2) — the helper hands unknown flags to
// flag.Parse, which writes a standard "flag provided but not defined"
// message to the FlagSet's output.
func TestAppsClean_bogusFlagAfterBundleIDStillErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
	}, []string{"com.example.app", "--bogus-flag"})

	if exit != 2 {
		t.Errorf("exit = %d, want 2 (unknown flag should error); stderr=%q", exit, stderr.String())
	}
}

func TestAppsClean_openErrorHintsStaleProbe(t *testing.T) {
	bang := errors.New("connect afc service failed")
	store := &loadingProbeStore{
		Results: map[string][]apps.ProbeResult{
			"U1": {{BundleID: "com.example.app", Outcome: apps.ProbeVended}},
		},
	}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.example.app", sandbox.FakeResponse{Err: bang})
	var stderr bytes.Buffer

	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:   new(bytes.Buffer),
		Stderr:   &stderr,
		Devices:  &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Store:    store,
		Sandbox:  sb,
		Prompter: ui.NewFakePrompter(nil),
	}, []string{"--device", "U1", "com.example.app"})

	if exit == 0 {
		t.Errorf("exit = 0, want non-zero")
	}
	// Split into two checks so a future copy-edit of the exact hint wording
	// doesn't silently break the safety hint. Both substrings together still
	// guarantee the user sees the command name AND the staleness explanation.
	if !strings.Contains(stderr.String(), "apps probe") {
		t.Errorf("stderr should mention the apps probe command; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "stale") {
		t.Errorf("stderr should explain the staleness possibility; got: %q", stderr.String())
	}
}

// TestAppsClean_rejectsAppleSystemBundle pins the defense-in-depth
// com.apple.* hard-reject: even if the probe store somehow carried a
// Vended outcome for a system bundle (test bug, hand-edited file,
// race), apps clean MUST refuse before opening any sandbox. ios-tidy
// is for third-party apps only.
func TestAppsClean_rejectsAppleSystemBundle(t *testing.T) {
	cases := []string{
		"com.apple.mobilemail",
		"com.apple.Music",
		"com.apple.mobilesafari",
	}
	for _, bundle := range cases {
		t.Run(bundle, func(t *testing.T) {
			// Pre-seed a Vended probe to prove the reject runs before the
			// probe gate (i.e. independent of probe state).
			store := &loadingProbeStore{
				Results: map[string][]apps.ProbeResult{
					"U1": {{BundleID: bundle, Outcome: apps.ProbeVended}},
				},
			}
			var stdout, stderr bytes.Buffer
			exit := runAppsClean(context.Background(), appsDeps{
				Stdout:  &stdout,
				Stderr:  &stderr,
				Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
				Sandbox: &trapSandbox{t: t},
				Store:   store,
			}, []string{bundle, "--device", "U1"})
			if exit == 0 {
				t.Fatalf("exit = 0 for system bundle %q, want non-zero", bundle)
			}
			if !strings.Contains(stderr.String(), "system app sandbox") {
				t.Errorf("stderr should explain the system-app refusal; got: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), bundle) {
				t.Errorf("stderr should echo the offending bundle; got: %q", stderr.String())
			}
		})
	}
}

// TestAppsClean_doesNotMatchComAppleWithoutDotSuffix pins the precise
// boundary of the reject regex: only `com.apple.<something>` is refused.
// A literal `com.apple` (no dot suffix) is a different reverse-DNS
// namespace (Apple itself uses none, but a third-party domain like
// `com.applesauce.app` MUST pass through).
func TestAppsClean_doesNotMatchComAppleWithoutDotSuffix(t *testing.T) {
	// "com.apple" exactly is a malformed bundle ID — there's no real app
	// with this — but the rejection rule must still distinguish it from
	// "com.apple.<rest>". The probe gate will refuse it (no Vended
	// entry), but the system-app reject MUST NOT fire because the gate
	// message for a missing probe is different from the system-app one.
	store := &loadingProbeStore{} // no probe results
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox: &trapSandbox{t: t},
		Store:   store,
	}, []string{"com.apple", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	// The expected refusal is the probe gate, NOT the system-app reject.
	if strings.Contains(stderr.String(), "system app sandbox") {
		t.Errorf("plain 'com.apple' must NOT be treated as a system bundle; got: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "not been confirmed as vended") {
		t.Errorf("stderr should be the probe-gate refusal; got: %q", stderr.String())
	}
}

// TestAppsClean_allowsApplesauceBundle pins the negative-side boundary:
// a third-party bundle like "com.applesauce.app" must pass through the
// system-app reject. (The probe gate will then refuse it for the same
// reason as any other un-probed bundle.)
func TestAppsClean_allowsApplesauceBundle(t *testing.T) {
	store := &loadingProbeStore{} // no probe results — gate will refuse
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Sandbox: &trapSandbox{t: t},
		Store:   store,
	}, []string{"com.applesauce.app", "--device", "U1"})
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if strings.Contains(stderr.String(), "system app sandbox") {
		t.Errorf("'com.applesauce.app' must NOT match the system-app reject; got: %q", stderr.String())
	}
}

// TestAppsClean_zeroDevicesExits0 pins the M1 spec contract: when no devices
// are attached, the command emits an informative stderr message and exits 0.
// `devices` and `storage` already follow this convention; `apps clean` (via
// resolveDevice's old error path) used to exit 1. The sentinel
// errNoDevicesAttached lets every caller treat empty-device as a clean
// non-error state.
func TestAppsClean_zeroDevicesExits0(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := runAppsClean(context.Background(), appsDeps{
		Stdout:  &stdout,
		Stderr:  &stderr,
		Devices: &device.FakeLister{Devices: []device.Device{}},
		Sandbox: &trapSandbox{t: t},
		Store:   &loadingProbeStore{},
	}, []string{"com.example.app"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (M1 spec: zero exit on empty device list)", exit)
	}
	if !strings.Contains(stderr.String(), "no devices attached") {
		t.Errorf("stderr should explain why nothing was done; got: %q", stderr.String())
	}
}
