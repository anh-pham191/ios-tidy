// internal/ui/bytes.go
package ui

import "fmt"

// FormatBytes renders a byte count using SI (1000-based) prefixes — B, KB, MB,
// GB, TB, PB, EB. We deliberately use SI rather than IEC (1024-based) so the
// output matches macOS Finder, which has used SI since OS X 10.6. Values below
// 1 KB are rendered as whole bytes with no decimal point. Values 1 KB and
// above are rendered with one decimal place. The largest representable value
// (math.MaxUint64 ≈ 18.45 EB) fits in EB without overflow.
//
// SI vs IEC: 1 KB = 1000 B (Finder/Settings), 1 KiB = 1024 B (RAM, kernel
// tooling). We use SI everywhere so the storage subcommand's header line is
// comparable with what the user sees in iOS Settings → General → iPhone
// Storage.
func FormatBytes(b uint64) string {
	const unit uint64 = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := unit, 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	suffixes := []string{"KB", "MB", "GB", "TB", "PB", "EB"}
	// exp is clamped to len(suffixes)-1 by the uint64 range — math.MaxUint64
	// fits in EB. No need for a guard.
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), suffixes[exp])
}
