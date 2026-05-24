// internal/sandbox/fake_test.go
package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeSandbox_Open_returnsCannedFS(t *testing.T) {
	want := &FakeFS{}
	fs := NewFakeSandbox()
	fs.SetResponse("com.foo.bar", FakeResponse{FS: want})

	got, err := fs.Open(context.Background(), "UDID", "com.foo.bar")
	if err != nil {
		t.Fatalf("Open: unexpected err %v", err)
	}
	if got != want {
		t.Fatalf("Open: got %p, want %p", got, want)
	}
}

func TestFakeSandbox_Open_returnsCannedError(t *testing.T) {
	wantErr := errors.New("VendContainer failed: denied")
	s := NewFakeSandbox()
	s.SetResponse("com.foo.bar", FakeResponse{Err: wantErr})

	_, err := s.Open(context.Background(), "UDID", "com.foo.bar")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Open: err = %v, want wraps %v", err, wantErr)
	}
}

func TestFakeSandbox_Open_unknownBundleIsZeroValue(t *testing.T) {
	s := NewFakeSandbox()
	got, err := s.Open(context.Background(), "UDID", "com.unset.app")
	if err != nil {
		t.Fatalf("Open: unexpected err %v", err)
	}
	if got != nil {
		t.Fatalf("Open: want nil FS for unset response, got %v", got)
	}
}

func TestFakeSandbox_Open_hangsUntilContextCancelled(t *testing.T) {
	s := NewFakeSandbox()
	s.SetResponse("com.hang.app", FakeResponse{Hang: true})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.Open(ctx, "UDID", "com.hang.app")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open: err = %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
		t.Fatalf("Open: returned too quickly (%v); should have waited for ctx", elapsed)
	}
}

func TestFakeFS_Close_incrementsCounter(t *testing.T) {
	f := &FakeFS{}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.CloseCalls != 2 {
		t.Fatalf("CloseCalls = %d, want 2", f.CloseCalls)
	}
}
