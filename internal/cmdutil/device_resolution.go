// Package cmdutil holds glue helpers that are shared between the
// ios-tidy CLI binary (cmd/ios-tidy) and the ios-tidy-mcp server
// (cmd/ios-tidy-mcp). Nothing here may import go-ios; this package
// depends only on the seam packages under internal/.
//
// The original home of these helpers was cmd/ios-tidy/ — they moved
// up to a shared package when the MCP server gained the same
// device-resolution UX (zero / one / many devices, optional override
// UDID) so the two binaries cannot drift.
package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/anh-pham191/ios-tidy/internal/device"
)

// ErrNoDevicesAttached is the sentinel returned by ResolveDevice when
// the device list is empty (as opposed to "lookup failed"). CLI callers
// map it to exit code 0 — M1 spec says zero-device is a clean,
// informative no-op, not an error. The MCP server treats it specially
// too: the empty-list outcome is surfaced as a tool result string
// "no devices attached" rather than an error, so the LLM caller can
// react instead of seeing a generic failure.
//
// ResolveDevice still writes the human-readable stderr line; callers
// that want to suppress duplicate output should consult the sentinel
// and route accordingly.
var ErrNoDevicesAttached = errors.New("no devices attached")

// ResolveDevice picks the target UDID using a sentinel-error pattern.
//
// Behaviour:
//   - List the connected devices; on lister failure, return the wrapped
//     error (caller maps to non-zero exit).
//   - If override != "", verify it appears in the device list. If it
//     does, return it. If not, print `device %q not attached` to stderr
//     and return a "device not attached" error. On lister failure with
//     an override, fall back to the verbatim shortcut so transient
//     listing errors don't block a user who has explicitly named the
//     target — the device-call downstream will surface a real failure
//     if the UDID is wrong.
//   - With no override: zero devices → ErrNoDevicesAttached sentinel;
//     exactly one → that UDID; many → error listing UDIDs with usage
//     hint.
func ResolveDevice(ctx context.Context, l device.Lister, override string, stderr io.Writer) (string, error) {
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
		return "", ErrNoDevicesAttached
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
