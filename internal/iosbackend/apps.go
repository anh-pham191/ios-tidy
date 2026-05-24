// internal/iosbackend/apps.go
//
// This file will be the ONLY M2 adapter that imports go-ios's installationproxy
// package; the rest of the codebase depends on internal/apps interfaces.
// The asUint64 helper below isolates plist-decoder type variability — see
// installationproxy.AppInfo (map[string]any) where DynamicDiskUsage and
// StaticDiskUsage can arrive as int64, uint64, float64, or string depending
// on the go-ios version.

package iosbackend

import (
	"context"
	"fmt"
	"strconv"

	"github.com/anh-pham191/ios-tidy/internal/apps"
	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
)

// asUint64 best-effort-converts any plist-decoded value to uint64. Negative
// signed numbers and non-numeric inputs return 0 — disk usage cannot be
// negative, and a missing/garbage key is functionally "unknown size".
func asUint64(v any) uint64 {
	switch x := v.(type) {
	case nil:
		return 0
	case uint64:
		return x
	case int:
		if x < 0 {
			return 0
		}
		return uint64(x)
	case int64:
		if x < 0 {
			return 0
		}
		return uint64(x)
	case float64:
		if x < 0 {
			return 0
		}
		return uint64(x)
	case string:
		n, err := strconv.ParseUint(x, 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// NewApps returns a production apps.Lister and apps.Uninstaller sharing one
// underlying go-ios installation-proxy adapter.
func NewApps() (apps.Lister, apps.Uninstaller) {
	a := &appsClient{}
	return a, a
}

type appsClient struct{}

func (a *appsClient) UserApps(ctx context.Context, udid string) ([]apps.App, error) {
	// Pre-flight ctx check; mid-flight cancellation of the proxy RPC is not
	// supported by go-ios on this path.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entry, err := ios.GetDevice(udid)
	if err != nil {
		return nil, fmt.Errorf("get device %q: %w", udid, err)
	}
	conn, err := installationproxy.New(entry)
	if err != nil {
		return nil, fmt.Errorf("open installation_proxy on %q: %w", udid, err)
	}
	// Connection.Close() has no return value — cannot be wrapped in if-err.
	defer conn.Close()

	raw, err := conn.BrowseUserApps()
	if err != nil {
		return nil, fmt.Errorf("browse user apps on %q: %w", udid, err)
	}
	out := make([]apps.App, 0, len(raw))
	for _, info := range raw {
		out = append(out, mapAppInfo(info))
	}
	return out, nil
}

func (a *appsClient) Uninstall(ctx context.Context, udid string, bundleID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entry, err := ios.GetDevice(udid)
	if err != nil {
		return fmt.Errorf("get device %q: %w", udid, err)
	}
	conn, err := installationproxy.New(entry)
	if err != nil {
		return fmt.Errorf("open installation_proxy on %q: %w", udid, err)
	}
	defer conn.Close()

	if err := conn.Uninstall(bundleID); err != nil {
		return fmt.Errorf("uninstall %q on %q: %w", bundleID, udid, err)
	}
	return nil
}

// mapAppInfo translates installationproxy.AppInfo (map[string]any) into apps.App,
// applying the documented key fallbacks. asUint64 (in this same file) handles
// plist-decoder type variability for numeric keys. Pure — no go-ios calls — so
// covered by TestMapAppInfo, not the device integration test.
func mapAppInfo(info installationproxy.AppInfo) apps.App {
	asString := func(key string) string {
		if v, ok := info[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	asBool := func(key string) bool {
		if v, ok := info[key]; ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
		return false
	}

	name := asString("CFBundleName")
	if name == "" {
		name = asString("CFBundleDisplayName")
	}
	version := asString("CFBundleShortVersionString")
	if version == "" {
		version = asString("CFBundleVersion")
	}
	return apps.App{
		BundleID:           asString("CFBundleIdentifier"),
		Name:               name,
		Version:            version,
		Container:          asString("Container"),
		DynamicBytes:       asUint64(info["DynamicDiskUsage"]),
		StaticBytes:        asUint64(info["StaticDiskUsage"]),
		FileSharingEnabled: asBool("UIFileSharingEnabled"),
		ApplicationType:    asString("ApplicationType"),
	}
}
