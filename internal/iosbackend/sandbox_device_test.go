//go:build device
// +build device

// internal/iosbackend/sandbox_device_test.go
//
// Integration test for the real Sandbox.Open adapter (M5 Task 14). Gated on
// IOS_TIDY_TEST_UDID; skips cleanly without it. Probing is read-only — we
// dial house_arrest, classify the result, and close — so no
// IOS_TIDY_TEST_ALLOW_DESTRUCTIVE gate is required.
//
// The test does NOT assert a specific outcome: per RESEARCH.md §3 the
// daemon's vending policy is variable across iOS versions and undocumented.
// We assert the probe plumbing terminates cleanly and emits a real outcome;
// the t.Logf lines capture the empirical answer for the open question.
package iosbackend

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
)

func TestSandbox_probe_systemAppRefusedOrUnknown(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID unset; skipping device integration test")
	}

	prober := apps.NewProber(NewSandbox())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Apple-installed app; daemon almost certainly will not vend.
	res := prober.Probe(ctx, udid, "com.apple.Preferences")

	t.Logf("[probe] bundle=com.apple.Preferences outcome=%s detail=%q", res.Outcome.String(), res.Detail)

	if res.BundleID != "com.apple.Preferences" {
		t.Errorf("BundleID = %q, want com.apple.Preferences", res.BundleID)
	}
	if res.At.IsZero() {
		t.Errorf("At is zero")
	}
	// RESEARCH.md §3 says the daemon's policy is variable; assert only that
	// the probe terminated with one of the four valid enum values.
	switch res.Outcome {
	case apps.ProbeVended, apps.ProbeRefused, apps.ProbeError, apps.ProbeUnknown:
		// OK
	default:
		t.Errorf("unexpected outcome enum value: %d", res.Outcome)
	}
}

func TestSandbox_probe_userAppHonoursDaemonPolicy(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID unset; skipping device integration test")
	}
	bundleID := os.Getenv("IOS_TIDY_TEST_USER_BUNDLE_ID")
	if bundleID == "" {
		t.Skip("IOS_TIDY_TEST_USER_BUNDLE_ID unset; skipping App Store probe test")
	}

	prober := apps.NewProber(NewSandbox())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := prober.Probe(ctx, udid, bundleID)
	t.Logf("[probe] bundle=%s outcome=%s detail=%q", bundleID, res.Outcome.String(), res.Detail)

	if res.BundleID != bundleID {
		t.Errorf("BundleID = %q, want %q", res.BundleID, bundleID)
	}
	if res.At.IsZero() {
		t.Errorf("At is zero")
	}
	switch res.Outcome {
	case apps.ProbeVended, apps.ProbeRefused, apps.ProbeError, apps.ProbeUnknown:
		// OK
	default:
		t.Errorf("unexpected outcome enum value: %d", res.Outcome)
	}
}
