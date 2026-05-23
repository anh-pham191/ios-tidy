package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/device"
)

func TestDispatch_unknownSubcommandReturnsNonZeroExit(t *testing.T) {
	var out, errOut bytes.Buffer
	exit := dispatch(context.Background(), &out, &errOut, []string{"nonsense"}, &device.FakeLister{}, &device.FakeTrustChecker{})
	if exit == 0 {
		t.Fatalf("exit = 0 on unknown subcommand, want non-zero")
	}
	if !strings.Contains(errOut.String(), "unknown subcommand") {
		t.Errorf("stderr should explain the failure, got %q", errOut.String())
	}
}

func TestDispatch_noArgsPrintsUsageAndReturnsNonZeroExit(t *testing.T) {
	var out, errOut bytes.Buffer
	exit := dispatch(context.Background(), &out, &errOut, nil, &device.FakeLister{}, &device.FakeTrustChecker{})
	if exit == 0 {
		t.Fatalf("exit = 0 with no args, want non-zero")
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr should print usage, got %q", errOut.String())
	}
}

func TestDispatch_versionFlagPrintsVersionAndExitsZero(t *testing.T) {
	var out, errOut bytes.Buffer
	exit := dispatch(context.Background(), &out, &errOut, []string{"--version"}, &device.FakeLister{}, &device.FakeTrustChecker{})
	if exit != 0 {
		t.Fatalf("exit = %d on --version, want 0", exit)
	}
	if !strings.Contains(out.String(), Version) {
		t.Errorf("stdout should contain version %q, got %q", Version, out.String())
	}
}

func TestDispatch_helpFlagPrintsUsageToStdoutAndExitsZero(t *testing.T) {
	var out, errOut bytes.Buffer
	exit := dispatch(context.Background(), &out, &errOut, []string{"--help"}, &device.FakeLister{}, &device.FakeTrustChecker{})
	if exit != 0 {
		t.Fatalf("exit = %d on --help, want 0", exit)
	}
	if !strings.Contains(out.String(), "usage") {
		t.Errorf("stdout should print usage, got %q", out.String())
	}
}
