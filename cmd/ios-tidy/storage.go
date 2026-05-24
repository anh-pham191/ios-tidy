// cmd/ios-tidy/storage.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/storage"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

// storageOpts is the parsed-flag shape for the `storage` subcommand.
type storageOpts struct {
	Device string
	JSON   bool
	Limit  int
}

func runStorage(
	ctx context.Context,
	opts storageOpts,
	dl device.Lister,
	sc storage.Client,
	al apps.Lister,
	stdout, stderr io.Writer,
) int {
	udid, exit, err := selectDevice(ctx, opts.Device, dl, stderr)
	if exit != 0 || err != nil {
		return exit
	}
	if udid == "" {
		return 0
	}

	info, appList, err := fetchInParallel(ctx, udid, sc, al)
	if err != nil {
		fmt.Fprintf(stderr, "fetch device data: %v\n", err)
		return 1
	}
	apps.Sort(appList)
	appList = apps.Limit(appList, opts.Limit)

	if opts.JSON {
		return renderJSON(info, appList, stdout, stderr)
	}
	return renderText(info, appList, stdout, stderr)
}

// fetchPair carries the named outputs of fetchInParallel so the goroutine
// bodies write to typed fields rather than four loose vars.
type fetchPair struct {
	info    storage.DeviceInfo
	apps    []apps.App
	infoErr error
	appsErr error
}

// fetchInParallel runs DeviceInfo and UserApps concurrently AND honours ctx
// cancellation: it derives a child context with cancel, and the first
// goroutine to error calls cancel(), which signals the other underlying call
// (if it consults ctx) to stop early. We do this with a sync.WaitGroup + a
// child cancel instead of pulling in golang.org/x/sync/errgroup; two
// goroutines is not enough to justify the dependency.
//
// Contract:
//   - Returns (info, apps, nil) iff both calls succeed.
//   - Returns (zero, nil, err) on the first error from either goroutine; the
//     other goroutine is signalled via the cancelled child context.
//   - If the caller cancels the parent ctx, both goroutines see ctx.Done() via
//     the derived child context.
//   - Both goroutines always complete before Wait returns — no leaks.
func fetchInParallel(ctx context.Context, udid string, sc storage.Client, al apps.Lister) (storage.DeviceInfo, []apps.App, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()

	var pair fetchPair
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pair.info, pair.infoErr = sc.DeviceInfo(child, udid)
		if pair.infoErr != nil {
			cancel()
		}
	}()
	go func() {
		defer wg.Done()
		pair.apps, pair.appsErr = al.UserApps(child, udid)
		if pair.appsErr != nil {
			cancel()
		}
	}()
	wg.Wait()

	if pair.infoErr != nil {
		return storage.DeviceInfo{}, nil, fmt.Errorf("device info: %w", pair.infoErr)
	}
	if pair.appsErr != nil {
		return storage.DeviceInfo{}, nil, fmt.Errorf("user apps: %w", pair.appsErr)
	}
	return pair.info, pair.apps, nil
}

func renderText(info storage.DeviceInfo, list []apps.App, stdout, _ io.Writer) int {
	pct := 0.0
	if info.TotalBytes > 0 {
		pct = float64(info.FreeBytes) / float64(info.TotalBytes) * 100.0
	}
	fmt.Fprintf(stdout, "%s — %s free of %s (%.1f%%)\n",
		info.Model,
		ui.FormatBytes(info.FreeBytes),
		ui.FormatBytes(info.TotalBytes),
		pct,
	)

	tbl := ui.NewTable("bundle id", "name", "version", "dynamic", "static", "total", "file-sharing")
	for _, a := range list {
		share := "no"
		if a.FileSharingEnabled {
			share = "yes"
		}
		tbl.AddRow(
			a.BundleID,
			a.Name,
			a.Version,
			ui.FormatBytes(a.DynamicBytes),
			ui.FormatBytes(a.StaticBytes),
			ui.FormatBytes(a.DynamicBytes+a.StaticBytes),
			share,
		)
	}
	fmt.Fprint(stdout, tbl.Render([]ui.Alignment{
		ui.AlignLeft, ui.AlignLeft, ui.AlignLeft,
		ui.AlignRight, ui.AlignRight, ui.AlignRight,
		ui.AlignLeft,
	}))
	return 0
}

func renderJSON(info storage.DeviceInfo, list []apps.App, stdout, stderr io.Writer) int {
	payload := struct {
		Device storage.DeviceInfo `json:"device"`
		Apps   []apps.App         `json:"apps"`
	}{Device: info, Apps: list}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintf(stderr, "encode json: %v\n", err)
		return 1
	}
	return 0
}

func selectDevice(ctx context.Context, requested string, dl device.Lister, stderr io.Writer) (string, int, error) {
	devices, err := dl.List(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "list devices: %v\n", err)
		return "", 1, err
	}
	if requested != "" {
		for _, d := range devices {
			if d.UDID == requested {
				return requested, 0, nil
			}
		}
		fmt.Fprintf(stderr, "device %q not connected\n", requested)
		return "", 1, errors.New("device not connected")
	}
	switch len(devices) {
	case 0:
		fmt.Fprintln(stderr, "no devices connected")
		return "", 0, nil
	case 1:
		return devices[0].UDID, 0, nil
	default:
		fmt.Fprintln(stderr, "multiple devices connected; pass --device <udid>:")
		for _, d := range devices {
			fmt.Fprintf(stderr, "  %s  %s  %s\n", d.UDID, d.Name, d.IOSVersion)
		}
		return "", 1, errors.New("ambiguous device")
	}
}
