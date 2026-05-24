// internal/iosbackend/crashlogs.go
//
// crashLogsClient is the go-ios adapter for crashlogs.Client.
//
// Design (cycle-2 — see plan Revision history):
//   - Pull delegates to crashreport.DownloadReports, which opens one AFC
//     connection to com.apple.crashreportcopymobile, walks the crash-report
//     tree once, and pulls every filepath.Match-matching basename via
//     PullSingleFile internally. We do NOT iterate per-entry here — that
//     would be N+1 walks + N reconnects on a phone with many crashes.
//   - List uses crashreport.ListReports to enumerate paths (pattern is
//     forwarded; go-ios applies filepath.Match(pattern, filepath.Base(p))
//     internally — verified at
//     https://raw.githubusercontent.com/danielpaulus/go-ios/main/ios/crashreport/crashreport.go),
//     then opens one AFC connection to populate Size via Stat. ModTime is
//     left at the zero value (see plan Open question #1).
//   - Remove delegates to crashreport.RemoveReports. Per-entry failure
//     reporting is not supported by go-ios's bulk APIs; PullResult.Failures
//     and RemoveResult.Failures will be empty even when the device-side
//     walker hit individual errors (a known reduction documented in the
//     plan's Revision history).
package iosbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/afc"
	"github.com/danielpaulus/go-ios/ios/crashreport"
)

// crashReportCopyMobileService is the AFC-over-crashreport service name.
//
// Verified against go-ios ios/crashreport/crashreport.go at the pinned SHA.
const crashReportCopyMobileService = "com.apple.crashreportcopymobile"

type crashLogsClient struct{}

// NewCrashLogs returns a crashlogs.Client backed by go-ios.
func NewCrashLogs() crashlogs.Client { return &crashLogsClient{} }

// findDevice locates the ios.DeviceEntry for the given UDID, or returns an
// error if it's not currently attached.
func findDevice(udid string) (ios.DeviceEntry, error) {
	list, err := ios.ListDevices()
	if err != nil {
		return ios.DeviceEntry{}, fmt.Errorf("list devices: %w", err)
	}
	for _, d := range list.DeviceList {
		if d.Properties.SerialNumber == udid {
			return d, nil
		}
	}
	return ios.DeviceEntry{}, fmt.Errorf("device %q not attached", udid)
}

// openCrashReportAfc opens an AFC client against the
// com.apple.crashreportcopymobile service for the given device. Caller MUST
// Close() the returned client when done.
func openCrashReportAfc(device ios.DeviceEntry) (*afc.Client, error) {
	conn, err := ios.ConnectToService(device, crashReportCopyMobileService)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", crashReportCopyMobileService, err)
	}
	return afc.NewFromConn(conn), nil
}

func (c *crashLogsClient) List(ctx context.Context, udid, pattern string) ([]crashlogs.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	device, err := findDevice(udid)
	if err != nil {
		return nil, err
	}
	if pattern == "" {
		pattern = "*"
	}
	// go-ios applies filepath.Match(pattern, filepath.Base(p)) server-side;
	// no host-side re-filter needed.
	paths, err := crashreport.ListReports(device, pattern)
	if err != nil {
		return nil, fmt.Errorf("list reports: %w", err)
	}
	cli, err := openCrashReportAfc(device)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	entries := make([]crashlogs.Entry, 0, len(paths))
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := crashlogs.Entry{Path: p, ModTime: time.Time{}}
		if info, statErr := cli.Stat(p); statErr == nil {
			entry.Size = info.Size
		}
		// If Stat fails (e.g. file vanished between list and stat), include
		// the entry with Size=0 rather than dropping it; the user can still
		// see and pull it.
		entries = append(entries, entry)
	}
	return entries, nil
}

func (c *crashLogsClient) Pull(ctx context.Context, udid, pattern, dst string) (crashlogs.PullResult, error) {
	if err := ctx.Err(); err != nil {
		return crashlogs.PullResult{}, err
	}
	// Pre-list to compute counts + bytes for the result. If the user only
	// cares about success/fail and not the count, this extra round-trip
	// could be elided in a future optimisation.
	entries, err := c.List(ctx, udid, pattern)
	if err != nil {
		return crashlogs.PullResult{}, err
	}
	device, err := findDevice(udid)
	if err != nil {
		return crashlogs.PullResult{}, err
	}
	if pattern == "" {
		pattern = "*"
	}
	// One call, one connection, one walk, all matching files pulled.
	if err := crashreport.DownloadReports(device, pattern, dst); err != nil {
		return crashlogs.PullResult{}, fmt.Errorf("download reports: %w", err)
	}
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	// Pulled and Bytes reflect the pre-list snapshot, not the actual bytes
	// written under dst. If a crash report is rotated or removed between the
	// nested List call and DownloadReports the count overstates reality;
	// verify dst contents if exact accounting matters.
	return crashlogs.PullResult{Pulled: len(entries), Bytes: total}, nil
}

// Remove deletes crash log entries matching pattern on the device identified
// by udid. It first lists matching entries (so it can report a removed-count
// and a real bytes-freed figure), stats each entry to sum bytes, then calls
// go-ios crashreport.RemoveReports once. RemoveReports does not return
// per-file failures: if the whole call errors, nothing is reported as
// removed and the error is returned; if it succeeds, every listed entry is
// treated as removed and Failures is nil.
func (c *crashLogsClient) Remove(ctx context.Context, udid, pattern string) (crashlogs.RemoveResult, error) {
	if err := ctx.Err(); err != nil {
		return crashlogs.RemoveResult{}, err
	}
	entry, err := ios.GetDevice(udid)
	if err != nil {
		return crashlogs.RemoveResult{}, fmt.Errorf("get device %s: %w", udid, err)
	}

	// Snapshot for the byte-freed total before removing. ListReports is the
	// same call M3's List adapter uses; the result is shared between the
	// size-sum and the reported Removed count so they always agree.
	names, err := crashreport.ListReports(entry, pattern)
	if err != nil {
		return crashlogs.RemoveResult{}, fmt.Errorf("list before remove: %w", err)
	}

	// Open our own AFC connection to crashreportcopymobile to stat each
	// entry. Symmetric with how go-ios's own crashreport.ListReports /
	// DownloadReports / RemoveReports construct an AFC client.
	conn, err := ios.ConnectToService(entry, crashReportCopyMobileService)
	if err != nil {
		return crashlogs.RemoveResult{}, fmt.Errorf("connect %s: %w", crashReportCopyMobileService, err)
	}
	afcClient := afc.NewFromConn(conn)
	defer func() { _ = afcClient.Close() }()

	var bytes int64
	for _, n := range names {
		info, statErr := afcClient.Stat(n)
		if statErr != nil {
			// Best-effort: a stat miss is not fatal, but it does mean the
			// bytes-freed total under-reports by that entry's size.
			continue
		}
		// afc.FileInfo.Size is an int64 field (verified via `go doc
		// github.com/danielpaulus/go-ios/ios/afc.FileInfo` against the
		// pinned go-ios SHA — the API surface uses a field, not a method).
		bytes += info.Size
	}

	if err := crashreport.RemoveReports(entry, "", pattern); err != nil {
		return crashlogs.RemoveResult{}, fmt.Errorf("remove: %w", err)
	}
	return crashlogs.RemoveResult{
		Removed:  len(names),
		Bytes:    bytes,
		Failures: nil,
	}, nil
}
