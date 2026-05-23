// internal/iosbackend/apps_test.go
package iosbackend

import "testing"

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
