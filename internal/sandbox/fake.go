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

// FakeFS is a test double for FS. CloseCalls counts how many times Close
// has been called — the probe success path MUST close exactly once.
type FakeFS struct {
	mu         sync.Mutex
	CloseCalls int
}

func (f *FakeFS) List(ctx context.Context, path string) ([]FileInfo, error) {
	return nil, nil
}
func (f *FakeFS) Stat(ctx context.Context, path string) (FileInfo, error) {
	return FileInfo{}, nil
}
func (f *FakeFS) Walk(ctx context.Context, root string, fn WalkFunc) error { return nil }
func (f *FakeFS) Remove(ctx context.Context, path string) error            { return nil }
func (f *FakeFS) RemoveAll(ctx context.Context, path string) error         { return nil }

func (f *FakeFS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CloseCalls++
	return nil
}
