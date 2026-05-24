package crashlogs

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// Entry is one crash report on the device.
//
// ModTime may be the zero value: as of go-ios v1.0.213 the AFC client wrapper
// does not surface st_mtime from the device. Consumers should treat
// ModTime.IsZero() as "unknown" and render accordingly.
type Entry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
}

// Failure describes one entry that could not be pulled or removed.
type Failure struct {
	Path string `json:"path"`
	Err  error  `json:"-"`
	// ErrMsg mirrors Err.Error() for JSON output (errors don't marshal).
	ErrMsg string `json:"error,omitempty"`
}

// PullResult is the outcome of a Client.Pull call.
type PullResult struct {
	Pulled   int       `json:"pulled"`
	Bytes    int64     `json:"bytes"`
	Failures []Failure `json:"failures,omitempty"`
}

// RemoveResult is the outcome of a Client.Remove call.
type RemoveResult struct {
	Removed  int       `json:"removed"`
	Bytes    int64     `json:"bytes"`
	Failures []Failure `json:"failures,omitempty"`
}

// Client is the seam over the on-device crash-report store.
//
// List returns all entries matching pattern (filepath.Match semantics applied
// against filepath.Base of each path). Pull copies matching entries to dst,
// preserving the on-device relative path under dst. Remove deletes matching
// entries on the device.
type Client interface {
	List(ctx context.Context, udid string, pattern string) ([]Entry, error)
	Pull(ctx context.Context, udid string, pattern string, dst string) (PullResult, error)
	Remove(ctx context.Context, udid string, pattern string) (RemoveResult, error)
}

// MatchEntries returns the subset of entries whose filepath.Base matches
// pattern (filepath.Match semantics). An empty pattern is treated as "*".
// Returns filepath.ErrBadPattern on a malformed pattern.
//
// Used by the cmd-layer overwrite pre-scan; the iosbackend adapter relies on
// go-ios's server-side filtering (crashreport.ListReports / DownloadReports
// already apply filepath.Match on filepath.Base internally — verified against
// https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go).
func MatchEntries(entries []Entry, pattern string) ([]Entry, error) {
	if pattern == "" {
		pattern = "*"
	}
	// Validate the pattern once via a probe call against an empty name so
	// callers get the bad-pattern error even if entries is empty.
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		ok, err := filepath.Match(pattern, filepath.Base(e.Path))
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// DestPath joins dstRoot with the on-device source path, preserving the
// relative directory structure under dstRoot. Leading slashes on src are
// stripped so filepath.Join doesn't treat src as absolute on Unix.
//
// Used by the cmd layer's overwrite pre-scan to identify destination
// conflicts before invoking Client.Pull. The iosbackend adapter does not
// call this helper — crashreport.DownloadReports computes its own paths
// internally.
func DestPath(dstRoot, src string) string {
	rel := strings.TrimLeft(src, "/")
	return filepath.Join(dstRoot, filepath.FromSlash(rel))
}
