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
