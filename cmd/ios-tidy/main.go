package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/iosbackend"
)

func main() {
	exit := dispatch(
		context.Background(),
		os.Stdout, os.Stderr,
		os.Args[1:],
		iosbackend.NewDeviceLister(),
		iosbackend.NewTrustChecker(),
	)
	os.Exit(exit)
}

// dispatch is the testable core of main(). Pulling stdout, stderr,
// args, and the seam impls out of os/globals lets unit tests run it
// against bytes.Buffers and fakes.
//
// The `devices` subcommand is wired here so the test in Task 7e can
// observe that dispatch correctly routes to newDevicesCmd. Until
// Task 7 lands, the case is present but newDevicesCmd does not exist,
// so we cannot reference it yet — the switch handles `devices` by
// printing "not yet implemented" and returning a sentinel non-zero
// exit. Task 7 replaces the case body with the real wiring.
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
		// Task 7 replaces this body with:
		//   return newDevicesCmd(out, errOut, lister, checker).Run(ctx, args[1:])
		_ = lister
		_ = checker
		_ = ctx
		fmt.Fprintln(errOut, "ios-tidy: devices subcommand not yet implemented (Task 7)")
		return 3
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
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "global flags:")
	fmt.Fprintln(w, "  --version  print version and exit")
	fmt.Fprintln(w, "  --help     print this help and exit")
}
