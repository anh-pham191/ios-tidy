//go:build device
// +build device

// internal/iosbackend/crashlogs_device_test.go
package iosbackend

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDevice_CrashLogs_ListAndPullIntoTempDir(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID unset; skipping device integration test")
	}

	c := NewCrashLogs()
	// 60s budget: one ListReports + one DownloadReports walk is well under
	// 30s on a phone with hundreds of crashes (single AFC connection, single
	// walk); the extra headroom covers the per-path Stat round-trips in List.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	entries, err := c.List(ctx, udid, "*")
	if err != nil {
		t.Fatalf("List err = %v", err)
	}
	t.Logf("found %d crash entries", len(entries))
	// Do not assert non-empty; a freshly-wiped phone may legitimately have zero.

	dst := t.TempDir()
	res, err := c.Pull(ctx, udid, "*", dst)
	if err != nil {
		t.Fatalf("Pull err = %v", err)
	}
	t.Logf("pulled %d entries (%d bytes), %d failures", res.Pulled, res.Bytes, len(res.Failures))
	// Cardinal rule: M3 is read-only. We do NOT call Remove here.
}
