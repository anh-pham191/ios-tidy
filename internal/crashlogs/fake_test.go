package crashlogs

import (
	"context"
	"errors"
	"testing"
)

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
