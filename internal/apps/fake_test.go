package apps

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFakeLister_UserApps_returnsCannedApps(t *testing.T) {
	want := []App{
		{BundleID: "com.example.a", Name: "A", DynamicBytes: 10, StaticBytes: 20},
		{BundleID: "com.example.b", Name: "B", DynamicBytes: 30, StaticBytes: 40},
	}
	f := &FakeLister{Apps: want}

	got, err := f.UserApps(context.Background(), "udid-A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len(UserApps) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UserApps[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestFakeLister_UserApps_returnsCannedError(t *testing.T) {
	wantErr := errors.New("transport down")
	f := &FakeLister{Err: wantErr}

	_, err := f.UserApps(context.Background(), "udid-A")
	if !errors.Is(err, wantErr) {
		t.Fatalf("UserApps err = %v, want %v", err, wantErr)
	}
}

func TestFakeLister_UserApps_recordsCalls(t *testing.T) {
	f := &FakeLister{}
	_, _ = f.UserApps(context.Background(), "udid-A")
	_, _ = f.UserApps(context.Background(), "udid-B")

	if got, want := f.Calls, []string{"udid-A", "udid-B"}; !slices.Equal(got, want) {
		t.Fatalf("Calls = %v, want %v", got, want)
	}
}

func TestFakeUninstaller_Uninstall_recordsAndReturnsErr(t *testing.T) {
	wantErr := errors.New("nope")
	f := &FakeUninstaller{Err: wantErr}

	err := f.Uninstall(context.Background(), "udid-A", "com.example.a")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Uninstall err = %v, want %v", err, wantErr)
	}
	if len(f.Calls) != 1 || f.Calls[0].UDID != "udid-A" || f.Calls[0].BundleID != "com.example.a" {
		t.Fatalf("Calls = %+v, want one call {udid-A, com.example.a}", f.Calls)
	}
}

func TestFakeUninstaller_Uninstall_succeedsByDefault(t *testing.T) {
	f := &FakeUninstaller{}
	if err := f.Uninstall(context.Background(), "u", "b"); err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
}
