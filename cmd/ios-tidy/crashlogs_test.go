package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anh-pham191/ios-tidy/internal/crashlogs"
	"github.com/anh-pham191/ios-tidy/internal/device"
	"github.com/anh-pham191/ios-tidy/internal/ui"
)

func TestCrashLogsList_tableOutput_filtersByPatternAndPrintsBytes(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1", Name: "iPhone"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/Chrome-1.ips", Size: 1024, ModTime: time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)},
			{Path: "/Mail.crash", Size: 2048},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--pattern", "Chrome-*"}, &stdout, &stderr)

	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "/Chrome-1.ips") {
		t.Fatalf("stdout missing /Chrome-1.ips: %q", out)
	}
	if strings.Contains(out, "/Mail.crash") {
		t.Fatalf("stdout should not include /Mail.crash (filtered out): %q", out)
	}
	if !strings.Contains(out, "1.0 KB") && !strings.Contains(out, "1 KB") {
		t.Fatalf("stdout should contain a human-readable size: %q", out)
	}
	// Pattern was applied client-side; the fake recorded the raw flag value.
	if len(cc.ListCalls) != 1 || cc.ListCalls[0].Pattern != "Chrome-*" {
		t.Fatalf("ListCalls = %+v, want one with Pattern=Chrome-*", cc.ListCalls)
	}
}

func TestCrashLogsList_jsonOutput_isCleanJSON(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{
		ListEntries: []crashlogs.Entry{
			{Path: "/A.ips", Size: 10, ModTime: time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)},
		},
	}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{"--json"}, &stdout, &stderr)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", exit, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"path": "/A.ips"`) {
		t.Fatalf("stdout missing path JSON: %q", out)
	}
	if !strings.Contains(out, `"size": 10`) {
		t.Fatalf("stdout missing size JSON: %q", out)
	}
	// stderr should NOT contain the prompt or table chrome.
	if stderr.Len() != 0 {
		t.Fatalf("stderr should be empty for clean JSON path, got %q", stderr.String())
	}
}

func TestCrashLogsList_errorsWhenMultipleDevicesAndNoFlag(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{
		{UDID: "U1", Name: "Phone1"},
		{UDID: "U2", Name: "Phone2"},
	}}
	cc := &crashlogs.FakeClient{}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero")
	}
	if !strings.Contains(stderr.String(), "U1") || !strings.Contains(stderr.String(), "U2") {
		t.Fatalf("stderr should list both UDIDs: %q", stderr.String())
	}
	if len(cc.ListCalls) != 0 {
		t.Fatalf("client.List should not be called when selection fails")
	}
}

func TestCrashLogsList_propagatesClientError(t *testing.T) {
	dl := &device.FakeLister{Devices: []device.Device{{UDID: "U1"}}}
	cc := &crashlogs.FakeClient{ListErr: errors.New("boom")}
	deps := crashLogsDeps{Lister: dl, Client: cc, Prompter: ui.NewFakePrompter(nil)}

	var stdout, stderr bytes.Buffer
	exit := runCrashLogsList(context.Background(), deps, []string{}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr should contain client error: %q", stderr.String())
	}
}
