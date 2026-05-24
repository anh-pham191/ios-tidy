package recommendations

import (
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/apps"
)

func TestClassifyLabel_boundaries(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "low"},
		{5, "low"},
		{9.999, "low"},
		{10.0, "normal"},
		{24.999, "normal"},
		{25.0, "high"},
		{50, "high"},
	}
	for _, c := range cases {
		if got := classifyLabel(c.pct); got != c.want {
			t.Errorf("classifyLabel(%v) = %q want %q", c.pct, got, c.want)
		}
	}
}

func TestPriorityForAppBytes(t *testing.T) {
	cases := []struct {
		bytes uint64
		want  Priority
	}{
		{0, PriorityLow},
		{100 * 1024 * 1024, PriorityLow},
		{499 * 1024 * 1024, PriorityLow},
		{500 * 1024 * 1024, PriorityMedium},
		{1024 * 1024 * 1024, PriorityMedium},
		{2 * 1024 * 1024 * 1024, PriorityHigh},
		{4 * 1024 * 1024 * 1024, PriorityHigh},
	}
	for _, c := range cases {
		if got := priorityForAppBytes(c.bytes); got != c.want {
			t.Errorf("priorityForAppBytes(%d) = %q want %q", c.bytes, got, c.want)
		}
	}
}

func TestBuild_emptyInputs_returnsNotTouchableAndZeroSummary(t *testing.T) {
	p := Build(Inputs{UDID: "U1", DeviceName: "iPhone One"})
	if p.Device.UDID != "U1" || p.Device.Name != "iPhone One" {
		t.Errorf("device echo wrong: %+v", p.Device)
	}
	if p.Summary.TotalBytes != 0 || p.Summary.FreeBytes != 0 {
		t.Errorf("expected zero summary: %+v", p.Summary)
	}
	if p.Summary.Label != "low" {
		t.Errorf("zero-total should label low, got %q", p.Summary.Label)
	}
	if p.NotTouchable.SystemData == "" || p.NotTouchable.Photos == "" || p.NotTouchable.MusicAndPodcasts == "" {
		t.Errorf("notTouchable disclosures must always be populated: %+v", p.NotTouchable)
	}
	if len(p.Recommendations) != 0 {
		t.Errorf("empty inputs should produce no recommendations, got %d", len(p.Recommendations))
	}
}

func TestBuild_crashlogsAboveThreshold_highPriority(t *testing.T) {
	in := Inputs{
		UDID:          "U1",
		FreeBytes:     50_000_000_000,
		TotalBytes:    100_000_000_000,
		CrashlogBytes: 10 * 1024 * 1024, // 10 MiB
	}
	p := Build(in)
	if len(p.Recommendations) == 0 {
		t.Fatalf("expected at least one recommendation")
	}
	first := p.Recommendations[0]
	if first.Action != ActionCleanCrashlogs {
		t.Errorf("first rec should be crashlogs, got %q", first.Action)
	}
	if first.Priority != PriorityHigh {
		t.Errorf("crashlogs should be high priority, got %q", first.Priority)
	}
	if first.ViaTool != ViaCrashlogsClean {
		t.Errorf("crashlogs viaTool wrong: %q", first.ViaTool)
	}
	if first.EstimatedRecoverBytes != 10*1024*1024 {
		t.Errorf("estimate wrong: %d", first.EstimatedRecoverBytes)
	}
}

func TestBuild_crashlogsBelowThreshold_noRec(t *testing.T) {
	in := Inputs{CrashlogBytes: crashlogThreshold - 1}
	p := Build(in)
	for _, r := range p.Recommendations {
		if r.Action == ActionCleanCrashlogs {
			t.Errorf("did not expect crashlog rec below threshold: %+v", r)
		}
	}
}

