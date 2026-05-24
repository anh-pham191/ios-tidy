//go:build device
// +build device

package iosbackend

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// requireDestructiveDevice returns a UDID for tests that delete on-device
// data. It skips the test unless BOTH IOS_TIDY_TEST_UDID and
// IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1 are set. M5/M6 device tests should
// reuse this helper.
func requireDestructiveDevice(t *testing.T) string {
	t.Helper()
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID not set; integration test requires a real device")
	}
	if os.Getenv("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE") != "1" {
		t.Skip("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE != 1; refusing to run destructive integration test")
	}
	return udid
}

// TestCrashlogsClient_RemoveDeletesOneFile_device targets a single, named
// crash log on the connected device and verifies it is gone afterwards.
// It deliberately avoids wildcard deletion: the destructive blast radius
// is exactly one file, and only when both UDID and ALLOW_DESTRUCTIVE are
// set.
//
// In addition to "the targeted basename is gone" the test asserts:
//   - res.Bytes > 0, so the C1 regression (silently-zero bytes) cannot ship
//     again,
//   - the total entry count drops by at least one across the destructive
//     window (catches the case where a new crash file appears mid-test).
func TestCrashlogsClient_RemoveDeletesOneFile_device(t *testing.T) {
	udid := requireDestructiveDevice(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := NewCrashLogs()

	beforeEntries, err := c.List(ctx, udid, "*")
	if err != nil {
		t.Fatalf("List before: %v", err)
	}
	if len(beforeEntries) == 0 {
		t.Skip("no crash logs on device — cannot test destructive flow")
	}

	target := beforeEntries[0]
	if target.Size <= 0 {
		// Pick the first entry with a positive reported size, so the
		// bytes-freed assertion below is meaningful.
		for _, e := range beforeEntries {
			if e.Size > 0 {
				target = e
				break
			}
		}
	}
	targetName := filepath.Base(target.Path)

	// Use the basename as a fully-qualified pattern: filepath.Match against
	// the literal filename matches only that one file.
	res, err := c.Remove(ctx, udid, targetName)
	if err != nil {
		t.Fatalf("Remove(%q): %v", targetName, err)
	}
	if res.Removed != 1 {
		t.Fatalf("Removed = %d, want 1", res.Removed)
	}
	// Regression guard for cycle-1 review finding C1.
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0 (C1 regression — statCrashReport must report real sizes)", res.Bytes)
	}

	// Verify gone (basename-specific).
	afterEntries, err := c.List(ctx, udid, "*")
	if err != nil {
		t.Fatalf("List after Remove: %v", err)
	}
	for _, e := range afterEntries {
		if filepath.Base(e.Path) == targetName {
			t.Fatalf("file %q still present after Remove", targetName)
		}
	}
	// Total count bound — accounts for the possibility of a new crash file
	// appearing between snapshot and verify.
	if len(afterEntries) > len(beforeEntries)-1 {
		t.Errorf("entry count after = %d, want <= %d (before %d - 1)",
			len(afterEntries), len(beforeEntries)-1, len(beforeEntries))
	}
}
