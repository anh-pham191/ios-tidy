package iosbackend

import (
	"errors"
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

func TestClassifyTrustError_pairingPendingIsNotTransport(t *testing.T) {
	err := errors.New("StartSession failed: ... PairingDialogResponsePending ...")
	if isTransport := classifyTrustError(err); isTransport {
		t.Errorf("PairingDialogResponsePending should be classified as trust-state, not transport")
	}
}

func TestClassifyTrustError_userDeniedIsNotTransport(t *testing.T) {
	err := errors.New("UserDeniedPairingError")
	if isTransport := classifyTrustError(err); isTransport {
		t.Errorf("UserDeniedPairingError should be classified as trust-state, not transport")
	}
}

func TestClassifyTrustError_missingPairRecordIsNotTransport(t *testing.T) {
	err := errors.New("could not retrieve PairRecord with error: no record found")
	if isTransport := classifyTrustError(err); isTransport {
		t.Errorf("missing PairRecord should be classified as trust-state, not transport (it means the user hasn't tapped Trust)")
	}
}

func TestClassifyTrustError_startSessionIsNotTransport(t *testing.T) {
	err := errors.New("StartSession failed: SessionInactive error: <nil>")
	if isTransport := classifyTrustError(err); isTransport {
		t.Errorf("StartSession failure should be classified as trust-state (untrusted), not transport")
	}
}

func TestClassifyTrustError_usbmuxFailureIsTransport(t *testing.T) {
	err := errors.New("USBMuxConnection failed with: dial unix /var/run/usbmuxd: connect: no such file")
	if isTransport := classifyTrustError(err); !isTransport {
		t.Errorf("USBMuxConnection failure should be classified as transport, not trust-state")
	}
}

func TestClassifyTrustError_lockdownConnectionIsTransport(t *testing.T) {
	err := errors.New("Lockdown connection failed with: connection reset by peer")
	if isTransport := classifyTrustError(err); !isTransport {
		t.Errorf("Lockdown connection failure should be classified as transport, not trust-state")
	}
}

func TestClassifyTrustError_nilIsNotTransport(t *testing.T) {
	if isTransport := classifyTrustError(nil); isTransport {
		t.Errorf("nil error should be classified as not-transport (the trust succeeded)")
	}
}

func TestClassifyTrustError_failedToDialIsTransport(t *testing.T) {
	err := errors.New("failed to dial: connection refused")
	if isTransport := classifyTrustError(err); !isTransport {
		t.Errorf("failed-to-dial should be classified as transport")
	}
}

// Pins the documented bias-toward-transport default for unknown error
// shapes. If this default ever changes, every "unknown" failure would
// silently classify as trust-state and the CLI would exit zero on a
// real transport failure — exactly the regression the cycle-1 review
// flagged.
func TestClassifyTrustError_unknownShapeDefaultsToTransport(t *testing.T) {
	err := errors.New("totally unrecognised gibberish")
	if isTransport := classifyTrustError(err); !isTransport {
		t.Errorf("unknown error shape should default to transport (bias toward exit non-zero)")
	}
}
