// internal/iosbackend/sandbox.go
//
// Production sandbox.Sandbox adapter. M5 Task 12 only needs a constructor
// the main dispatcher can call when wiring `apps probe`; the real go-ios
// AFC / house_arrest integration lands in M5 Task 13. Until then Open
// returns a sentinel error so users running the CLI without the real
// backend get a clear "not implemented" message rather than a panic.
package iosbackend

import (
	"context"
	"errors"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

// NewSandbox returns the production sandbox.Sandbox. Task 13 will swap the
// stub for a real go-ios-backed implementation; the constructor signature
// is locked in now so cmd/ios-tidy can already depend on it.
func NewSandbox() sandbox.Sandbox { return &sandboxStub{} }

type sandboxStub struct{}

func (s *sandboxStub) Open(_ context.Context, _, _ string) (sandbox.FS, error) {
	return nil, errors.New("sandbox: not yet implemented (M5 Task 13)")
}
