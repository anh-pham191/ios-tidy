package apps

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/sandbox"
)

// reNotInstalled fires when the daemon (or installation_proxy) tells us the
// bundle isn't installed. Either form means "we cannot conclude anything
// about the daemon's vending policy from this attempt".
//
// Patterns:
//
//	/application.*not installed/i  — matches "Application com.foo not installed",
//	                                 "application 'x' is not installed on device", etc.
//	InstallationLookupFailed       — substring match (case-sensitive: this is the
//	                                 daemon's literal error code from go-ios).
var (
	reNotInstalled       = regexp.MustCompile(`(?i)application.*not installed`)
	reInstallationLookup = regexp.MustCompile(`InstallationLookupFailed`)

	// reTransport fires for host-side / pairing-layer errors that have nothing
	// to do with the daemon's vending policy. These MUST win over reRefused so
	// a TCC error containing the word "denied" (e.g. "pair-record path denied
	// by TCC", RESEARCH.md §6 / go-ios #710) does not get steered into
	// ProbeRefused — M6 would then send the user to Settings instead of telling
	// them to repair their pair record.
	reTransport = regexp.MustCompile(`(?i)lockdown|pair-record|tcc|usbmuxd`)

	// reRefused fires when the daemon actively refused. The "vendcontainer.*failed"
	// branch covers RESEARCH.md §3's known iOS 17/18 refusal phrasing; "connect
	// afc service failed" covers go-ios open issue #653.
	reRefused = regexp.MustCompile(`(?i)denied|refused|vendcontainer.*failed|connect afc service failed`)
)

// ProbeOutcome classifies the result of asking the device's
// mobile_house_arrest daemon to vend a given app's sandbox.
type ProbeOutcome int

const (
	// ProbeUnknown means the probe could not draw a conclusion (e.g. the
	// bundle ID was not installed at probe time). NOT a daemon refusal.
	ProbeUnknown ProbeOutcome = iota
	// ProbeVended means house_arrest.VendContainer succeeded. The app is
	// eligible for sandbox-level cleanup.
	ProbeVended
	// ProbeRefused means the daemon refused. The app cannot be cleaned via
	// house_arrest; the user must use Settings on-device.
	ProbeRefused
	// ProbeError means a transport / connection failure. Retryable.
	ProbeError
)

func (o ProbeOutcome) String() string {
	switch o {
	case ProbeVended:
		return "vended"
	case ProbeRefused:
		return "refused"
	case ProbeError:
		return "error"
	default:
		return "unknown"
	}
}

// ProbeResult is one row of the probe cache.
type ProbeResult struct {
	BundleID string
	Outcome  ProbeOutcome
	Detail   string    // error message or empty
	At       time.Time // when probed
}

// Prober probes a single (udid, bundleID) pair. Implementations must NOT
// retry internally — the caller orchestrates sequencing.
type Prober interface {
	Probe(ctx context.Context, udid string, bundleID string) ProbeResult
}

// prober is the production implementation, driven by a sandbox.Sandbox seam.
type prober struct {
	sb sandbox.Sandbox
}

// NewProber returns a Prober that uses sb for the actual VendContainer call.
func NewProber(sb sandbox.Sandbox) Prober {
	return &prober{sb: sb}
}

func (p *prober) Probe(ctx context.Context, udid, bundleID string) ProbeResult {
	fs, err := p.sb.Open(ctx, udid, bundleID)
	at := time.Now().UTC()

	if err == nil {
		// MUST close to avoid leaking the AFC socket — we only needed
		// to know whether the daemon would vend.
		_ = fs.Close()
		return ProbeResult{BundleID: bundleID, Outcome: ProbeVended, At: at}
	}

	// A ctx-driven cancellation is NOT a daemon refusal. We cannot
	// conclude anything about the daemon's policy from a timeout —
	// surface as ProbeError so the user knows to retry, and embed
	// the substring "timeout" so callers can render a friendly hint.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ProbeResult{
			BundleID: bundleID,
			Outcome:  ProbeError,
			Detail:   "timeout: " + err.Error(),
			At:       at,
		}
	}

	msg := err.Error()
	return ProbeResult{
		BundleID: bundleID,
		Outcome:  classifyErr(msg),
		Detail:   msg,
		At:       at,
	}
}

// classifyErr maps a Sandbox.Open error message to a ProbeOutcome.
// Order matters:
//  1. "not installed" / InstallationLookupFailed → ProbeUnknown (the daemon
//     sometimes phrases a missing app as "VendContainer failed: ... not installed").
//  2. Transport / pairing-layer keywords → ProbeError (host-side problem; do
//     NOT misclassify as a daemon refusal even if the string also matches
//     reRefused's "denied"/"refused" alternation).
//  3. Daemon-refusal keywords → ProbeRefused.
//  4. Otherwise → ProbeError.
func classifyErr(msg string) ProbeOutcome {
	switch {
	case reNotInstalled.MatchString(msg):
		return ProbeUnknown
	case reInstallationLookup.MatchString(msg):
		return ProbeUnknown
	case reTransport.MatchString(msg):
		return ProbeError
	case reRefused.MatchString(msg):
		return ProbeRefused
	default:
		return ProbeError
	}
}
