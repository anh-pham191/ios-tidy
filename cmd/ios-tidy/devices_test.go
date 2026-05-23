package main

import (
	"bytes"
	"context"
	"errors"
	"slices"
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

// splitFields splits a single output line on runs of whitespace and
// returns its cells. Used to assert on cell positions without coupling
// to the renderer's exact gutter width.
func splitFields(line string) []string {
	return strings.Fields(line)
}

// lineContaining returns the first line of out whose first whitespace
// field equals udid, or "" if no such line exists.
func lineContaining(out, udid string) string {
	for _, ln := range strings.Split(out, "\n") {
		fields := strings.Fields(ln)
		if len(fields) > 0 && fields[0] == udid {
			return ln
		}
	}
	return ""
}

func TestDevicesCmd_tableShowsTrustStateAndMetadata(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{
		{UDID: "AAAA", Name: "iPhoneOne", Model: "iPhone15,2", IOSVersion: "18.4"},
		{UDID: "BBBB", Name: "", Model: "", IOSVersion: ""}, // untrusted: lockdown values unreadable
	}}
	checker := &device.FakeTrustChecker{Trusts: map[string]bool{"AAAA": true}}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{})

	if exit != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", exit, errOut.String())
	}
	if lister.Calls != 1 {
		t.Errorf("Lister.Calls = %d, want 1", lister.Calls)
	}
	if got, want := checker.Queried, []string{"AAAA", "BBBB"}; !slices.Equal(got, want) {
		t.Errorf("Queried UDIDs = %v, want %v", got, want)
	}

	stdout := out.String()
	for _, hdr := range []string{"UDID", "NAME", "MODEL", "IOS", "TRUST"} {
		if !strings.Contains(stdout, hdr) {
			t.Errorf("stdout missing header %q.\nstdout:\n%s", hdr, stdout)
		}
	}

	rowA := lineContaining(stdout, "AAAA")
	if rowA == "" {
		t.Fatalf("stdout missing row for AAAA.\nstdout:\n%s", stdout)
	}
	cellsA := splitFields(rowA)
	if len(cellsA) < 5 {
		t.Fatalf("AAAA row has %d cells, want >=5: %q", len(cellsA), rowA)
	}
	if cellsA[0] != "AAAA" || cellsA[1] != "iPhoneOne" || cellsA[2] != "iPhone15,2" || cellsA[3] != "18.4" || cellsA[4] != "trusted" {
		t.Errorf("AAAA cells = %v, want [AAAA iPhoneOne iPhone15,2 18.4 trusted]", cellsA)
	}

	rowB := lineContaining(stdout, "BBBB")
	if rowB == "" {
		t.Fatalf("stdout missing row for BBBB.\nstdout:\n%s", stdout)
	}
	cellsB := splitFields(rowB)
	if len(cellsB) < 5 {
		t.Fatalf("BBBB row has %d cells, want >=5: %q", len(cellsB), rowB)
	}
	if cellsB[0] != "BBBB" || cellsB[1] != "-" || cellsB[2] != "-" || cellsB[3] != "-" || cellsB[4] != "untrusted" {
		t.Errorf("BBBB cells = %v, want [BBBB - - - untrusted]", cellsB)
	}
}

