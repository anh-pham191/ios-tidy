package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestFakeClient_DeviceInfo_returnsCannedValue(t *testing.T) {
	want := DeviceInfo{Model: "iPhone15,3", TotalBytes: 500_000_000_000, FreeBytes: 120_000_000_000, BlockSize: 4096}
	f := &FakeClient{Info: want}

	got, err := f.DeviceInfo(context.Background(), "udid-A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("DeviceInfo() = %+v, want %+v", got, want)
	}
}

func TestFakeClient_DeviceInfo_returnsCannedError(t *testing.T) {
	wantErr := errors.New("boom")
	f := &FakeClient{Err: wantErr}

	_, err := f.DeviceInfo(context.Background(), "udid-A")
	if !errors.Is(err, wantErr) {
		t.Fatalf("DeviceInfo() err = %v, want %v", err, wantErr)
	}
}

func TestFakeClient_DeviceInfo_recordsCalls(t *testing.T) {
	f := &FakeClient{}

	_, _ = f.DeviceInfo(context.Background(), "udid-A")
	_, _ = f.DeviceInfo(context.Background(), "udid-B")

	if got, want := f.Calls, []string{"udid-A", "udid-B"}; !slices.Equal(got, want) {
		t.Fatalf("Calls = %v, want %v", got, want)
	}
}
