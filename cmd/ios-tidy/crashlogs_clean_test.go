package main

import (
	"bytes"
	"context"
	"errors"
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
	var stdoutAtPromptTime string
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		stdoutAtPromptTime = stdout.String()
		return false, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (user aborted)", code)
	}
	if !strings.Contains(stdoutAtPromptTime, "Plan: ") {
		t.Fatalf("stdout at prompt time missing plan header; got:\n%s", stdoutAtPromptTime)
	}
	if !strings.Contains(stdoutAtPromptTime, "/var/mobile/Library/Logs/CrashReporter/A.ips") {
		t.Fatalf("stdout at prompt time missing entry path; got:\n%s", stdoutAtPromptTime)
	}
	if !strings.Contains(stdoutAtPromptTime, "Total: 1 files, 1.0 KB") {
		t.Fatalf("stdout at prompt time missing total footer; got:\n%s", stdoutAtPromptTime)
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
	body := stdout.String()
	if !strings.Contains(body, "Plan: ") {
		t.Fatalf("stdout missing plan header; got:\n%s", body)
	}
	if !strings.Contains(body, "Total: 2 files, 3.1 KB") {
		t.Fatalf("stdout missing total footer; got:\n%s", body)
	}
	if !strings.Contains(body, "Dry run — no changes made.") {
		t.Fatalf("stdout missing dry-run notice; got:\n%s", body)
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
	if !strings.Contains(stdout.String(), "Dry run — no changes made.") {
		t.Fatalf("stdout missing dry-run notice; got:\n%s", stdout.String())
	}
}

func TestRunCrashlogsClean_yesFlagSkipsPromptAndRendersPlan(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) {
		t.Fatalf("Prompter must not be called when --yes is set")
		return false, nil
	}
	_ = runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes"})
	if !strings.Contains(stdout.String(), "Plan: ") {
		t.Fatalf("stdout missing plan header (--yes must still render plan); got:\n%s", stdout.String())
	}
}

func TestRunCrashlogsClean_yesCallsRemoveOnceWithUDIDAndPattern(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
	}
	_ = runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes", "--pattern=*.ips"})
	if len(fc.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %d, want 1", len(fc.RemoveCalls))
	}
	if fc.RemoveCalls[0].UDID != "ABC123" || fc.RemoveCalls[0].Pattern != "*.ips" {
		t.Fatalf("RemoveCalls[0] = %+v, want {UDID:ABC123 Pattern:*.ips}", fc.RemoveCalls[0])
	}
}

func TestRunCrashlogsClean_successSummaryFormat(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Deleted 1 of 1 files (1.0 KB freed). 0 failures.") {
		t.Fatalf("stdout missing summary; got:\n%s", stdout.String())
	}
}

func TestRunCrashlogsClean_promptYesProceedsRemoveCalledOnce(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{Removed: 1, Bytes: 1024}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return true, nil }
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls = %d, want 1", len(fc.RemoveCalls))
	}
}

func TestRunCrashlogsClean_promptNoAbortsWithoutRemove(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called when user answers 'n'")
		return crashlogs.RemoveResult{}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return false, nil }
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Fatalf("stdout missing 'Aborted.'; got:\n%s", stdout.String())
	}
}

func TestRunCrashlogsClean_promptEOFTreatedAsNo(t *testing.T) {
	// Per ui.Prompter.Confirm contract (SHARED_CONTEXT.md §3): EOF returns
	// (false, nil). clean must treat this exactly like "n".
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called on EOF stdin")
		return crashlogs.RemoveResult{}, nil
	}
	fp.ConfirmFn = func(ctx context.Context, q string) (bool, error) { return false, nil }
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0 on EOF path", len(fc.RemoveCalls))
	}
	if !strings.Contains(stdout.String(), "Aborted.") {
		t.Fatalf("stdout missing 'Aborted.' on EOF; got:\n%s", stdout.String())
	}
}

