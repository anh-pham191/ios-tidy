//go:build device
// +build device

package iosbackend

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/house_arrest"
)

// TestSandboxClean_endToEnd is a destructive integration test that pushes a
// sentinel file into a real device's app sandbox tmp/ and verifies our
// sandbox.BuildPlan + sandbox.Execute machinery sees it and removes it.
//
// It is gated behind THREE env vars (all required) so `make test-device` is
// safely runnable without a device or without opting in to destruction:
//
//   - IOS_TIDY_TEST_UDID                  — target device UDID
//   - IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1   — explicit destructive opt-in
//   - IOS_TIDY_TEST_SENTINEL_BUNDLE_ID    — a dev/TestFlight app installed on
//     the device whose tmp/ we may write to and clear
//
// Any of these missing → t.Skip (NOT t.Fatal). The `device` build tag keeps
// the file out of normal `go test ./...` runs entirely.
//
// WARNING: this test exercises the production `sandbox.Execute(TargetTmp)`
// code path on a real device. Execute walks one level into `tmp/` and
// RemoveAll's every top-level child — i.e. the test still WIPES the entire
// `tmp/` directory of the chosen sentinel bundle (the directory NODE
// survives; its contents do not). Pick a sentinel app whose tmp/ you do
// NOT care about. The sentinel sub-directory naming is documented for
// auditability, not for isolation.
func TestSandboxClean_endToEnd(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID not set")
	}
	if os.Getenv("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE") != "1" {
		t.Skip("IOS_TIDY_TEST_ALLOW_DESTRUCTIVE != 1 — refusing to delete on a real device")
	}
	bundleID := os.Getenv("IOS_TIDY_TEST_SENTINEL_BUNDLE_ID")
	if bundleID == "" {
		t.Skip("IOS_TIDY_TEST_SENTINEL_BUNDLE_ID not set — supply a TestFlight/dev-signed app you have installed")
	}

	dev, err := ios.GetDevice(udid)
	if err != nil {
		t.Fatalf("GetDevice: %v", err)
	}
	afcClient, err := house_arrest.New(dev, bundleID)
	if err != nil {
		t.Fatalf("house_arrest.New: %v", err)
	}
	defer afcClient.Close()

	// Push a sentinel file into tmp/ via the raw go-ios client. Push is not
	// part of our sandbox.FS interface (it's a test-only escape hatch — the
	// production seam is read + delete, never write).
	//
	// The sentinel is staged inside a dedicated subdir (`tmp/ios-tidy-sentinel-test/`)
	// rather than directly at `tmp/<name>`. The subdir scopes the sentinel
	// to a recognizable namespace for auditing, but does NOT shield other
	// `tmp/` entries from the production Execute path — Execute(TargetTmp)
	// still RemoveAlls every top-level child of `tmp/` (see the warning
	// above on this test function). The subdir is for naming hygiene; the
	// blast radius is the whole `tmp/`.
	const sentinelSubdir = "ios-tidy-sentinel-test"
	sentinelName := fmt.Sprintf("ios-tidy-sentinel-%d.txt", time.Now().UnixNano())
	tmpHostFile := filepath.Join(t.TempDir(), sentinelName)
	if err := os.WriteFile(tmpHostFile, []byte("ios-tidy test"), 0o644); err != nil {
		t.Fatalf("write host sentinel: %v", err)
	}
	// MkDir is idempotent on AFC — if a previous test run left the dir
	// behind we proceed; any error is reported but not fatal because the
	// Push below will surface a clearer failure if the dir truly doesn't
	// exist.
	_ = afcClient.MkDir("tmp/" + sentinelSubdir)
	devicePath := "tmp/" + sentinelSubdir + "/" + sentinelName
	if err := afcClient.Push(tmpHostFile, devicePath); err != nil {
		t.Fatalf("Push sentinel: %v", err)
	}

	// Wrap the raw client in the same sandbox.FS adapter the production
	// Open path uses (afcFS lives in sandbox.go). This is the only place
	// outside Open that constructs afcFS directly — justified because we
	// already own the *afc.Client.
	fs := &afcFS{c: afcClient}

	plan, err := sandbox.BuildPlan(context.Background(), fs, sandbox.TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.TotalBytes == 0 {
		t.Fatalf("BuildPlan returned zero bytes; sentinel push may have failed")
	}

	res := sandbox.Execute(context.Background(), fs, plan)
	if len(res.Failures) != 0 {
		t.Errorf("Execute failures: %+v", res.Failures)
	}

	// Re-build the plan: the sentinel must be gone. We don't insist on
	// TotalBytes == 0 because the app under test may have written its own
	// files into tmp/ between our two walks — we only assert OUR sentinel
	// (and its containing subdir) were actually removed.
	plan2, err := sandbox.BuildPlan(context.Background(), fs, sandbox.TargetTmp)
	if err != nil {
		t.Fatalf("BuildPlan post-clean: %v", err)
	}
	for _, f := range plan2.Files {
		if filepath.Base(f.Path) == sentinelName {
			t.Errorf("sentinel %s still present after clean", sentinelName)
		}
	}
	_ = devicePath
}
