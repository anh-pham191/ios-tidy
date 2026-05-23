// internal/apps/sort.go
package apps

import (
	"cmp"
	"slices"
)

// Sort orders apps by total disk usage (DynamicBytes + StaticBytes) descending,
// breaking ties on BundleID ascending so output is stable. In-place; returns
// no value to make the mutation contract obvious to callers. Pure given the
// slice header — does not allocate.
func Sort(apps []App) {
	slices.SortFunc(apps, func(a, b App) int {
		ta := a.DynamicBytes + a.StaticBytes
		tb := b.DynamicBytes + b.StaticBytes
		if ta != tb {
			// Descending: larger total first.
			if ta > tb {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.BundleID, b.BundleID)
	})
}

// Limit returns the first n apps from the slice. If n <= 0 or n >= len(apps),
// returns the slice unchanged (no copy). We treat non-positive n as "no limit"
// so callers can pass --limit 0 (or omit the flag) to mean "all apps" without
// branching at the call site.
func Limit(apps []App, n int) []App {
	if n <= 0 || n >= len(apps) {
		return apps
	}
	return apps[:n]
}
