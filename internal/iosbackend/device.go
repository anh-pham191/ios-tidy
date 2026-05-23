package iosbackend

import (
	"context"
	"errors"

	"github.com/anh-pham191/ios-tidy/internal/device"
)

// errNotImplemented is returned by the stubs in this file until Task 8
// wires the real go-ios calls. main.go can still construct backends
// without panic; behaviour is validated by //go:build device integration
// tests, not unit tests.
var errNotImplemented = errors.New("iosbackend: not yet implemented")

// NewDeviceLister returns a device.Lister. Task 8 replaces this stub
// with a real go-ios-backed implementation; until then it returns
// errNotImplemented so any accidental wiring is loud.
func NewDeviceLister() device.Lister { return &deviceLister{} }

// NewTrustChecker returns a device.TrustChecker. Task 8 replaces the
// stub with the real go-ios-backed implementation.
func NewTrustChecker() device.TrustChecker { return &trustChecker{} }

type deviceLister struct{}

func (l *deviceLister) List(_ context.Context) ([]device.Device, error) {
	return nil, errNotImplemented
}

type trustChecker struct{}

func (t *trustChecker) Trusted(_ context.Context, _ string) (bool, error) {
	return false, errNotImplemented
}
