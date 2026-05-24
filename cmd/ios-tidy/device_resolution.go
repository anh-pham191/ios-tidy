// cmd/ios-tidy/device_resolution.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/anh-pham191/ios-tidy/internal/device"
)

// errNoDevicesAttached is the sentinel returned by resolveDevice when the
// device list is empty (as opposed to "lookup failed"). Callers map it to
// exit code 0 — M1 spec says zero-device is a clean, informative no-op, not
// an error. The stderr message is still emitted by resolveDevice so the user
// learns why nothing happened.
var errNoDevicesAttached = errors.New("no devices attached")

// resolveDevice picks the target UDID using a sentinel-error pattern.
//
// Behaviour:
//   - List the connected devices; on lister failure, return the wrapped error
//     (caller maps to non-zero exit).
//   - If override != "", verify it appears in the device list. If it does,
//     return it. If not, print `device %q not attached` to stderr and return
//     a "device not attached" error. On lister failure with an override, fall
//     back to the verbatim shortcut so transient listing errors don't block a
//     user who has explicitly named the target — the device-call downstream
//     will surface a real failure if the UDID is wrong.
//   - With no override: zero devices → errNoDevicesAttached sentinel; exactly
//     one → that UDID; many → error listing UDIDs with usage hint.
func resolveDevice(ctx context.Context, l device.Lister, override string, stderr io.Writer) (string, error) {
	devs, err := l.List(ctx)
	if err != nil {
		// Fallback: if the caller named a device explicitly, honour it
		// verbatim despite the lister error. The device-targeted call
		// downstream is the authoritative check.
		if override != "" {
			return override, nil
		}
		return "", fmt.Errorf("list devices: %w", err)
	}
	if override != "" {
		for _, d := range devs {
			if d.UDID == override {
				return override, nil
			}
		}
		fmt.Fprintf(stderr, "device %q not attached\n", override)
		return "", errors.New("device not attached")
	}
	switch len(devs) {
	case 0:
		fmt.Fprintln(stderr, "no devices attached")
		return "", errNoDevicesAttached
	case 1:
		return devs[0].UDID, nil
	default:
		fmt.Fprintln(stderr, "multiple devices attached; use --device UDID:")
		for _, d := range devs {
			fmt.Fprintf(stderr, "  %s  %s\n", d.UDID, d.Name)
		}
		return "", errors.New("multiple devices attached")
	}
}