func TestDevicesCmd_jsonOutputUsesTrustedBool(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{
		{UDID: "AAAA", Name: "iPhone One", Model: "iPhone15,2", IOSVersion: "18.4"},
		{UDID: "BBBB"}, // untrusted
	}}
	checker := &device.FakeTrustChecker{Trusts: map[string]bool{"AAAA": true}}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{"--json"})

	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", exit, errOut.String())
	}

	stdout := out.String()
	for _, want := range []string{
		`"udid": "AAAA"`,
		`"udid": "BBBB"`,
		`"trusted": true`,
		`"trusted": false`,
		`"name": "iPhone One"`,
		`"iosVersion": "18.4"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("JSON missing %q.\nstdout:\n%s", want, stdout)
		}
	}
	for _, banned := range []string{"UDID  ", "TRUST"} {
		if strings.Contains(stdout, banned) {
			t.Errorf("JSON mode leaked table output %q.\nstdout:\n%s", banned, stdout)
		}
	}
}

func TestDevicesCmd_listerTransportFailureReturnsNonZeroExit(t *testing.T) {
	listErr := errors.New("usbmuxd: connection refused")
	lister := &device.FakeLister{Err: listErr}
	checker := &device.FakeTrustChecker{}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{})

	if exit == 0 {
		t.Fatalf("exit = 0 on transport failure, want non-zero")
	}
	if !strings.Contains(errOut.String(), "usbmuxd: connection refused") {
		t.Errorf("stderr should mention transport error, got %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty on failure, got %q", out.String())
	}
}

func TestDevicesCmd_trustCheckFailureReturnsNonZeroExit(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{{UDID: "AAAA"}}}
	checker := &device.FakeTrustChecker{Err: errors.New("lockdown timeout")}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{})

	if exit == 0 {
		t.Fatalf("exit = 0 on trust-check failure, want non-zero")
	}
	if !strings.Contains(errOut.String(), "trust check failed for AAAA") {
		t.Errorf("stderr should explain trust failure with UDID, got %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "lockdown timeout") {
		t.Errorf("stderr should include underlying error, got %q", errOut.String())
	}
}

func TestDevicesCmd_emitsTahoeHintOnPairRecordError(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{{UDID: "AAAA"}}}
	checker := &device.FakeTrustChecker{Err: errors.New("could not retrieve PairRecord with error: permission denied")}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{})

	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero on trust-check transport failure")
	}
	if !strings.Contains(errOut.String(), "go-ios issue #710") {
		t.Errorf("stderr should include Tahoe TCC hint, got %q", errOut.String())
	}
}

func TestDevicesCmd_doesNotEmitTahoeHintForUnrelatedErrors(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{{UDID: "AAAA"}}}
	checker := &device.FakeTrustChecker{Err: errors.New("lockdown timeout")}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	_ = cmd.Run(context.Background(), []string{})

	if strings.Contains(errOut.String(), "go-ios issue #710") {
		t.Errorf("Tahoe hint should not fire on unrelated errors, got %q", errOut.String())
	}
}

func TestDevicesCmd_emptyListTableExitsZeroWithStderrNotice(t *testing.T) {
	lister := &device.FakeLister{Devices: nil}
	checker := &device.FakeTrustChecker{}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{})

	if exit != 0 {
		t.Fatalf("exit = %d on empty list, want 0", exit)
	}
	if !strings.Contains(errOut.String(), "no iPhones connected") {
		t.Errorf("stderr should say no devices connected, got %q", errOut.String())
	}
	if checker.Queried != nil {
		t.Errorf("TrustChecker should not be queried for an empty list, got %v", checker.Queried)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty for empty list, got %q", out.String())
	}
}

func TestDevicesCmd_emptyListJSONAlsoExitsZeroWithStderrNotice(t *testing.T) {
	lister := &device.FakeLister{Devices: nil}
	checker := &device.FakeTrustChecker{}
	var out, errOut bytes.Buffer

	cmd := newDevicesCmd(&out, &errOut, lister, checker)
	exit := cmd.Run(context.Background(), []string{"--json"})

	if exit != 0 {
		t.Fatalf("exit = %d on empty list with --json, want 0", exit)
	}
	if !strings.Contains(errOut.String(), "no iPhones connected") {
		t.Errorf("stderr should say no devices connected even with --json, got %q", errOut.String())
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty for empty list (no JSON array on empty), got %q", out.String())
	}
}

func TestDispatch_devicesSubcommandWiresFakes(t *testing.T) {
	lister := &device.FakeLister{Devices: []device.Device{{UDID: "AAAA"}}}
	checker := &device.FakeTrustChecker{Trusts: map[string]bool{"AAAA": true}}
	var out, errOut bytes.Buffer

	exit := dispatch(context.Background(), &out, &errOut, []string{"devices"}, lister, checker)

	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr=%q)", exit, errOut.String())
	}
	if lister.Calls != 1 {
		t.Errorf("Lister.Calls = %d, want 1", lister.Calls)
	}
}
