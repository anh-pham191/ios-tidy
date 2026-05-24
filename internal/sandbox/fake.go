package sandbox

import (
	"context"
	"sync"
)

// FakeResponse is the canned reply for a single bundle ID.
// Exactly one of {FS, Err, Hang} is meaningful at a time.
//   - FS != nil   → Open returns (FS, nil)
//   - Err != nil  → Open returns (nil, Err)
//   - Hang == true → Open blocks until ctx is done, then returns ctx.Err()
type FakeResponse struct {
	FS   FS
	Err  error
	Hang bool
}

// FakeSandbox is a test double for Sandbox. Construct via NewFakeSandbox.
type FakeSandbox struct {
	mu        sync.Mutex
	responses map[string]FakeResponse
	openCalls []string // bundle IDs, in order
}

func NewFakeSandbox() *FakeSandbox {
	return &FakeSandbox{responses: map[string]FakeResponse{}}
}

func (s *FakeSandbox) SetResponse(bundleID string, r FakeResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses[bundleID] = r
}

func (s *FakeSandbox) OpenCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.openCalls))
	copy(out, s.openCalls)
	return out
}

func (s *FakeSandbox) Open(ctx context.Context, udid, bundleID string) (FS, error) {
	s.mu.Lock()
	r := s.responses[bundleID]
	s.openCalls = append(s.openCalls, bundleID)
	s.mu.Unlock()

	if r.Hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return r.FS, nil
}

// FakeFS is a test double for FS. The recording slices let M6's destructive
// matrix tests assert "Remove was/wasn't called" without reaching into this
// package later. WalkResults seeds Walk's iteration; ListResults seeds List.
//
// Zero-value FakeFS is usable: empty result maps, nil errors, all calls
// succeed and return zero values.
//
// Canned-error semantics: when a *Err field is non-nil, the corresponding
// method still records the call (so test assertions can distinguish "not
// called" from "called and failed"), then returns the canned error.
type FakeFS struct {
	mu             sync.Mutex
	CloseCalls     int
	ListCalls      []string
	StatCalls      []string
	WalkCalls      []string
	RemoveCalls    []string
	RemoveAllCalls []string
	ListResults    map[string][]FileInfo
	StatResults    map[string]FileInfo
	WalkResults    map[string][]FileInfo
	RemoveErr      error
	RemoveAllErr   error
	StatErr        error
	ListErr        error
	WalkErr        error
	CloseErr       error
	closed         bool
}

func (f *FakeFS) List(_ context.Context, path string) ([]FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ListCalls = append(f.ListCalls, path)
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return f.ListResults[path], nil
}

func (f *FakeFS) Stat(_ context.Context, path string) (FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StatCalls = append(f.StatCalls, path)
	if f.StatErr != nil {
		return FileInfo{}, f.StatErr
	}
	return f.StatResults[path], nil
}

func (f *FakeFS) Walk(_ context.Context, root string, fn WalkFunc) error {
	f.mu.Lock()
	entries := f.WalkResults[root]
	f.WalkCalls = append(f.WalkCalls, root)
	walkErr := f.WalkErr
	f.mu.Unlock()
	if walkErr != nil {
		return walkErr
	}
	for _, e := range entries {
		if err := fn(e, nil); err != nil {
			return err
		}
	}
	return nil
}

func (f *FakeFS) Remove(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveCalls = append(f.RemoveCalls, path)
	return f.RemoveErr
}

func (f *FakeFS) RemoveAll(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveAllCalls = append(f.RemoveAllCalls, path)
	return f.RemoveAllErr
}

func (f *FakeFS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CloseCalls++
	f.closed = true
	return f.CloseErr
}

// Closed reports whether Close has been called at least once. It returns
// true even when the call returned a canned CloseErr — the spy tracks intent.
func (f *FakeFS) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
