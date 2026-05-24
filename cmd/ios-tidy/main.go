package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/iosbackend"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

func main() {
	// Install a signal handler on the root context so Ctrl-C (SIGINT) and
	// SIGTERM cancel long-running operations cleanly rather than killing
	// the process mid-AFC-transfer.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exit := dispatch(
		ctx,
		os.Stdout, os.Stderr,
		os.Args[1:],
		iosbackend.NewDeviceLister(),
		iosbackend.NewTrustChecker(),
	)
	os.Exit(exit)
}

// dispatch is the testable core of main(). Pulling stdout, stderr,
// args, and the seam impls out of os/globals lets unit tests run it
// against bytes.Buffers and fakes. Each subcommand case constructs
// its handler with the injected seams and delegates.
func dispatch(
	ctx context.Context,
	out, errOut io.Writer,
	args []string,
	lister device.Lister,
	checker device.TrustChecker,
) int {
	if len(args) == 0 {
		printUsage(errOut)
		return 2
	}
	switch args[0] {
	case "--version", "-v":
		fmt.Fprintln(out, Version)
		return 0
	case "--help", "-h":
		printUsage(out)
		return 0
	case "devices":
		return newDevicesCmd(out, errOut, lister, checker).Run(ctx, args[1:])
	case "storage":
		fs := flag.NewFlagSet("storage", flag.ContinueOnError)
		fs.SetOutput(errOut)
		opts := storageOpts{}
		fs.StringVar(&opts.Device, "device", "", "UDID to target; omit if exactly one device is connected")
		fs.BoolVar(&opts.JSON, "json", false, "emit JSON instead of a table")
		fs.IntVar(&opts.Limit, "limit", 0, "show only the top N apps by total bytes; 0 or negative means all")
		// splitFlagsAndPositionals fixes the flag.Parse-stops-at-positional
		// trap. storage takes NO positionals; any extras are a usage error.
		flagArgs, positionals := splitFlagsAndPositionals(fs, args[1:])
		if err := fs.Parse(flagArgs); err != nil {
			return 2 // flag.ContinueOnError already wrote usage to errOut
		}
		if len(positionals) > 0 {
			fmt.Fprintf(errOut, "storage: unexpected positional argument(s): %v\n", positionals)
			return 2
		}
		sc := iosbackend.NewStorage()
		al, _ := iosbackend.NewApps()
		return runStorage(ctx, opts, lister, sc, al, out, errOut)
	case "crashlogs":
		deps := runDeps{
			Lister:   lister,
			Client:   iosbackend.NewCrashLogs(),
			Prompter: ui.NewStdinPrompter(os.Stdin, errOut),
			Stdout:   out,
			Stderr:   errOut,
		}
		return runCrashLogs(ctx, deps, args[1:])
	case "apps":
		al, _ := iosbackend.NewApps()
		deps := appsDeps{
			Lister:  al,
			Devices: lister,
			Sandbox: iosbackend.NewSandbox(),
			// Store left nil — the probe subcommand builds a FileProbeStore
			// from --store-dir or the default user config dir. Leaving Store
			// nil keeps `apps list` from paying for a probes/ mkdir it
			// doesn't use.
			Prompter: ui.NewStdinPrompter(os.Stdin, errOut),
			Stdout:   out,
			Stderr:   errOut,
		}
		return runApps(ctx, deps, args[1:])
	default:
		fmt.Fprintf(errOut, "ios-tidy: unknown subcommand %q\n", args[0])
		printUsage(errOut)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: ios-tidy <subcommand> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "subcommands:")
	fmt.Fprintln(w, "  devices    list connected iPhones")
	fmt.Fprintln(w, "  storage    show device free/used + app sizes")
	fmt.Fprintln(w, "  crashlogs  list, pull, or clean crash reports")
	fmt.Fprintln(w, "  apps       list installed apps + probe per-app vending")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "global flags:")
	fmt.Fprintln(w, "  --version  print version and exit")
	fmt.Fprintln(w, "  --help     print this help and exit")
}
