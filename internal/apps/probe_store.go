package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ProbeStore persists per-UDID probe results. Save and Load are full-file
// operations; there is no incremental update path because the cache is small
// and a single device only has a few hundred apps.
type ProbeStore interface {
	Save(udid string, results []ProbeResult) error
	// Load returns (nil, nil) if no cache file exists for this UDID — that
	// is NOT an error condition; it just means "no probe has run yet".
	Load(udid string) ([]ProbeResult, error)
}

// fileProbeStore is the default ProbeStore. Files live at
// <dir>/<UDID>.json. Writes are atomic via rename.
type fileProbeStore struct {
	dir string
}

// NewFileProbeStore returns a ProbeStore that persists each UDID's probe
// cache as a JSON file under dir. The directory is created lazily on Save.
func NewFileProbeStore(dir string) ProbeStore {
	return &fileProbeStore{dir: dir}
}

// on-disk schema (camelCase, sorted, stable across runs)
type diskFile struct {
	UDID    string       `json:"udid"`
	SavedAt time.Time    `json:"savedAt"`
	Results []diskResult `json:"results"`
}

type diskResult struct {
	BundleID string    `json:"bundleID"`
	Outcome  string    `json:"outcome"`
	Detail   string    `json:"detail"`
	At       time.Time `json:"at"`
}

func (s *fileProbeStore) Save(udid string, results []ProbeResult) error {
	// 0o700 / 0o600: the probe cache contains device UDIDs (weakly sensitive
	// identifiers per code-review.md §Security). Single-user macOS already
	// makes this defensible at 0o755, but tightening costs nothing.
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("probe store: mkdir %s: %w", s.dir, err)
	}

	// Copy + sort by bundle ID for stable diffs.
	sorted := slices.Clone(results)
	slices.SortFunc(sorted, func(a, b ProbeResult) int {
		switch {
		case a.BundleID < b.BundleID:
			return -1
		case a.BundleID > b.BundleID:
			return 1
		default:
			return 0
		}
	})

	disk := diskFile{
		UDID:    udid,
		SavedAt: time.Now().UTC(),
		Results: make([]diskResult, len(sorted)),
	}
	for i, r := range sorted {
		disk.Results[i] = diskResult{
			BundleID: r.BundleID,
			Outcome:  r.Outcome.String(),
			Detail:   r.Detail,
			At:       r.At,
		}
	}

	payload, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return fmt.Errorf("probe store: marshal: %w", err)
	}

	final := filepath.Join(s.dir, udid+".json")

	// Atomic write: write to a uniquely-named tmp file then rename. rename(2)
	// is atomic on the same filesystem, so a partial write can never produce a
	// half-valid final file. The unique tmp name avoids concurrent-save
	// collisions where two goroutines would race on the same .tmp path.
	tmp, err := os.CreateTemp(s.dir, filepath.Base(final)+".*.tmp")
	if err != nil {
		return fmt.Errorf("probe store: create tmp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("probe store: write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("probe store: close tmp: %w", err)
	}
	// os.CreateTemp creates with 0o600 by default, which matches the privacy
	// target. We don't need an explicit Chmod.
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("probe store: rename: %w", err)
	}
	return nil
}

func (s *fileProbeStore) Load(udid string) ([]ProbeResult, error) {
	path := filepath.Join(s.dir, udid+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // first probe; not an error
		}
		return nil, fmt.Errorf("probe store: read %s: %w", path, err)
	}
	var disk diskFile
	if err := json.Unmarshal(raw, &disk); err != nil {
		return nil, fmt.Errorf("probe store: parse %s: %w", path, err)
	}
	out := make([]ProbeResult, len(disk.Results))
	for i, r := range disk.Results {
		out[i] = ProbeResult{
			BundleID: r.BundleID,
			Outcome:  parseOutcome(r.Outcome),
			Detail:   r.Detail,
			At:       r.At,
		}
	}
	return out, nil
}

func parseOutcome(s string) ProbeOutcome {
	switch s {
	case "vended":
		return ProbeVended
	case "refused":
		return ProbeRefused
	case "error":
		return ProbeError
	default:
		return ProbeUnknown
	}
}
