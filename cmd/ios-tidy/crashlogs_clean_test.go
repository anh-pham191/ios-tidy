package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// newCleanEnv wires the minimal set of fakes + buffers used by the
// crashlogs-clean tests. Tests that need dynamic behaviour set the *Fn
// fields on the returned fakes; tests that don't can use the zero-value
// canned fields. Stdout/Stderr buffers are returned so each test can
// assert on output without re-declaring locals.
func newCleanEnv() (
	*crashlogs.FakeClient, *device.FakeLister, *ui.FakePrompter,
	*bytes.Buffer, *bytes.Buffer,
) {
	return &crashlogs.FakeClient{},
		&device.FakeLister{},
		&ui.FakePrompter{},
		new(bytes.Buffer),
		new(bytes.Buffer)
}

func TestRunCrashlogsClean_unknownFlagReturnsUsageError(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--no-such-flag"})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("stderr missing flag-error message; got:\n%s", stderr.String())
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("Remove was called %d times on unknown-flag path, want 0", len(fc.RemoveCalls))
	}
}

// TestRunDeps_carriesListerField is a compile-time guard: if runDeps
// doesn't carry Lister, this file fails to compile.
func TestRunDeps_carriesListerField(t *testing.T) {
	var d runDeps
	var _ device.Lister = d.Lister
	_ = d
}

func TestRunCrashlogsClean_emptyEntriesExitsZeroWithoutPromptOrRemove(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return nil, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		t.Fatalf("prompt must not be called on empty entries path")
		return false, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("Remove was called %d times, want 0", len(fc.RemoveCalls))
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout should be empty; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "No matching crash logs.") {
		t.Fatalf("stderr missing empty-message; got:\n%s", stderr.String())
	}
}

func TestRunCrashlogsClean_planPrintedBeforePromptIsAsked(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{
			{Path: "/var/mobile/Library/Logs/CrashReporter/A.ips", Size: 1024},
		}, nil
	}
	var stderrAtPromptTime string
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		stderrAtPromptTime = stderr.String()
		return false, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (user aborted)", code)
	}
	if !strings.Contains(stderrAtPromptTime, "Plan: ") {
		t.Fatalf("stderr at prompt time missing plan header; got:\n%s", stderrAtPromptTime)
	}
	if !strings.Contains(stderrAtPromptTime, "/var/mobile/Library/Logs/CrashReporter/A.ips") {
		t.Fatalf("stderr at prompt time missing entry path; got:\n%s", stderrAtPromptTime)
	}
	if !strings.Contains(stderrAtPromptTime, "Total: 1 files, 1.0 KB") {
		t.Fatalf("stderr at prompt time missing total footer; got:\n%s", stderrAtPromptTime)
	}
}

func TestRunCrashlogsClean_promptFormatStringIsExact(t *testing.T) {
	cases := []struct {
		name     string
		entries  []crashlogs.Entry
		wantText string
	}{
		{
			name:     "singular entry uses 'file'",
			entries:  []crashlogs.Entry{{Path: "/a.ips", Size: 1024}},
			wantText: "Delete 1 file (1.0 KB) from device ABC123? [y/N]",
		},
		{
			name: "plural entries use 'files'",
			entries: []crashlogs.Entry{
				{Path: "/a.ips", Size: 1024},
				{Path: "/b.ips", Size: 2048},
			},
			wantText: "Delete 2 files (3.1 KB) from device ABC123? [y/N]",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fc, fl, fp, stdout, stderr := newCleanEnv()
			fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
				return []device.Device{{UDID: "ABC123"}}, nil
			}
			entries := tc.entries
			fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
				return entries, nil
			}
			var gotPrompt string
			fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
				gotPrompt = q
				return false, nil
			}
			_ = runCrashlogsClean(context.Background(), runDeps{
				Client: fc, Lister: fl, Prompter: fp,
				Stdout: stdout, Stderr: stderr,
			}, []string{})
			if gotPrompt != tc.wantText {
				t.Fatalf("prompt = %q, want %q", gotPrompt, tc.wantText)
			}
		})
	}
}

func TestRunCrashlogsClean_dryRunNeverCallsRemoveOrPrompter(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{
			{Path: "/a.ips", Size: 1024},
			{Path: "/b.ips", Size: 2048},
		}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called on --dry-run")
		return crashlogs.RemoveResult{}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		t.Fatalf("Prompter must not be called on --dry-run")
		return false, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--dry-run"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
	}
	body := stderr.String()
	if !strings.Contains(body, "Plan: ") {
		t.Fatalf("stderr missing plan header; got:\n%s", body)
	}
	if !strings.Contains(body, "Total: 2 files, 3.1 KB") {
		t.Fatalf("stderr missing total footer; got:\n%s", body)
	}
	if !strings.Contains(body, "Dry run — no changes made.") {
		t.Fatalf("stderr missing dry-run notice; got:\n%s", body)
	}
}

func TestRunCrashlogsClean_dryRunBeatsYesNeitherCallsRemoveNorPrompts(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called when --dry-run is set, even with --yes")
		return crashlogs.RemoveResult{}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		t.Fatalf("Prompter must not be called when --dry-run is set, even with --yes")
		return false, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--dry-run", "--yes"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
	}
	if !strings.Contains(stderr.String(), "Dry run — no changes made.") {
		t.Fatalf("stderr missing dry-run notice; got:\n%s", stderr.String())
	}
}
