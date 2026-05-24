// cmd/ios-tidy/device_resolution.go
//
// Thin aliases over internal/cmdutil. The real implementation moved to
// internal/cmdutil so cmd/ios-tidy-mcp can share the same zero/one/many
// device UX without duplicating the logic. Keeping the lowercase
// package-local names here lets the existing call sites stay untouched.
package main

import (
	"github.com/anh-pham191/ios-tidy/internal/cmdutil"
)

// errNoDevicesAttached is the package-local alias for
// cmdutil.ErrNoDevicesAttached so existing `errors.Is(err,
// errNoDevicesAttached)` checks keep compiling.
var errNoDevicesAttached = cmdutil.ErrNoDevicesAttached

// resolveDevice is the package-local alias for cmdutil.ResolveDevice.
// All existing callers (storage, crashlogs, apps subcommands) call
// through this name — keeping the alias avoids a sweeping rename in a
// pure-extraction commit.
var resolveDevice = cmdutil.ResolveDevice
