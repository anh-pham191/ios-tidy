package crashlogs

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestMatchEntries_filtersByBasenameGlob(t *testing.T) {
	entries := []Entry{
		{Path: "/IPS/Chrome-2026-05-23-1.ips", Size: 100, ModTime: time.Time{}},
		{Path: "/Chrome-2026-05-22.ips", Size: 50},
		{Path: "/Mail-2026-05-23.crash", Size: 200},
		{Path: "/sub/Mail-2026-05-22.crash", Size: 25}, // basename match
	}

	got, err := MatchEntries(entries, "Chrome-*")
	if err != nil {
		t.Fatalf("MatchEntries err = %v, want nil", err)
	}
	wantPaths := []string{"/IPS/Chrome-2026-05-23-1.ips", "/Chrome-2026-05-22.ips"}
	if len(got) != len(wantPaths) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(wantPaths), got)
	}
	for i, p := range wantPaths {
		if got[i].Path != p {
			t.Fatalf("got[%d].Path = %q, want %q", i, got[i].Path, p)
		}
	}
}

func TestMatchEntries_starMatchesAll(t *testing.T) {
	entries := []Entry{{Path: "/a"}, {Path: "/b/c"}, {Path: "/d.crash"}}
	got, err := MatchEntries(entries, "*")
	if err != nil {
		t.Fatalf("MatchEntries err = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (star must match every basename)", len(got))
	}
}

func TestMatchEntries_emptyPatternMatchesAll(t *testing.T) {
	// Empty pattern is treated as "*" so cmd defaults work without special-casing.
	entries := []Entry{{Path: "/a"}, {Path: "/b"}}
	got, err := MatchEntries(entries, "")
	if err != nil {
		t.Fatalf("MatchEntries err = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestMatchEntries_returnsErrBadPatternForInvalidPattern(t *testing.T) {
	_, err := MatchEntries([]Entry{{Path: "/a"}}, "[invalid")
	if !errors.Is(err, filepath.ErrBadPattern) {
		t.Fatalf("err = %v, want filepath.ErrBadPattern", err)
	}
}

func TestDestPath_preservesRelativeStructure(t *testing.T) {
	cases := []struct {
		src, dstRoot, want string
	}{
		{src: "/Chrome.ips", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Chrome.ips")},
		{src: "/Retired/2026-05-22/Mail.crash", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Retired", "2026-05-22", "Mail.crash")},
		{src: "Chrome.ips", dstRoot: "/tmp/out", want: filepath.Join("/tmp/out", "Chrome.ips")},
	}
	for _, tc := range cases {
		got := DestPath(tc.dstRoot, tc.src)
		if got != tc.want {
			t.Fatalf("DestPath(%q, %q) = %q, want %q", tc.dstRoot, tc.src, got, tc.want)
		}
	}
}

func TestFakeClient_List_returnsCannedEntriesAndRecordsCall(t *testing.T) {
	want := []Entry{
		{Path: "/A.ips", Size: 100, ModTime: time.Date(2026, 5, 23, 10, 0, 0, 0, time.UTC)},
		{Path: "/B.crash", Size: 50},
	}
	fc := &FakeClient{ListEntries: want}

	got, err := fc.List(context.Background(), "UDID1", "*")
	if err != nil {
		t.Fatalf("List err = %v, want nil", err)
	}
	if len(got) != 2 || got[0].Path != "/A.ips" || got[1].Path != "/B.crash" {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	if len(fc.ListCalls) != 1 || fc.ListCalls[0].UDID != "UDID1" || fc.ListCalls[0].Pattern != "*" {
		t.Fatalf("ListCalls = %+v, want one {UDID1, *}", fc.ListCalls)
	}
}

func TestFakeClient_List_returnsCannedError(t *testing.T) {
	want := errors.New("transport boom")
	fc := &FakeClient{ListErr: want}

	_, err := fc.List(context.Background(), "UDID1", "*")
	if !errors.Is(err, want) {
		t.Fatalf("List err = %v, want %v", err, want)
	}
}

func TestFakeClient_Pull_returnsCannedResultAndRecordsCall(t *testing.T) {
	want := PullResult{Pulled: 3, Bytes: 1234, Failures: []Failure{{Path: "/X", ErrMsg: "x"}}}
	fc := &FakeClient{PullResult: want}

	got, err := fc.Pull(context.Background(), "UDID1", "Chrome-*", "/tmp/dst")
	if err != nil {
		t.Fatalf("Pull err = %v, want nil", err)
	}
	if got.Pulled != 3 || got.Bytes != 1234 || len(got.Failures) != 1 {
		t.Fatalf("Pull = %+v, want %+v", got, want)
	}
	if len(fc.PullCalls) != 1 {
		t.Fatalf("PullCalls = %d, want 1", len(fc.PullCalls))
	}
	if c := fc.PullCalls[0]; c.UDID != "UDID1" || c.Pattern != "Chrome-*" || c.Dst != "/tmp/dst" {
		t.Fatalf("PullCalls[0] = %+v", c)
	}
}

func TestFakeClient_Remove_returnsCannedResultAndRecordsCall(t *testing.T) {
	want := RemoveResult{Removed: 2, Bytes: 500}
	fc := &FakeClient{RemoveResult: want}

	got, err := fc.Remove(context.Background(), "UDID1", "*")
	if err != nil || got.Removed != 2 || got.Bytes != 500 {
		t.Fatalf("Remove = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	if len(fc.RemoveCalls) != 1 || fc.RemoveCalls[0].UDID != "UDID1" {
		t.Fatalf("RemoveCalls = %+v", fc.RemoveCalls)
	}
}
