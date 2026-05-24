// internal/sandbox/sandbox_test.go
package sandbox

import (
	"context"
	"testing"
)

// TestInterfaces_compile is a compile-time-only assertion that the seam
// interfaces exist with the expected method shape. If any method signature
// drifts from SHARED_CONTEXT.md §3, this file fails to compile.
func TestInterfaces_compile(t *testing.T) {
	var _ Sandbox = (*nopSandbox)(nil)
	var _ FS = (*nopFS)(nil)
}

type nopSandbox struct{}

func (nopSandbox) Open(ctx context.Context, udid, bundleID string) (FS, error) {
	return nil, nil
}

type nopFS struct{}

func (nopFS) List(ctx context.Context, path string) ([]FileInfo, error) { return nil, nil }
func (nopFS) Stat(ctx context.Context, path string) (FileInfo, error)   { return FileInfo{}, nil }
func (nopFS) Walk(ctx context.Context, root string, fn WalkFunc) error  { return nil }
func (nopFS) Remove(ctx context.Context, path string) error             { return nil }
func (nopFS) RemoveAll(ctx context.Context, path string) error          { return nil }
func (nopFS) Close() error                                              { return nil }
