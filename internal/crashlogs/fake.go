package crashlogs

import "context"

// FakeClient is a hand-written fake Client for cross-package tests.
//
// Set the *Entries / *Result / *Err fields to canned values; the recording
// slices capture each call's arguments in order.
//
// For tests that need to simulate a *sequence* of Pull outcomes (e.g.
// partial-failure scenarios where the first call succeeds and the second
// returns a per-entry failure), populate PullResults instead of PullResult.
// When PullResults is non-empty, each Pull call pops the head; when it
// empties, the fake falls back to PullResult (zero value if unset).
type FakeClient struct {
	ListEntries []Entry
	ListErr     error
	ListCalls   []ListCall
	// ListFn, when non-nil, supersedes ListEntries/ListErr and is invoked per
	// call. It receives the same ctx/udid/pattern the production Client would,
	// so tests that need to simulate dynamic behaviour (e.g. different
	// outcomes per call, inspect ctx cancellation) can do so without touching
	// the canned-response fields. The recording slice ListCalls is appended to
	// on every call regardless of which path is taken.
	ListFn func(ctx context.Context, udid, pattern string) ([]Entry, error)

	PullResult  PullResult
	PullResults []PullResult // optional queue; takes precedence over PullResult
	PullErr     error
	PullCalls   []PullCall

	RemoveResult RemoveResult
	RemoveErr    error
	RemoveCalls  []RemoveCall
	// RemoveFn, when non-nil, supersedes RemoveResult/RemoveErr and is invoked
	// per call. It receives the same ctx/udid/pattern the production Client
	// would, so tests that need to simulate dynamic behaviour (e.g. different
	// outcomes per call, inspect ctx cancellation) can do so without touching
	// the canned-response fields. The recording slice RemoveCalls is appended
	// to on every call regardless of which path is taken.
	RemoveFn func(ctx context.Context, udid, pattern string) (RemoveResult, error)
}

type ListCall struct {
	UDID    string
	Pattern string
}

type PullCall struct {
	UDID    string
	Pattern string
	Dst     string
}

type RemoveCall struct {
	UDID    string
	Pattern string
}

func (f *FakeClient) List(ctx context.Context, udid, pattern string) ([]Entry, error) {
	f.ListCalls = append(f.ListCalls, ListCall{UDID: udid, Pattern: pattern})
	if f.ListFn != nil {
		return f.ListFn(ctx, udid, pattern)
	}
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListEntries, nil
}

func (f *FakeClient) Pull(_ context.Context, udid, pattern, dst string) (PullResult, error) {
	f.PullCalls = append(f.PullCalls, PullCall{UDID: udid, Pattern: pattern, Dst: dst})
	if f.PullErr != nil {
		return PullResult{}, f.PullErr
	}
	if len(f.PullResults) > 0 {
		r := f.PullResults[0]
		f.PullResults = f.PullResults[1:]
		return r, nil
	}
	return f.PullResult, nil
}

func (f *FakeClient) Remove(ctx context.Context, udid, pattern string) (RemoveResult, error) {
	f.RemoveCalls = append(f.RemoveCalls, RemoveCall{UDID: udid, Pattern: pattern})
	if f.RemoveFn != nil {
		return f.RemoveFn(ctx, udid, pattern)
	}
	if f.RemoveErr != nil {
		return RemoveResult{}, f.RemoveErr
	}
	return f.RemoveResult, nil
}
