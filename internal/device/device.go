// Package device defines the abstract iPhone-listing and trust-checking
// seams used by the rest of ios-tidy. The real go-ios-backed implementation
// lives in internal/iosbackend; this package must remain free of any
// go-ios import so that consumers can be unit-tested against fakes.
package device

import "context"

// Device is the metadata ios-tidy renders for a single connected iPhone.
// All fields are empty strings for an untrusted device whose lockdown
// values we cannot read.
type Device struct {
	UDID       string
	Name       string
	Model      string
	IOSVersion string
}

// Lister enumerates currently-connected iOS devices.
type Lister interface {
	List(ctx context.Context) ([]Device, error)
}

// TrustChecker reports whether the host has been trusted by the device
// (i.e. the user has tapped "Trust" on the iPhone). It is allowed to
// return (false, nil) for "untrusted but reachable" — only transport
// failures (usbmuxd dead, socket gone, TCC blocking pair-record reads
// on macOS Tahoe) should produce a non-nil error.
type TrustChecker interface {
	Trusted(ctx context.Context, udid string) (bool, error)
}
