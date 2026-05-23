//go:build device
// +build device

package iosbackend

import (
	"context"
	"os"
	"testing"
)

// TestDeviceLister_listsConnectedDevice verifies the real go-ios
// adapter against a physically-connected, already-paired iPhone.
// Run with:
//
//	IOS_TIDY_TEST_UDID=<udid> go test -tags=device -count=1 \
//	    ./internal/iosbackend/... -v
//
// The Makefile's `test-device` target wraps this with the env-var
// guard so it cannot run unintentionally.
func TestDeviceLister_listsConnectedDevice(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID not set; skipping device test (this is the intended behaviour for CI without a phone attached)")
	}

	lister := NewDeviceLister()
	devices, err := lister.List(context.Background())
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	var found bool
	for _, d := range devices {
		if d.UDID == udid {
			found = true
			if d.Name == "" {
				t.Errorf("device %s has empty Name; either untrusted or GetValues failed", udid)
			}
			if d.Model == "" {
				t.Errorf("device %s has empty Model", udid)
			}
			if d.IOSVersion == "" {
				t.Errorf("device %s has empty IOSVersion", udid)
			}
			break
		}
	}
	if !found {
		t.Fatalf("UDID %q not found among %d connected devices: %+v", udid, len(devices), devices)
	}
}

// TestTrustChecker_reportsTrustedForPairedDevice assumes the user has
// already accepted the Trust prompt on the test phone. If the test
// fails with "got false", check that the device shows the Trust state
// in Finder before debugging the code.
func TestTrustChecker_reportsTrustedForPairedDevice(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("IOS_TIDY_TEST_UDID not set; skipping device test")
	}

	checker := NewTrustChecker()
	trusted, err := checker.Trusted(context.Background(), udid)
	if err != nil {
		t.Fatalf("Trusted returned transport error: %v", err)
	}
	if !trusted {
		t.Fatalf("Trusted(%q) = false; tap 'Trust' on the device and rerun. macOS 26 (Tahoe) users: see RESEARCH.md §6 — go-ios issue #710 may block pair-record reads.", udid)
	}
}
