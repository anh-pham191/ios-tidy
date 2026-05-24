package apps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewFileProbeStore_returnsNonNil(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(filepath.Join(dir, "probes"))
	if s == nil {
		t.Fatal("NewFileProbeStore returned nil")
	}
}

func TestFileProbeStore_Save_writesSortedCamelCaseJSON(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(dir)

	at := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	results := []ProbeResult{
		{BundleID: "com.zeta.app", Outcome: ProbeVended, Detail: "", At: at},
		{BundleID: "com.alpha.app", Outcome: ProbeRefused, Detail: "denied", At: at},
	}
	if err := s.Save("UDID123", results); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "UDID123.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got struct {
		UDID    string `json:"udid"`
		SavedAt string `json:"savedAt"`
		Results []struct {
			BundleID string `json:"bundleID"`
			Outcome  string `json:"outcome"`
			Detail   string `json:"detail"`
			At       string `json:"at"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("Unmarshal: %v\nraw=%s", err, raw)
	}
	if got.UDID != "UDID123" {
		t.Errorf("udid = %q, want %q", got.UDID, "UDID123")
	}
	if got.SavedAt == "" {
		t.Errorf("savedAt is empty; want an RFC3339 timestamp")
	}
	if len(got.Results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(got.Results))
	}
	if got.Results[0].BundleID != "com.alpha.app" || got.Results[1].BundleID != "com.zeta.app" {
		t.Errorf("results not sorted: got [%s, %s]", got.Results[0].BundleID, got.Results[1].BundleID)
	}
	if got.Results[0].Outcome != "refused" || got.Results[1].Outcome != "vended" {
		t.Errorf("outcome rendering wrong: got [%s, %s]", got.Results[0].Outcome, got.Results[1].Outcome)
	}
}

func TestFileProbeStore_Save_createsDirIfMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "probes")
	s := NewFileProbeStore(dir)
	if err := s.Save("UDID", nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "UDID.json")); err != nil {
		t.Fatalf("Stat: %v", err)
	}
}

func TestFileProbeStore_Save_writesWithRestrictivePermissions(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(filepath.Join(dir, "probes"))
	if err := s.Save("UDID", []ProbeResult{{BundleID: "a", Outcome: ProbeVended}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Directory mode: at most 0o700 on the bits we care about (owner-only).
	dInfo, err := os.Stat(filepath.Join(dir, "probes"))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if dInfo.Mode().Perm()&0o077 != 0 {
		t.Errorf("dir perm = %o, want no group/other bits (0o700)", dInfo.Mode().Perm())
	}
	// File mode: same — owner read/write only (0o600).
	fInfo, err := os.Stat(filepath.Join(dir, "probes", "UDID.json"))
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if fInfo.Mode().Perm()&0o077 != 0 {
		t.Errorf("file perm = %o, want no group/other bits (0o600)", fInfo.Mode().Perm())
	}
}

func TestFileProbeStore_Save_atomicRename_noTmpFileLeft(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(dir)
	if err := s.Save("UDID", []ProbeResult{{BundleID: "a", Outcome: ProbeVended}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestFileProbeStore_Load_roundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(dir)
	at := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	want := []ProbeResult{
		{BundleID: "com.alpha.app", Outcome: ProbeVended, Detail: "", At: at},
		{BundleID: "com.zeta.app", Outcome: ProbeRefused, Detail: "denied", At: at},
	}
	if err := s.Save("UDID", want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Load("UDID")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].BundleID != want[i].BundleID {
			t.Errorf("[%d].BundleID = %q, want %q", i, got[i].BundleID, want[i].BundleID)
		}
		if got[i].Outcome != want[i].Outcome {
			t.Errorf("[%d].Outcome = %v, want %v", i, got[i].Outcome, want[i].Outcome)
		}
		if got[i].Detail != want[i].Detail {
			t.Errorf("[%d].Detail = %q, want %q", i, got[i].Detail, want[i].Detail)
		}
		if !got[i].At.Equal(want[i].At) {
			t.Errorf("[%d].At = %v, want %v", i, got[i].At, want[i].At)
		}
	}
}

func TestFileProbeStore_Load_missingFileReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(dir)
	got, err := s.Load("UDID_NEVER_PROBED")
	if err != nil {
		t.Fatalf("Load: err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("Load: got = %v, want nil", got)
	}
}

func TestFileProbeStore_Load_malformedFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "UDID.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFileProbeStore(dir)
	_, err := s.Load("UDID")
	if err == nil {
		t.Fatal("Load: err = nil, want a parse error")
	}
}

func TestFileProbeStore_Save_concurrentSavesProduceValidFile(t *testing.T) {
	dir := t.TempDir()
	s := NewFileProbeStore(dir)

	at := time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)
	mk := func(seed string) []ProbeResult {
		return []ProbeResult{
			{BundleID: "com.alpha." + seed, Outcome: ProbeVended, At: at},
			{BundleID: "com.beta." + seed, Outcome: ProbeRefused, Detail: "denied", At: at},
		}
	}

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		seed := fmt.Sprintf("%d", i)
		go func() {
			defer wg.Done()
			if err := s.Save("UDID_CONCURRENT", mk(seed)); err != nil {
				t.Errorf("Save: %v", err)
			}
		}()
	}
	wg.Wait()

	// File must exist, parse cleanly, and contain exactly one of the seeds.
	got, err := s.Load("UDID_CONCURRENT")
	if err != nil {
		t.Fatalf("Load after concurrent saves: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2 (one save's worth)", len(got))
	}

	// No stale .tmp left over (each rename consumes its own .tmp).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("leftover tmp: %s", e.Name())
		}
	}
}
