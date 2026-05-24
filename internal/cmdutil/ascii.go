// internal/cmdutil/ascii.go
//
// Shared printable-ASCII gate for bundle-ID arguments. Lifted from
// cmd/ios-tidy-mcp/tools.go (where it originally lived as
// isPrintableASCII / firstNonASCIIRune) so the CLI binary and the
// MCP server cannot drift on the homograph defence.
//
// Apple bundle IDs are reverse-DNS: ASCII letters, digits, '-' and '.'.
// Anything outside the printable-ASCII range [0x20, 0x7E] is either a
// typo or an injection attempt (Cyrillic 'а' U+0430 renders identically
// to ASCII 'a' but is byte-different; embedded NULs / RTL overrides /
// smart quotes are all rejected here). Both surfaces (CLI and MCP)
// share the same on-disk probe cache, so a homoglyph entry from one
// surface would poison the other — refusing at parse time keeps the
// cache ASCII-clean by construction.

package cmdutil

// IsPrintableASCII reports whether every rune in s is in the printable
// ASCII range [0x20, 0x7E].
func IsPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return false
		}
	}
	return true
}

// FirstNonASCIIRune returns the first non-printable-ASCII rune in s,
// suitable for embedding in an error message with %U so the caller (and
// auditor) sees exactly which codepoint triggered the refusal. Returns
// 0 when s is entirely printable ASCII.
func FirstNonASCIIRune(s string) rune {
	for _, r := range s {
		if r < 0x20 || r > 0x7E {
			return r
		}
	}
	return 0
}
