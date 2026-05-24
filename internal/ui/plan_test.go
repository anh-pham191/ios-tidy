package ui

import (
	"bytes"
	"strings"
	"testing"
)

func TestAction_carriesPathAndSize(t *testing.T) {
	a := Action{Path: "/var/mobile/Library/Logs/CrashReporter/Foo.ips", Size: 4096}
	if a.Path != "/var/mobile/Library/Logs/CrashReporter/Foo.ips" {
		t.Fatalf("Path = %q, want %q", a.Path, "/var/mobile/Library/Logs/CrashReporter/Foo.ips")
	}
	if a.Size != 4096 {
		t.Fatalf("Size = %d, want %d", a.Size, 4096)
	}
}

func TestRenderPlan_emptyListPrintsHeaderAndZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	total := RenderPlan(&buf, "delete crash logs on ABC123", nil)
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	got := buf.String()
	if !strings.Contains(got, "Plan: delete crash logs on ABC123") {
		t.Fatalf("missing header line; got:\n%s", got)
	}
	if !strings.Contains(got, "Total: 0 files, 0 B") {
		t.Fatalf("missing zero-total footer; got:\n%s", got)
	}
}

func TestRenderPlan_writesActionRowsAndReturnsTotal(t *testing.T) {
	cases := []struct {
		name       string
		title      string
		actions    []Action
		wantTotal  int64
		wantInBody []string
	}{
		{
			name:      "single entry",
			title:     "delete crash logs on ABC123",
			actions:   []Action{{Path: "/a.ips", Size: 1024}},
			wantTotal: 1024,
			wantInBody: []string{
				"Plan: delete crash logs on ABC123",
				"/a.ips",
				"1.0 KB",
				"Total: 1 files, 1.0 KB",
			},
		},
		{
			name:  "mixed sizes including zero",
			title: "delete crash logs on XYZ",
			actions: []Action{
				{Path: "/a.ips", Size: 0},
				{Path: "/b.ips", Size: 512},
				{Path: "/c.ips", Size: 2 * 1000 * 1000},
			},
			wantTotal: 0 + 512 + 2*1000*1000,
			wantInBody: []string{
				"/a.ips",
				"/b.ips",
				"/c.ips",
				"0 B",
				"512 B",
				"2.0 MB",
				"Total: 3 files, 2.0 MB",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			got := RenderPlan(&buf, tc.title, tc.actions)
			if got != tc.wantTotal {
				t.Fatalf("total = %d, want %d", got, tc.wantTotal)
			}
			body := buf.String()
			for _, frag := range tc.wantInBody {
				if !strings.Contains(body, frag) {
					t.Errorf("body missing %q\nbody:\n%s", frag, body)
				}
			}
		})
	}
}
