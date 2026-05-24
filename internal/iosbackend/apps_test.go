// internal/iosbackend/apps_test.go
package iosbackend

import (
	"testing"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
)

// Compile-time interface conformance assertions. Mirrors the M1 device adapter
// convention in device_test.go: any constructor-signature drift surfaces at
// `go test` time rather than at first user run.
func TestNewApps_returnsListerAndUninstallerInterfaces(t *testing.T) {
	lister, uninstaller := NewApps()
	var _ apps.Lister = lister
	var _ apps.Uninstaller = uninstaller
}

func TestAsUint64(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want uint64
	}{
		{"nil", nil, 0},
		{"int positive", int(42), 42},
		{"int negative clamped to zero", int(-7), 0},
		{"int64 positive", int64(123), 123},
		{"int64 negative clamped to zero", int64(-1), 0},
		{"uint64 passthrough", uint64(456), 456},
		{"float64 truncated", float64(789.9), 789},
		{"float64 negative clamped to zero", float64(-1.5), 0},
		{"string of digits parsed", "1234", 1234},
		{"string with junk yields zero", "abc", 0},
		{"empty string yields zero", "", 0},
		{"bool yields zero", true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := asUint64(c.in); got != c.want {
				t.Fatalf("asUint64(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestMapAppInfo(t *testing.T) {
	cases := []struct {
		name string
		in   installationproxy.AppInfo
		want struct {
			BundleID           string
			Name               string
			Version            string
			Container          string
			DynamicBytes       uint64
			StaticBytes        uint64
			FileSharingEnabled bool
			ApplicationType    string
		}
	}{
		{
			name: "all fields present, name from CFBundleName",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier":         "com.example.full",
				"CFBundleName":               "FullName",
				"CFBundleDisplayName":        "ShouldNotUse",
				"CFBundleShortVersionString": "1.2.3",
				"CFBundleVersion":            "123",
				"Container":                  "/var/mobile/Containers/Data/Application/UUID",
				"DynamicDiskUsage":           uint64(1_000),
				"StaticDiskUsage":            uint64(2_000),
				"UIFileSharingEnabled":       true,
				"ApplicationType":            "User",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{
				BundleID: "com.example.full", Name: "FullName", Version: "1.2.3",
				Container:    "/var/mobile/Containers/Data/Application/UUID",
				DynamicBytes: 1_000, StaticBytes: 2_000,
				FileSharingEnabled: true, ApplicationType: "User",
			},
		},
		{
			name: "name falls back to CFBundleDisplayName when CFBundleName missing",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier":  "com.example.fallback",
				"CFBundleDisplayName": "DisplayOnly",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.fallback", Name: "DisplayOnly"},
		},
		{
			name: "name falls back to CFBundleDisplayName when CFBundleName is empty string",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier":  "com.example.empty",
				"CFBundleName":        "",
				"CFBundleDisplayName": "DisplayOnly",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.empty", Name: "DisplayOnly"},
		},
		{
			name: "version falls back to CFBundleVersion when short version absent",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.ver",
				"CFBundleVersion":    "42",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.ver", Version: "42"},
		},
		{
			name: "file sharing false when key absent",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.share",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.share", FileSharingEnabled: false},
		},
		{
			name: "dynamic disk usage arriving as int64",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.i64",
				"DynamicDiskUsage":   int64(7_777),
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.i64", DynamicBytes: 7_777},
		},
		{
			name: "dynamic disk usage arriving as float64 truncates",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.f64",
				"DynamicDiskUsage":   float64(8_888.9),
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.f64", DynamicBytes: 8_888},
		},
		{
			name: "static disk usage arriving as string parses",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.str",
				"StaticDiskUsage":    "9999",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.str", StaticBytes: 9_999},
		},
		{
			name: "missing container yields empty string",
			in: installationproxy.AppInfo{
				"CFBundleIdentifier": "com.example.nocontainer",
			},
			want: struct {
				BundleID           string
				Name               string
				Version            string
				Container          string
				DynamicBytes       uint64
				StaticBytes        uint64
				FileSharingEnabled bool
				ApplicationType    string
			}{BundleID: "com.example.nocontainer", Container: ""},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapAppInfo(c.in)
			if got.BundleID != c.want.BundleID {
				t.Fatalf("BundleID = %q, want %q", got.BundleID, c.want.BundleID)
			}
			if got.Name != c.want.Name {
				t.Fatalf("Name = %q, want %q", got.Name, c.want.Name)
			}
			if got.Version != c.want.Version {
				t.Fatalf("Version = %q, want %q", got.Version, c.want.Version)
			}
			if got.Container != c.want.Container {
				t.Fatalf("Container = %q, want %q", got.Container, c.want.Container)
			}
			if got.DynamicBytes != c.want.DynamicBytes {
				t.Fatalf("DynamicBytes = %d, want %d", got.DynamicBytes, c.want.DynamicBytes)
			}
			if got.StaticBytes != c.want.StaticBytes {
				t.Fatalf("StaticBytes = %d, want %d", got.StaticBytes, c.want.StaticBytes)
			}
			if got.FileSharingEnabled != c.want.FileSharingEnabled {
				t.Fatalf("FileSharingEnabled = %v, want %v", got.FileSharingEnabled, c.want.FileSharingEnabled)
			}
			if got.ApplicationType != c.want.ApplicationType {
				t.Fatalf("ApplicationType = %q, want %q", got.ApplicationType, c.want.ApplicationType)
			}
		})
	}
}
