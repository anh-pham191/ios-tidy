package iosbackend

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/danielpaulus/go-ios/ios"
)

// NewDeviceLister returns a device.Lister backed by go-ios's usbmuxd
// protocol. Listing requires no trust — it only talks to the local
// usbmuxd socket — but reading per-device metadata (name, model, iOS
// version) does require a paired/trusted lockdown session, which is
// why the lockdown read is wrapped in its own error path. A trusted
// device whose metadata read fails will get a single-line warning on
// the warn writer so the user sees the symptom (otherwise the row
// would appear with blank fields and look identical to an untrusted
// device).
func NewDeviceLister() device.Lister {
	return &deviceLister{warn: os.Stderr}
}

// NewTrustChecker returns a device.TrustChecker that probes pairing
// state by attempting a lockdown session via ios.ConnectLockdownWithSession.
// Trust-state failures (PairingDialogResponsePending, UserDeniedPairingError,
// missing pair record, StartSession failure) return (false, nil).
// Transport failures (usbmuxd dead, lockdown TCP reset, dial errors,
// macOS Tahoe TCC blocking pair-record reads) return (false, err) so
// the CLI exits non-zero per SHARED_CONTEXT.md §8 M1.
func NewTrustChecker() device.TrustChecker { return &trustChecker{} }

type deviceLister struct {
	// warn receives single-line warnings when a metadata read fails for
	// what looks like a trusted device. Defaulted to os.Stderr; settable
	// for tests if we ever want to add one (not required at M1).
	warn io.Writer
}

func (l *deviceLister) List(_ context.Context) ([]device.Device, error) {
	list, err := ios.ListDevices()
	if err != nil {
		return nil, err
	}

	out := make([]device.Device, 0, len(list.DeviceList))
	for _, entry := range list.DeviceList {
		udid := entry.Properties.SerialNumber
		name, model, version, mdErr := readMetadata(entry)

		// readMetadata returns ("", "", "", err) on any lockdown failure.
		// An untrusted device legitimately can't be read, but a trusted
		// device whose read fails is a real symptom worth surfacing —
		// emit a single-line warning. We can't cheaply distinguish the
		// two cases here without re-running the trust probe (which would
		// double the syscalls), so we warn whenever the error looks like
		// a transport failure rather than a pairing/trust-state failure.
		if mdErr != nil && classifyTrustError(mdErr) {
			fmt.Fprintf(l.warn, "ios-tidy: warning: could not read metadata for %s: %v\n", udid, mdErr)
		}

		out = append(out, device.Device{
			UDID:       udid,
			Name:       name,
			Model:      model,
			IOSVersion: version,
		})
	}
	return out, nil
}

// readMetadata returns ("", "", "", err) on any lockdown failure. The
// caller (List) decides whether to surface the error as a warning based
// on classifyTrustError. Returning the error rather than swallowing it
// gives the caller observability without forcing a hard failure (an
// untrusted device is normal, not exceptional).
func readMetadata(entry ios.DeviceEntry) (name, model, version string, err error) {
	values, vErr := ios.GetValues(entry)
	if vErr != nil {
		return "", "", "", vErr
	}
	return values.Value.DeviceName, values.Value.ProductType, values.Value.ProductVersion, nil
}

type trustChecker struct{}

func (t *trustChecker) Trusted(_ context.Context, udid string) (bool, error) {
	// We re-list rather than caching the DeviceEntry from a prior
	// List() call: trust state can change between calls (user taps
	// "Trust" or "Don't Trust" on the device), so each probe must be
	// fresh.
	list, err := ios.ListDevices()
	if err != nil {
		// ListDevices errors are unambiguously transport-level
		// (usbmuxd is unreachable). Propagate.
		return false, err
	}
	for _, entry := range list.DeviceList {
		if entry.Properties.SerialNumber != udid {
			continue
		}
		conn, err := ios.ConnectLockdownWithSession(entry)
		if err != nil {
			// Classify: pairing/trust-state errors collapse to
			// (false, nil); transport errors propagate as (false, err)
			// so the CLI can exit non-zero per the acceptance criterion.
			if classifyTrustError(err) {
				return false, err
			}
			return false, nil
		}
		conn.Close()
		return true, nil
	}
	// UDID not currently connected. This is unambiguously transport-
	// level (the device went away between List and Trusted) — but it is
	// recoverable for the *other* devices in the table, so we report
	// untrusted-without-error so the CLI keeps rendering. The caller
	// (cmd/ios-tidy/devices.go) builds the row from its own snapshot of
	// the lister output, so this device will appear once with a stale
	// trust value rather than zero times.
	return false, nil
}

// classifyTrustError reports whether err looks like a transport-level
// failure (usbmuxd down, lockdown TCP reset, TCC blocking pair-record
// reads on macOS Tahoe) rather than a pairing/trust-state failure
// (PairingDialogResponsePending, UserDeniedPairingError, missing pair
// record, StartSession failure).
//
// Returns true for transport. Returns false for trust-state or nil.
//
// String-matching is the only mechanism available because go-ios does
// not export sentinel errors for these classes (verified against
// connect.go and pair.go at the pinned v1.0.213 tag, 2026-05-24).
// The prefixes used here are the ones go-ios actually returns: see
// `fmt.Errorf("USBMuxConnection failed with: %v", ...)` etc. in
// ios/connect.go.
func classifyTrustError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()

	// Trust-state markers. If we match any of these the error is NOT
	// transport — it means the device is reachable but the user has
	// not (yet, or has refused to) tap Trust.
	trustStateMarkers := []string{
		"PairingDialogResponsePending",
		"UserDeniedPairingError",
		"could not retrieve PairRecord", // device exists but no pair record on disk
		"StartSession failed",           // session refused — usually means not trusted
	}
	for _, m := range trustStateMarkers {
		if strings.Contains(msg, m) {
			return false
		}
	}

	// Transport markers. If we match any of these the error IS
	// transport — usbmuxd is unreachable or the lockdown TCP channel
	// died mid-call.
	transportMarkers := []string{
		"USBMuxConnection failed",
		"Lockdown connection failed",
		"Could not connect to usbmuxd socket",
		"failed to dial",
	}
	for _, m := range transportMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}

	// Unknown error shape. We bias toward transport because surfacing
	// a non-zero exit is the safer default for "unknown failure" —
	// silently swallowing an unknown error would mask real bugs (the
	// failure mode the cycle-1 review flagged). If the user sees this
	// path, the error message itself goes to stderr unchanged, so the
	// shape can be filed against this function.
	return true
}
