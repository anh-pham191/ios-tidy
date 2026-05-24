package cmdutil

import "testing"

func TestIsPrintableASCII(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", true},
		{"ascii reverse-DNS", "com.example.app", true},
		{"ascii with digits and dash", "com.example.app-2", true},
		{"cyrillic homoglyph", "com.exаmple.app", false}, // U+0430
		{"NUL byte", "com.example\x00", false},
		{"tab", "com.example\t", false},
		{"DEL", "com.example\x7f", false},
		{"unicode RTL override", "com.example‮.app", false},
		{"smart quote", "com.“example”.app", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPrintableASCII(tc.in); got != tc.want {
				t.Errorf("IsPrintableASCII(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFirstNonASCIIRune(t *testing.T) {
	if got := FirstNonASCIIRune("com.example.app"); got != 0 {
		t.Errorf("clean ASCII: want 0, got %U", got)
	}
	if got := FirstNonASCIIRune("com.exаmple.app"); got != 'а' {
		t.Errorf("cyrillic: want U+0430, got %U", got)
	}
	if got := FirstNonASCIIRune("com.example\x00"); got != 0x00 {
		t.Errorf("NUL: want 0x00, got %U", got)
	}
}
