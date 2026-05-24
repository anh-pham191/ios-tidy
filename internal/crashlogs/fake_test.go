package crashlogs

import (
	"context"
	"errors"
	"testing"
)

func TestFakeClient_ListFnTakesPrecedenceOverCannedFields(t *testing.T) {
	want := []Entry{{Path: "/dyn.ips", Size: 7}}
	f := &FakeClient{
		ListEntries: []Entry{{Path: "/canned.ips", Size: 1}}, // should be ignored
		ListErr:     errors.New("canned error — should be ignored"),
		ListFn: func(ctx context.Context, udid, pattern string) ([]Entry, error) {
			if udid != "U1" || pattern != "Chrome-*" {
				t.Fatalf("ListFn args = (%q, %q), want (U1, Chrome-*)", udid, pattern)
			}
			return want, nil
		},
	}
	got, err := f.List(context.Background(), "U1", "Chrome-*")
	if err != nil {
		t.Fatalf("List returned err = %v, want nil (ListFn path)", err)
	}
	if len(got) != 1 || got[0].Path != "/dyn.ips" {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	if len(f.ListCalls) != 1 || f.ListCalls[0].UDID != "U1" || f.ListCalls[0].Pattern != "Chrome-*" {
		t.Fatalf("ListCalls = %+v, want one call recorded", f.ListCalls)
	}
}

func TestFakeClient_ListFallsBackToCannedFieldsWhenFnUnset(t *testing.T) {
	want := []Entry{{Path: "/canned.ips", Size: 1}}
	f := &FakeClient{ListEntries: want}
	got, err := f.List(context.Background(), "U1", "*")
	if err != nil {
		t.Fatalf("List err = %v, want nil", err)
	}
	if len(got) != 1 || got[0].Path != "/canned.ips" {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
}

func TestFakeClient_RemoveRecordsCallArgsAndFnTakesPrecedence(t *testing.T) {
	f := &FakeClient{
		RemoveFn: func(ctx context.Context, udid, pattern string) (RemoveResult, error) {
			return RemoveResult{Removed: 2, Bytes: 4096}, nil
		},
	}
	res, err := f.Remove(context.Background(), "ABC123", "*.ips")
	if err != nil {
		t.Fatalf("Remove returned err: %v", err)
	}
	if res.Removed != 2 || res.Bytes != 4096 {
		t.Fatalf("result = %+v, want {Removed:2, Bytes:4096}", res)
	}
	if len(f.RemoveCalls) != 1 {
		t.Fatalf("RemoveCalls len = %d, want 1", len(f.RemoveCalls))
	}
	if f.RemoveCalls[0].UDID != "ABC123" || f.RemoveCalls[0].Pattern != "*.ips" {
		t.Fatalf("RemoveCalls[0] = %+v, want {UDID:ABC123 Pattern:*.ips}", f.RemoveCalls[0])
	}
	// Error pass-through path via RemoveFn.
	f.RemoveFn = func(ctx context.Context, udid, pattern string) (RemoveResult, error) {
		return RemoveResult{}, errors.New("boom")
	}
	if _, err := f.Remove(context.Background(), "X", "Y"); err == nil {
		t.Fatalf("expected error on second Remove, got nil")
	}
	if len(f.RemoveCalls) != 2 {
		t.Fatalf("RemoveCalls len after second call = %d, want 2", len(f.RemoveCalls))
	}
}
