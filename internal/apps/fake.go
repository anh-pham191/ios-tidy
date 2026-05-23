// internal/apps/fake.go
package apps

import "context"

// FakeLister is the hand-written fake for Lister. Exported per SHARED_CONTEXT.md §5
// so cmd/ios-tidy tests can reuse it.
type FakeLister struct {
	Apps  []App
	Err   error
	Calls []string
}

func (f *FakeLister) UserApps(_ context.Context, udid string) ([]App, error) {
	f.Calls = append(f.Calls, udid)
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Apps, nil
}

// UninstallCall records a single Uninstall invocation for assertion in tests.
type UninstallCall struct {
	UDID     string
	BundleID string
}

// FakeUninstaller records every Uninstall and returns Err if set.
type FakeUninstaller struct {
	Err   error
	Calls []UninstallCall
}

func (f *FakeUninstaller) Uninstall(_ context.Context, udid string, bundleID string) error {
	f.Calls = append(f.Calls, UninstallCall{UDID: udid, BundleID: bundleID})
	return f.Err
}
