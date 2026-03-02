package util

import "strings"

// Fields splits s around runs of whitespace and returns a slice of substrings,
// identical to strings.Fields.
//
// Centralised here so that all whitespace-splitting in the hot parsing path
// flows through one place, enabling a future replacement with a lower-
// allocation implementation (e.g. iterator-style or arena-backed) without
// touching any call site.
func Fields(s string) []string { return strings.Fields(s) }

// Split slices s into all substrings separated by sep and returns a slice of
// the substrings between those separators.  Delegates to strings.Split.
func Split(s, sep string) []string { return strings.Split(s, sep) }

// SplitN slices s into at most n substrings separated by sep.
// Delegates to strings.SplitN.
func SplitN(s, sep string, n int) []string { return strings.SplitN(s, sep, n) }

// SplitLines splits s on newline boundaries and trims a single trailing empty
// element that strings.Split would produce when s ends with '\n'.  This avoids
// an off-by-one when iterating over the output of commands like `tc -s qdisc`.
func SplitLines(s string) []string {
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
