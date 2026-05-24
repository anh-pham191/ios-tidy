package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/anh-pham191/ios-tidy/internal/device"
)

// TestOpenAppStorageSettings_returnsManualRequired pins the
// investigation outcome: go-ios v1.0.213 has no URL-opening capability,
// so the handler always emits action="manual-required" along with the
// hard-coded prefs:root URL and step-by-step instructions.
func TestOpenAppStorageSettings_returnsManualRequired(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{
			{UDID: "U1", Name: "iPhone One"},
		}},
		Apps: &apps.FakeLister{Apps: []apps.App{
			{BundleID: "com.burbn.instagram", Name: "Instagram"},
		}},
	}
	h := newOpenAppStorageSettingsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.burbn.instagram",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error result: %s", extractText(res))
	}
	var out openAppStorageSettingsResult
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, extractText(res))
	}
	if out.Action != "manual-required" {
		t.Errorf("action = %q want manual-required", out.Action)
	}
	if out.URL != "prefs:root=STORAGE_MGMT_USAGE/com.burbn.instagram" {
		t.Errorf("url wrong: %q", out.URL)
	}
	if out.BundleID != "com.burbn.instagram" {
		t.Errorf("bundleID echo wrong: %q", out.BundleID)
	}
	if out.AppName != "Instagram" {
		t.Errorf("expected app name resolved from apps list, got: %q", out.AppName)
	}
	if !strings.Contains(out.Instructions, "Settings") || !strings.Contains(out.Instructions, "iPhone Storage") {
		t.Errorf("instructions missing key steps: %q", out.Instructions)
	}
	if out.Device.UDID != "U1" || out.Device.Name != "iPhone One" {
		t.Errorf("device echo wrong: %+v", out.Device)
	}
}

func TestOpenAppStorageSettings_requiresBundleID(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:   &apps.FakeLister{},
	}
	h := newOpenAppStorageSettingsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(nil))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("missing bundle_id should error, got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "bundle_id is required") {
		t.Errorf("expected 'bundle_id is required', got: %s", extractText(res))
	}
}

func TestOpenAppStorageSettings_rejectsAppleSystemBundle(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:   &apps.FakeLister{},
	}
	h := newOpenAppStorageSettingsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.apple.mobilesafari",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("apple bundle should be rejected, got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "system bundle") {
		t.Errorf("expected 'system bundle' rejection, got: %s", extractText(res))
	}
}

func TestOpenAppStorageSettings_rejectsHomoglyph(t *testing.T) {
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:   &apps.FakeLister{},
	}
	h := newOpenAppStorageSettingsHandler(deps)
	// "com.foo.cаt" — Cyrillic 'а' (U+0430) for 'a'.
	cyrillicA := "com.foo.cаt"
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": cyrillicA,
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !resultIsError(res) {
		t.Fatalf("homoglyph bundle should be rejected, got: %s", extractText(res))
	}
	if !strings.Contains(extractText(res), "non-printable-ASCII") {
		t.Errorf("expected non-printable-ASCII rejection, got: %s", extractText(res))
	}
}

func TestOpenAppStorageSettings_urlFormatPinned(t *testing.T) {
	// Defense in depth: confirm the exact URL format is what we expect,
	// regardless of bundle ID content (which is sanitised upstream).
	if got := storageMgmtURL("com.example.test"); got != "prefs:root=STORAGE_MGMT_USAGE/com.example.test" {
		t.Errorf("storageMgmtURL format drifted: %q", got)
	}
}

func TestOpenAppStorageSettings_unknownBundleStillEmitsURL(t *testing.T) {
	// App not in the lister's list. The handler should still emit the
	// URL — the user might have a typo, but the hard-coded URL is the
	// data they need; we just can't auto-name the app.
	deps := serverDeps{
		Lister: &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}},
		Apps:   &apps.FakeLister{Apps: nil},
	}
	h := newOpenAppStorageSettingsHandler(deps)
	res, err := h(context.Background(), callToolRequestWithArgs(map[string]any{
		"bundle_id": "com.unknown.app",
	}))
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if resultIsError(res) {
		t.Fatalf("unexpected error: %s", extractText(res))
	}
	var out openAppStorageSettingsResult
	if err := json.Unmarshal([]byte(extractText(res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.URL != "prefs:root=STORAGE_MGMT_USAGE/com.unknown.app" {
		t.Errorf("URL wrong: %q", out.URL)
	}
	if out.AppName != "" {
		t.Errorf("unknown bundle should not invent appName: %q", out.AppName)
	}
}
