package main

import (
	"context"
	"os/signal"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestMainRootContextCancelsOnSignal pins the LIBRARY behaviour that main.go
// relies on: signal.NotifyContext returns a ctx whose Done channel fires when
// the registered signal is delivered. main.go is essentially untestable at
// the process level without an external harness, so this test guards the
// wrapper construction by exercising the same pattern with SIGUSR1 (which is
// guaranteed to be deliverable on Unix; Windows skips).
func TestMainRootContextCancelsOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGUSR1 not available on Windows")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGUSR1)
	defer stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGUSR1); err != nil {
		t.Skipf("SIGUSR1 not deliverable on this platform: %v", err)
	}
	select {
	case <-ctx.Done():
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("ctx did not cancel after SIGUSR1")
	}
}
