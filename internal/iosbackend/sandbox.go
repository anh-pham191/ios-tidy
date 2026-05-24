// internal/iosbackend/sandbox.go
//
// sandboxClient is the go-ios adapter for sandbox.Sandbox. It is the ONLY
// file in the codebase outside this package that may import
// github.com/danielpaulus/go-ios/ios/house_arrest or .../ios/afc — the
// sandbox seam (internal/sandbox) keeps cmd/ios-tidy and the rest of the
// internal tree go-ios-free.
//
// Design:
//   - Open resolves the UDID via the same findDevice helper as crashlogs.go
//     (kept package-local in crashlogs.go), then dials
//     house_arrest.New(entry, bundleID) which returns *afc.Client per
//     RESEARCH.md §2. The dial runs in a goroutine so ctx cancellation can
//     unblock the caller; if ctx fires after a successful dial we drain the
//     result and Close the client to avoid leaking an AFC socket.
//   - The returned FS wraps *afc.Client. All FS methods delegate directly
//     to afc.Client methods — the underlying client is single-flight per
//     connection, so callers must serialise concurrent calls (documented on
//     sandbox.FS).
//   - afc.FileInfo → sandbox.FileInfo mapping: only Name/Path/Size/IsDir
//     transfer. afc.FileInfo does not surface ModTime at the pinned SHA
//     (same caveat as crashlogs.go); ModTime is left at the zero value.
//     Path is reconstructed by the caller-provided path argument so the
//     mapping helper stays pure.
package iosbackend

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"

	"github.com/danielpaulus/go-ios/ios/afc"
	"github.com/danielpaulus/go-ios/ios/house_arrest"
)

// NewSandbox returns the production sandbox.Sandbox backed by go-ios's
// house_arrest service.
func NewSandbox() sandbox.Sandbox { return &sandboxClient{} }

type sandboxClient struct{}

// Open dials house_arrest for (udid, bundleID). The dial runs on a
// goroutine so ctx cancellation is honoured; if the dial completes after
// ctx.Done we close the resulting client to avoid leaking the AFC socket.
func (s *sandboxClient) Open(ctx context.Context, udid, bundleID string) (sandbox.FS, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dev, err := findDevice(udid)
	if err != nil {
		return nil, fmt.Errorf("sandbox open: %w", err)
	}

	type dialResult struct {
		c   *afc.Client
		err error
	}
	done := make(chan dialResult, 1)
	go func() {
		c, err := house_arrest.New(dev, bundleID)
		done <- dialResult{c: c, err: err}
	}()

	select {
	case <-ctx.Done():
		// Drain & close if the dial succeeds after we've given up.
		go func() {
			r := <-done
			if r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return nil, r.err
		}
		if r.c == nil {
			return nil, errors.New("house_arrest.New returned nil client without error")
		}
		return &afcFS{c: r.c}, nil
	}
}

// afcFS adapts *afc.Client to sandbox.FS by delegating each method. The
// underlying AFC transport is single-flight per connection (documented on
// sandbox.FS), so callers must not invoke methods concurrently on the same
// FS.
type afcFS struct {
	c *afc.Client
}

func (f *afcFS) Close() error { return f.c.Close() }

// List returns the immediate children of path. The afc.Client.List call
// returns basenames; we synthesise sandbox.FileInfo entries via Stat on each
// child so the caller gets Size/IsDir without a second round trip on their
// end. ModTime stays zero — afc.FileInfo does not surface it at the pinned
// SHA (RESEARCH.md §2 / same caveat as crashlogs.go).
func (f *afcFS) List(ctx context.Context, dir string) ([]sandbox.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := f.c.List(dir)
	if err != nil {
		return nil, err
	}
	out := make([]sandbox.FileInfo, 0, len(names))
	for _, name := range names {
		if name == "." || name == ".." {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		child := path.Join(dir, name)
		info, err := f.c.Stat(child)
		if err != nil {
			// Best-effort: skip entries that disappeared between List and
			// Stat. Surfacing them as errors would make a single-file race
			// abort the whole listing.
			continue
		}
		out = append(out, toSandboxFileInfo(info, child))
	}
	return out, nil
}

func (f *afcFS) Stat(ctx context.Context, p string) (sandbox.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return sandbox.FileInfo{}, err
	}
	info, err := f.c.Stat(p)
	if err != nil {
		return sandbox.FileInfo{}, err
	}
	return toSandboxFileInfo(info, p), nil
}

// Walk delegates to afc.Client.WalkDir. The afc walk func signature is
// (path, info, err) → error; we adapt it to sandbox.WalkFunc which receives
// a fully-populated sandbox.FileInfo (with Path) plus the error.
func (f *afcFS) Walk(ctx context.Context, root string, fn sandbox.WalkFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.c.WalkDir(root, func(p string, info afc.FileInfo, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fn(toSandboxFileInfo(info, p), err)
	})
}

func (f *afcFS) Remove(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.c.Remove(p)
}

func (f *afcFS) RemoveAll(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.c.RemoveAll(p)
}

// toSandboxFileInfo maps afc.FileInfo → sandbox.FileInfo. Path is taken
// from the caller because afc.FileInfo only carries the basename. ModTime
// is left zero (see file header).
func toSandboxFileInfo(in afc.FileInfo, fullPath string) sandbox.FileInfo {
	name := in.Name
	if name == "" {
		// Fallback: derive basename from fullPath. afc.Stat sets Name from
		// the path it was given, so this only fires for unusual callers.
		if idx := strings.LastIndex(fullPath, "/"); idx >= 0 && idx < len(fullPath)-1 {
			name = fullPath[idx+1:]
		} else {
			name = fullPath
		}
	}
	return sandbox.FileInfo{
		Name:  name,
		Path:  fullPath,
		Size:  in.Size,
		IsDir: in.IsDir(),
	}
}
