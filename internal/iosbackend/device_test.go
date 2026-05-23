package iosbackend

import (
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/device"
)

// These are compile-time interface assertions. They cannot drift —
// if NewDeviceLister ever stops satisfying device.Lister the test
// file will fail to compile, surfacing the regression at `go test`
// time rather than at first user run. We keep them in the default
// (non-//go:build device) suite because they have no runtime go-ios
// calls: the constructors return zero-value adapter structs.
func TestNewDeviceLister_returnsListerInterface(t *testing.T) {
	var _ device.Lister = NewDeviceLister()
}

func TestNewTrustChecker_returnsTrustCheckerInterface(t *testing.T) {
	var _ device.TrustChecker = NewTrustChecker()
}
