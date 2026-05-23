// internal/apps/apps.go

// Package apps defines the seam interfaces for enumerating, sorting, and
// uninstalling iOS user-installed applications. The only production
// implementations of Lister and Uninstaller live in internal/iosbackend/apps.go.
// Sort/Limit (in sort.go) are pure helpers shared across consumers.
package apps

import "context"

// App is the cross-package representation of a single installed application.
// JSON tags use camelCase for stable machine-readable output (see
// cmd/ios-tidy/storage.go for the --json contract).
type App struct {
	BundleID           string `json:"bundleId"`
	Name               string `json:"name"`
	Version            string `json:"version"`
	Container          string `json:"container"`
	DynamicBytes       uint64 `json:"dynamicBytes"`
	StaticBytes        uint64 `json:"staticBytes"`
	FileSharingEnabled bool   `json:"fileSharingEnabled"`
	ApplicationType    string `json:"applicationType"`
}

// Lister enumerates the apps a connected device reports via installation_proxy.
type Lister interface {
	UserApps(ctx context.Context, udid string) ([]App, error)
}

// Uninstaller removes a single app by bundle ID. Destructive.
type Uninstaller interface {
	Uninstall(ctx context.Context, udid string, bundleID string) error
}