func TestRunCrashlogsClean_partialFailureSummaryAndNonZeroExit(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{
			{Path: "/a.ips", Size: 1024},
			{Path: "/b.ips", Size: 2048},
			{Path: "/c.ips", Size: 4096},
		}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{
			Removed: 2,
			Bytes:   1024 + 2048,
			Failures: []crashlogs.Failure{
				{Path: "/c.ips", Err: errors.New("afc: permission denied")},
			},
		}, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes"})
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (partial failure)", code)
	}
	if !strings.Contains(stdout.String(), "Deleted 2 of 3 files (3.1 KB freed). 1 failures.") {
		t.Fatalf("stdout missing summary; got:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "/c.ips: afc: permission denied") {
		t.Fatalf("stderr missing per-failure detail; got:\n%s", stderr.String())
	}
}

func TestRunCrashlogsClean_cancelledContextAbortsBeforeRemove(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called on cancelled context")
		return crashlogs.RemoveResult{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately
	code := runCrashlogsClean(ctx, runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes"})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero on cancelled context")
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
	}
}

func TestRunCrashlogsClean_listErrorExitsNonZeroAndDoesNotRemove(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return nil, errors.New("lockdown: connection refused")
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called when List errored")
		return crashlogs.RemoveResult{}, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if len(fc.RemoveCalls) != 0 {
		t.Fatalf("RemoveCalls = %d, want 0", len(fc.RemoveCalls))
	}
	if !strings.Contains(stderr.String(), "list crash logs: lockdown: connection refused") {
		t.Fatalf("stderr missing wrapped list error; got:\n%s", stderr.String())
	}
}

func TestRunCrashlogsClean_removeWholeCallErrorExitsNonZero(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return []crashlogs.Entry{{Path: "/a.ips", Size: 1024}}, nil
	}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		return crashlogs.RemoveResult{}, errors.New("afc: service vanished")
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"--yes"})
	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "remove crash logs: afc: service vanished") {
		t.Fatalf("stderr missing wrapped remove error; got:\n%s", stderr.String())
	}
}

// TestRunCrashlogs_dispatchesCleanSubcommand pins that the top-level
// `runCrashLogs` dispatcher routes `crashlogs clean` to runCrashlogsClean.
// The empty-entries path is used as a cheap, side-effect-free probe: if
// the dispatcher correctly forwards the call, we observe the
// "No matching crash logs." notice that runCrashlogsClean emits when
// Client.List returns nil entries. If the `clean` arm is removed or
// misrouted, this test catches it.
// TestCrashLogsClean_zeroDevicesExits0 pins the M1 spec contract: when no
// devices are attached, the command emits an informative stderr message and
// exits 0. The sentinel errNoDevicesAttached lets resolveDevice signal "no
// devices" distinctly from "lookup failed" so the caller can map empty to 0.
func TestCrashLogsClean_zeroDevicesExits0(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	// Empty device list — resolveDevice should return the sentinel.
	fl.Devices = []device.Device{}
	fc.RemoveFn = func(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
		t.Fatalf("Remove must not be called when no devices are attached")
		return crashlogs.RemoveResult{}, nil
	}
	code := runCrashlogsClean(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{})
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (M1 spec: zero exit on empty device list)", code)
	}
	if !strings.Contains(stderr.String(), "no devices attached") {
		t.Errorf("stderr should explain why nothing was done; got: %q", stderr.String())
	}
}

func TestRunCrashlogs_dispatchesCleanSubcommand(t *testing.T) {
	fc, fl, fp, stdout, stderr := newCleanEnv()
	fl.ListFn = func(ctx context.Context) ([]device.Device, error) {
		return []device.Device{{UDID: "ABC123"}}, nil
	}
	fc.ListFn = func(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
		return nil, nil
	}
	code := runCrashLogs(context.Background(), runDeps{
		Client: fc, Lister: fl, Prompter: fp,
		Stdout: stdout, Stderr: stderr,
	}, []string{"clean"})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (empty-entries path)", code)
	}
	if !strings.Contains(stderr.String(), "No matching crash logs.") {
		t.Fatalf("stderr missing empty-entries notice; got:\n%s", stderr.String())
	}
}