func TestBuild_skipsAppleSystemBundles(t *testing.T) {
	in := Inputs{
		FreeBytes:  100,
		TotalBytes: 1000,
		Apps: []apps.App{
			{BundleID: "com.apple.mobilesafari", Name: "Safari", DynamicBytes: 5 * 1024 * 1024 * 1024},
			{BundleID: "com.burbn.instagram", Name: "Instagram", DynamicBytes: 1 * 1024 * 1024 * 1024},
		},
	}
	p := Build(in)
	for _, r := range p.Recommendations {
		if r.BundleID == "com.apple.mobilesafari" {
			t.Errorf("must not recommend any action for com.apple.* bundle: %+v", r)
		}
	}
	// Should still have Instagram.
	found := false
	for _, r := range p.Recommendations {
		if r.BundleID == "com.burbn.instagram" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected instagram rec, recs=%+v", p.Recommendations)
	}
}

func TestBuild_skipsTinyApps(t *testing.T) {
	// Apps below perAppRecThreshold (50 MiB) should not generate
	// recommendations — even acting on them frees nothing meaningful.
	in := Inputs{
		Apps: []apps.App{
			{BundleID: "com.cold.app", Name: "Cold", DynamicBytes: 0, StaticBytes: 0},
			{BundleID: "com.tiny.app", Name: "Tiny", DynamicBytes: 5 * 1024 * 1024},
			{BundleID: "com.warm.app", Name: "Warm", DynamicBytes: 1024 * 1024 * 1024},
		},
	}
	p := Build(in)
	for _, r := range p.Recommendations {
		if r.BundleID == "com.cold.app" || r.BundleID == "com.tiny.app" {
			t.Errorf("sub-threshold app should not generate rec: %+v", r)
		}
	}
}

func TestBuild_vendedAppGetsSandboxCleanAction(t *testing.T) {
	now := time.Now()
	in := Inputs{
		Apps: []apps.App{
			{BundleID: "com.foo.cleanable", Name: "Cleanable", DynamicBytes: 800 * 1024 * 1024},
		},
		ProbeResults: []apps.ProbeResult{
			{BundleID: "com.foo.cleanable", Outcome: apps.ProbeVended, At: now},
		},
	}
	p := Build(in)
	if len(p.Recommendations) != 1 {
		t.Fatalf("expected exactly one rec, got %d", len(p.Recommendations))
	}
	r := p.Recommendations[0]
	if r.Action != ActionCleanAppSandbox {
		t.Errorf("vended app should get clean_app_sandbox, got %q", r.Action)
	}
	if r.ViaTool != ViaAppsClean {
		t.Errorf("viaTool wrong: %q", r.ViaTool)
	}
}

func TestBuild_unvendedAppGetsOffloadAction(t *testing.T) {
	in := Inputs{
		Apps: []apps.App{
			{BundleID: "com.foo.offload", Name: "Offload", DynamicBytes: 800 * 1024 * 1024},
		},
		// No probe results -> not vended.
	}
	p := Build(in)
	if len(p.Recommendations) != 1 {
		t.Fatalf("expected 1 rec, got %d: %+v", len(p.Recommendations), p.Recommendations)
	}
	if p.Recommendations[0].Action != ActionOffloadApp {
		t.Errorf("expected offload action, got %q", p.Recommendations[0].Action)
	}
	if p.Recommendations[0].ViaTool != ViaOpenAppStorageSettings {
		t.Errorf("viaTool wrong: %q", p.Recommendations[0].ViaTool)
	}
	if !strings.Contains(p.Recommendations[0].Rationale, "preserves user data") {
		t.Errorf("offload rationale should mention preserving user data: %q",
			p.Recommendations[0].Rationale)
	}
}

func TestBuild_capsTopNAppsPerCategory(t *testing.T) {
	// 10 unvended apps; expect only top 5.
	var as []apps.App
	for i := 0; i < 10; i++ {
		as = append(as, apps.App{
			BundleID:     "com.example." + string(rune('a'+i)),
			Name:         "App",
			DynamicBytes: uint64(100*1024*1024 + i*1024),
		})
	}
	p := Build(Inputs{Apps: as})
	offloadCount := 0
	for _, r := range p.Recommendations {
		if r.Action == ActionOffloadApp {
			offloadCount++
		}
	}
	if offloadCount != topAppsByTotal {
		t.Errorf("expected exactly %d offload recs, got %d", topAppsByTotal, offloadCount)
	}
}

