// Package sandbox is the seam interface for the per-app sandbox vended by
// the iOS mobile_house_arrest daemon. The only production implementation is
// internal/iosbackend/sandbox.go.
package sandbox

import (
	"context"
	"time"
)

// FileInfo describes a single entry inside an app's sandbox. Paths are
// absolute from the container root (e.g. "/Documents/foo.txt"). Size is in
// bytes. ModTime is in the device's reported timezone (typically UTC).
type FileInfo struct {
	Name    string
	Path    string
	Size    int64
	IsDir   bool
	ModTime time.Time
}

// WalkFunc mirrors filepath.WalkFunc semantics: returning a non-nil error
// short-circuits the walk.
type WalkFunc func(info FileInfo, err error) error

// FS is an open handle on a single app's sandbox. Callers MUST Close it.
// Methods are context-aware; the underlying AFC transport is single-flight
// per FS, so concurrent calls on the same FS are not supported.
type FS interface {
	List(ctx context.Context, path string) ([]FileInfo, error)
	Stat(ctx context.Context, path string) (FileInfo, error)
	Walk(ctx context.Context, root string, fn WalkFunc) error
	Remove(ctx context.Context, path string) error
	RemoveAll(ctx context.Context, path string) error
	Close() error
}

// Sandbox dials house_arrest for a (udid, bundleID) and returns an open FS.
// Open MUST be cheap to retry; if Open succeeds the caller takes ownership
// of the FS and must Close it.
type Sandbox interface {
	Open(ctx context.Context, udid string, bundleID string) (FS, error)
}
