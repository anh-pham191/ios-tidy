package apps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

func TestProbeOutcome_stringRendering(t *testing.T) {
	cases := []struct {
		o    ProbeOutcome
		want string
	}{
		{ProbeUnknown, "unknown"},
		{ProbeVended, "vended"},
		{ProbeRefused, "refused"},
		{ProbeError, "error"},
	}
	for _, c := range cases {
		if got := c.o.String(); got != c.want {
			t.Errorf("ProbeOutcome(%d).String() = %q, want %q", c.o, got, c.want)
		}
	}
}

func TestProbe_successYieldsVendedAndClosesFS(t *testing.T) {
	fs := &sandbox.FakeFS{}
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.foo.bar", sandbox.FakeResponse{FS: fs})

	p := NewProber(sb)
	got := p.Probe(context.Background(), "UDID", "com.foo.bar")

	if got.Outcome != ProbeVended {
		t.Errorf("Outcome = %v, want ProbeVended", got.Outcome)
	}
	if got.Detail != "" {
		t.Errorf("Detail = %q, want empty", got.Detail)
	}
	if got.BundleID != "com.foo.bar" {
		t.Errorf("BundleID = %q, want com.foo.bar", got.BundleID)
	}
	if got.At.IsZero() {
		t.Errorf("At is zero; want a real timestamp")
	}
	if fs.CloseCalls != 1 {
		t.Errorf("FakeFS.CloseCalls = %d, want 1", fs.CloseCalls)
	}
}

func TestProbe_classifyErrors_table(t *testing.T) {
	cases := []struct {
		name    string
		errMsg  string
		wantOut ProbeOutcome
	}{
		// daemon refusals — matched case-insensitively against:
		//   /denied|refused|vendcontainer.*failed|connect afc service failed/i
		// …but ONLY after the transport-prefix check fails (see reTransport).
		{"vendContainer failed denied", "VendContainer failed: denied", ProbeRefused},
		{"connect afc service failed", "connect afc service failed", ProbeRefused},
		{"Connect AFC Service Failed mixed case", "Connect AFC Service Failed", ProbeRefused},
		{"refused mixed case", "Connection Refused by daemon", ProbeRefused},
		{"vendcontainer lowercase failed", "vendcontainer failed: policy mismatch", ProbeRefused},

		// not-installed signals — outcome ProbeUnknown
		{"application not installed", "Application com.foo.bar not installed", ProbeUnknown},
		{"installation lookup failed", "InstallationLookupFailed: bundle missing", ProbeUnknown},
		{"app not installed lowercase", "application 'x' is not installed on device", ProbeUnknown},

		// transport errors — ProbeError
		{"tcc denied is transport not refusal", "pair-record path denied by TCC", ProbeError},
		{"lockdown denied is transport not refusal", "lockdown session denied: pair invalid", ProbeError},
		{"transport reset", "transport reset by peer", ProbeError},
		{"unknown error vendcontainer", "unknown error during vendcontainer", ProbeError},
		{"plist decode failure", "plist decode: malformed", ProbeError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sb := sandbox.NewFakeSandbox()
			sb.SetResponse("com.foo.bar", sandbox.FakeResponse{Err: errors.New(c.errMsg)})

			p := NewProber(sb)
			got := p.Probe(context.Background(), "UDID", "com.foo.bar")

			if got.Outcome != c.wantOut {
				t.Errorf("Outcome for %q = %v, want %v", c.errMsg, got.Outcome, c.wantOut)
			}
			if got.Detail != c.errMsg {
				t.Errorf("Detail = %q, want %q", got.Detail, c.errMsg)
			}
		})
	}
}

func TestProbe_timeoutYieldsErrorNotRefused(t *testing.T) {
	sb := sandbox.NewFakeSandbox()
	sb.SetResponse("com.hang.app", sandbox.FakeResponse{Hang: true})

	p := NewProber(sb)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	got := p.Probe(ctx, "UDID", "com.hang.app")

	if got.Outcome != ProbeError {
		t.Errorf("Outcome = %v, want ProbeError (a timeout does not tell us the daemon's policy)", got.Outcome)
	}
	if !strings.Contains(strings.ToLower(got.Detail), "timeout") {
		t.Errorf("Detail = %q, want it to contain 'timeout'", got.Detail)
	}
}
