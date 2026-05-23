// internal/ui/bytes_test.go
package ui

import (
	"math"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		name string
		in   uint64
		want string
	}{
		{"zero", 0, "0 B"},
		{"one byte", 1, "1 B"},
		{"just below kilobyte", 999, "999 B"},
		{"exact kilobyte SI", 1000, "1.0 KB"},
		// 1024 / 1000 = 1.024 → "1.0 KB" with %.1f rounding. The test name flags
		// that SI (1000) is intentional, not IEC (1024). See bytes.go header.
		{"binary kilobyte stays kilobyte under SI rounding", 1024, "1.0 KB"},
		{"just below megabyte", 999_999, "1000.0 KB"},
		{"exact megabyte SI", 1_000_000, "1.0 MB"},
		{"about 1.23 GB", 1_234_567_890, "1.2 GB"},
		{"about 1.23 TB", 1_234_567_890_123, "1.2 TB"},
		{"max uint64", math.MaxUint64, "18.4 EB"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatBytes(c.in)
			if got != c.want {
				t.Fatalf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