func TestBuild_lowSpaceNoLargeApp_genericFallback(t *testing.T) {
	// 5% free, no app >= 1 GiB.
	in := Inputs{
		FreeBytes:  5,
		TotalBytes: 100,
		Apps: []apps.App{
			{BundleID: "com.small.one", Name: "Small1", DynamicBytes: 10 * 1024 * 1024},
		},
	}
	p := Build(in)
	if p.Summary.Label != "low" {
		t.Fatalf("expected label low, got %q", p.Summary.Label)
	}
	found := false
	for _, r := range p.Recommendations {
		if r.Action == ActionGenericReview {
			found = true
		}
	}
	if !found {
		t.Errorf("expected generic review rec, got %+v", p.Recommendations)
	}
}

func TestBuild_lowSpaceWithLargeApp_noFallback(t *testing.T) {
	in := Inputs{
		FreeBytes:  5,
		TotalBytes: 100,
		Apps: []apps.App{
			{BundleID: "com.big.one", Name: "Big", DynamicBytes: 2 * 1024 * 1024 * 1024},
		},
	}
	p := Build(in)
	for _, r := range p.Recommendations {
		if r.Action == ActionGenericReview {
			t.Errorf("did not expect generic fallback when a large app exists: %+v", r)
		}
	}
}

func TestBuild_recommendationsSortedByPriorityThenBytes(t *testing.T) {
	in := Inputs{
		FreeBytes: 1, TotalBytes: 100,
		CrashlogBytes: 50 * 1024 * 1024, // high priority
		Apps: []apps.App{
			{BundleID: "com.medium.a", Name: "M", DynamicBytes: 600 * 1024 * 1024},
			{BundleID: "com.big.a", Name: "Big", DynamicBytes: 3 * 1024 * 1024 * 1024},
		},
	}
	p := Build(in)
	if len(p.Recommendations) < 2 {
		t.Fatalf("need >=2 recs, got %d", len(p.Recommendations))
	}
	// All high-priority recs must come before any medium-priority rec.
	sawMedium := false
	for _, r := range p.Recommendations {
		if r.Priority == PriorityMedium {
			sawMedium = true
		}
		if sawMedium && r.Priority == PriorityHigh {
			t.Errorf("high-priority rec found after a medium-priority rec: %+v", p.Recommendations)
		}
	}
}

func TestBuild_percentFreeCalculation(t *testing.T) {
	p := Build(Inputs{FreeBytes: 25, TotalBytes: 100})
	if p.Summary.PercentFree != 25.0 {
		t.Errorf("percentFree = %v want 25.0", p.Summary.PercentFree)
	}
}

func TestBuild_isVendedInResultsUsesLatest(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)
	in := Inputs{
		Apps: []apps.App{
			{BundleID: "com.foo.app", DynamicBytes: 600 * 1024 * 1024},
		},
		ProbeResults: []apps.ProbeResult{
			{BundleID: "com.foo.app", Outcome: apps.ProbeVended, At: earlier},
			{BundleID: "com.foo.app", Outcome: apps.ProbeRefused, At: now},
		},
	}
	p := Build(in)
	// Latest is refused -> should NOT be sandbox-cleanable, should
	// fall through to offload.
	if len(p.Recommendations) != 1 {
		t.Fatalf("expected 1 rec, got %d", len(p.Recommendations))
	}
	if p.Recommendations[0].Action != ActionOffloadApp {
		t.Errorf("latest probe was refused; expected offload action, got %q",
			p.Recommendations[0].Action)
	}
}

func TestBuild_displayNameFallsBackToBundleID(t *testing.T) {
	in := Inputs{
		Apps: []apps.App{
			{BundleID: "com.nameless.app", Name: "", DynamicBytes: 800 * 1024 * 1024},
		},
	}
	p := Build(in)
	if len(p.Recommendations) != 1 {
		t.Fatalf("expected 1 rec, got %d", len(p.Recommendations))
	}
	if !strings.Contains(p.Recommendations[0].Rationale, "com.nameless.app") {
		t.Errorf("rationale should fall back to bundleID when name empty: %q",
			p.Recommendations[0].Rationale)
	}
}
