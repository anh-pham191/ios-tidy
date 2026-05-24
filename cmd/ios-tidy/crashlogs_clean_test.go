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
