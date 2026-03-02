package util

import "strings"

// TrimSpace returns s without leading and trailing Unicode whitespace.
// Delegates to strings.TrimSpace; centralised here so callers import only
// this package for string utilities and to allow future SIMD-backed swaps.
func TrimSpace(s string) string { return strings.TrimSpace(s) }

// TrimPrefix returns s without the leading prefix p.
// If s does not start with p, s is returned unchanged.
func TrimPrefix(s, prefix string) string { return strings.TrimPrefix(s, prefix) }

// TrimSuffix returns s without the trailing suffix.
// If s does not end with suffix, s is returned unchanged.
func TrimSuffix(s, suffix string) string { return strings.TrimSuffix(s, suffix) }

// TrimRight returns s with all trailing characters in cutset removed.
func TrimRight(s, cutset string) string { return strings.TrimRight(s, cutset) }

// AfterColon returns the whitespace-trimmed substring that follows the first
// ':' in s.  Returns "" when no ':' is present or when the remainder is
// entirely whitespace.
//
// This pattern is extremely common in tc output lines, for example:
//
//	"capacity estimate: 100Mbit"  →  "100Mbit"
//	"average network hdr offset:  14"  →  "14"
//	"memory used: 14Kb of 4Mb"  →  "14Kb of 4Mb"
func AfterColon(s string) string {
	i := strings.Index(s, ":")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(s[i+1:])
}

// AfterSlash returns the whitespace-trimmed substring that follows the first
// '/' in s.  Returns "" when no '/' is present.
//
// Used to extract the high half of "min/max" range lines such as
// "min/max network layer size:  28 / 1500".
func AfterSlash(s string) string {
	i := strings.Index(s, "/")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(s[i+1:])
}
