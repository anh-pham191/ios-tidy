package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// devicesCmd holds wired-up dependencies for the `devices` subcommand.
// It exists as a struct so tests can substitute fakes for the real
// go-ios-backed implementations from internal/iosbackend.
type devicesCmd struct {
	out     io.Writer
	errOut  io.Writer
	lister  device.Lister
	checker device.TrustChecker
}

func newDevicesCmd(out, errOut io.Writer, lister device.Lister, checker device.TrustChecker) *devicesCmd {
	return &devicesCmd{out: out, errOut: errOut, lister: lister, checker: checker}
}

// deviceRow is the JSON-output shape. We keep it private to cmd/ios-tidy
// because nothing else needs the wire format; the renderer is the only
// consumer. JSON keys use camelCase per SHARED_CONTEXT.md §11.
type deviceRow struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	IOSVersion string `json:"iosVersion"`
	Trusted    bool   `json:"trusted"`
}

// Run parses args, queries the seams, and writes either a table or
// JSON. It returns the desired process exit code so tests can assert
// on it without os.Exit (which would terminate the test binary).
func (c *devicesCmd) Run(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(c.errOut)
	asJSON := fs.Bool("json", false, "emit machine-readable JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	devices, err := c.lister.List(ctx)
	if err != nil {
		fmt.Fprintf(c.errOut, "ios-tidy: failed to list devices: %v\n", err)
		return 1
	}

	if len(devices) == 0 {
		fmt.Fprintln(c.errOut, "ios-tidy: no iPhones connected over USB.")
		return 0
	}

	rows := make([]deviceRow, 0, len(devices))
	for _, d := range devices {
		trusted, err := c.checker.Trusted(ctx, d.UDID)
		if err != nil {
			fmt.Fprintf(c.errOut, "ios-tidy: trust check failed for %s: %v\n", d.UDID, err)
			emitTahoeHintIfApplicable(c.errOut, err)
			return 1
		}
		rows = append(rows, deviceRow{
			UDID:       d.UDID,
			Name:       d.Name,
			Model:      d.Model,
			IOSVersion: d.IOSVersion,
			Trusted:    trusted,
		})
	}

	if *asJSON {
		return c.writeJSON(rows)
	}
	return c.writeTable(rows)
}

func (c *devicesCmd) writeTable(rows []deviceRow) int {
	header := []string{"UDID", "NAME", "MODEL", "IOS", "TRUST"}
	tableRows := make([][]string, len(rows))
	for i, r := range rows {
		tableRows[i] = []string{
			r.UDID,
			ui.DashIfEmpty(r.Name),
			ui.DashIfEmpty(r.Model),
			ui.DashIfEmpty(r.IOSVersion),
			trustLabel(r.Trusted),
		}
	}
	if err := ui.RenderTable(c.out, header, tableRows); err != nil {
		fmt.Fprintf(c.errOut, "ios-tidy: failed to render table: %v\n", err)
		return 1
	}
	return 0
}

func (c *devicesCmd) writeJSON(rows []deviceRow) int {
	enc := json.NewEncoder(c.out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		fmt.Fprintf(c.errOut, "ios-tidy: failed to encode JSON: %v\n", err)
		return 1
	}
	return 0
}

func trustLabel(trusted bool) string {
	if trusted {
		return "trusted"
	}
	return "untrusted"
}

// emitTahoeHintIfApplicable writes a single-line hint to errOut when the
// trust-check error message looks like a macOS 26 Tahoe TCC issue (see
// RESEARCH.md §6 — go-ios #710). The hint is best-effort and string-based
// because go-ios does not export sentinel error values; matching on
// "pair record" / "permission denied" covers the symptoms users actually
// see on Tahoe.
func emitTahoeHintIfApplicable(errOut io.Writer, err error) {
	if err == nil {
		return
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "pair record") || strings.Contains(msg, "permission denied") {
		fmt.Fprintln(errOut, "ios-tidy: hint: macOS Tahoe may be blocking pair-record reads via TCC (see go-ios issue #710).")
	}
}
