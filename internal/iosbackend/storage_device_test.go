//go:build device
// +build device

package iosbackend

import (
	"context"
	"os"
	"testing"
)

// TestStorage_realDevice_reportsPlausibleSizes verifies the AFC adapter against
// a real device. Run with:
//   IOS_TIDY_TEST_UDID=<udid> go test -tags=device ./internal/iosbackend/...
// Skipped if IOS_TIDY_TEST_UDID is unset to prevent accidental runs against the
// wrong phone (per SHARED_CONTEXT.md §5).
func TestStorage_realDevice_reportsPlausibleSizes(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("set IOS_TIDY_TEST_UDID to run device integration tests")
	}

	sc := NewStorage()
	info, err := sc.DeviceInfo(context.Background(), udid)
	if err != nil {
		t.Fatalf("DeviceInfo(%q) err = %v", udid, err)
	}
	if info.TotalBytes == 0 {
		t.Fatalf("TotalBytes = 0, want > 0")
	}
	if info.FreeBytes > info.TotalBytes {
		t.Fatalf("FreeBytes (%d) > TotalBytes (%d) — impossible", info.FreeBytes, info.TotalBytes)
	}
	if info.Model == "" {
		t.Fatalf("Model is empty — AFC should always return it")
	}
}

// TestApps_realDevice_listsAtLeastOneUserApp verifies the installation-proxy
// adapter. Assumes the test device has at least one user-installed app — true
// for any non-pristine phone.
func TestApps_realDevice_listsAtLeastOneUserApp(t *testing.T) {
	udid := os.Getenv("IOS_TIDY_TEST_UDID")
	if udid == "" {
		t.Skip("set IOS_TIDY_TEST_UDID to run device integration tests")
	}

	lister, _ := NewApps()
	list, err := lister.UserApps(context.Background(), udid)
	if err != nil {
		t.Fatalf("UserApps(%q) err = %v", udid, err)
	}
	if len(list) == 0 {
		t.Fatalf("no user apps returned — expected at least one")
	}
	for i, a := range list {
		if a.BundleID == "" {
			t.Fatalf("apps[%d] has empty BundleID: %+v", i, a)
		}
	}
}
