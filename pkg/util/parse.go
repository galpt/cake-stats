package util

import (
	"strconv"
	"strings"
)

// ParseUint64 converts a numeric string to uint64, stripping any trailing
// byte/packet unit suffix characters (b, B, k, K, m, M, g, G, p, P) that
// tc/iproute2 appends to counter values (e.g. "1024b", "42p", "5Mb").
// Returns 0 for empty or non-numeric input; never panics.
func ParseUint64(s string) uint64 {
	s = strings.TrimRight(s, "bBkKmMgGpP")
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// ParseBytesStr parses a tc-style byte-count string (e.g. "238656b", "4Kb",
// "32Mb", "1Gb") and returns the absolute byte count as uint64.
// Only the exact case-sensitive suffixes that iproute2 emits are recognised:
//
//	"Gb" → ×1 GiB,  "Mb" → ×1 MiB,  "Kb" → ×1 KiB,  "b" → ×1
//
// Returns 0 for empty or unrecognised input.
func ParseBytesStr(s string) uint64 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "Gb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Gb"), 10, 64)
		return v * 1024 * 1024 * 1024
	case strings.HasSuffix(s, "Mb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Mb"), 10, 64)
		return v * 1024 * 1024
	case strings.HasSuffix(s, "Kb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Kb"), 10, 64)
		return v * 1024
	default:
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "b"), 10, 64)
		return v
	}
}

// ParseDelayMs converts a tc delay string to float64 milliseconds.
//
// Recognised suffixes: "us" (microseconds), "ms" (milliseconds), "s" (seconds).
// The function handles decimal values such as "1.5ms".
// Returns 0 for empty input, the bare string "0", or unrecognised suffixes.
//
// This is the single canonical implementation; it replaces the duplicate
// cakeParseDelayUsec (parser package) and parseDelayMs (history package) that
// previously drifted apart.
func ParseDelayMs(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0
	}
	for _, sfx := range []string{"us", "ms", "s"} {
		if strings.HasSuffix(s, sfx) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(s, sfx), 64)
			if err != nil {
				return 0
			}
			switch sfx {
			case "us":
				return v / 1e3
			case "ms":
				return v
			case "s":
				return v * 1e3
			}
		}
	}
	return 0
}

// ParseDelayUsec is like ParseDelayMs but returns microseconds.
// Provided for callers that need usec precision (e.g. "worst-case delay"
// comparisons inside the parser's tier-aggregation path).
func ParseDelayUsec(s string) float64 {
	return ParseDelayMs(s) * 1e3
}
