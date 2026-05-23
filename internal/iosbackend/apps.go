// internal/iosbackend/apps.go
//
// This file will be the ONLY M2 adapter that imports go-ios's installationproxy
// package; the rest of the codebase depends on internal/apps interfaces.
// The asUint64 helper below isolates plist-decoder type variability — see
// installationproxy.AppInfo (map[string]any) where DynamicDiskUsage and
// StaticDiskUsage can arrive as int64, uint64, float64, or string depending
// on the go-ios version.

package iosbackend

import "strconv"

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
