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
	if !f.Closed() {
		t.Fatalf("Closed() = false after 2 Close() calls, want true")
	}
}

func TestFakeFS_Closed_falseBeforeClose(t *testing.T) {
	f := &FakeFS{}
	if f.Closed() {
		t.Fatalf("Closed() = true before any Close() call, want false")
	}
}

func TestFakeFS_Close_returnsCannedError(t *testing.T) {
	wantErr := errFake("close fail")
	f := &FakeFS{CloseErr: wantErr}
	err := f.Close()
	if err == nil || err.Error() != "close fail" {
		t.Fatalf("Close err = %v, want %v", err, wantErr)
	}
	// Closed() should still report true even when Close returned an error.
	if !f.Closed() {
		t.Fatalf("Closed() = false after Close() with canned error, want true")
	}
}

func TestFakeFS_RemoveAll_recordsTheCall(t *testing.T) {
	f := &FakeFS{}
	if err := f.RemoveAll(context.Background(), "tmp"); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if got, want := len(f.RemoveAllCalls), 1; got != want {
		t.Fatalf("RemoveAllCalls len = %d, want %d", got, want)
	}
	if f.RemoveAllCalls[0] != "tmp" {
		t.Fatalf("RemoveAllCalls[0] = %q, want %q", f.RemoveAllCalls[0], "tmp")
	}
}

func TestFakeFS_Remove_recordsTheCall(t *testing.T) {
	f := &FakeFS{}
	if err := f.Remove(context.Background(), "Documents/a.txt"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got, want := len(f.RemoveCalls), 1; got != want {
		t.Fatalf("RemoveCalls len = %d, want %d", got, want)
	}
	if f.RemoveCalls[0] != "Documents/a.txt" {
		t.Fatalf("RemoveCalls[0] = %q, want %q", f.RemoveCalls[0], "Documents/a.txt")
	}
}

func TestFakeFS_RemoveAll_returnsCannedError(t *testing.T) {
	wantErr := errFake("nope")
	f := &FakeFS{RemoveAllErr: wantErr}
	err := f.RemoveAll(context.Background(), "tmp")
	if err == nil || err.Error() != "nope" {
		t.Fatalf("RemoveAll err = %v, want %v", err, wantErr)
	}
}

func TestFakeFS_Stat_returnsCannedError(t *testing.T) {
	wantErr := errFake("stat nope")
	f := &FakeFS{StatErr: wantErr}
	_, err := f.Stat(context.Background(), "x")
	if err == nil || err.Error() != "stat nope" {
		t.Fatalf("Stat err = %v, want %v", err, wantErr)
	}
}

func TestFakeFS_List_returnsCannedError(t *testing.T) {
	wantErr := errFake("list nope")
	f := &FakeFS{ListErr: wantErr}
	_, err := f.List(context.Background(), "x")
	if err == nil || err.Error() != "list nope" {
		t.Fatalf("List err = %v, want %v", err, wantErr)
	}
}

func TestFakeFS_Walk_returnsCannedError(t *testing.T) {
	wantErr := errFake("walk nope")
	f := &FakeFS{WalkErr: wantErr}
	err := f.Walk(context.Background(), "x", func(_ FileInfo, _ error) error { return nil })
	if err == nil || err.Error() != "walk nope" {
		t.Fatalf("Walk err = %v, want %v", err, wantErr)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
