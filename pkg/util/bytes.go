// Package util provides shared, allocation-minimising string and byte-
// conversion helpers used across multiple packages in cake-stats.
//
// Design constraints:
//   - Every exported function is a pure transform (no package-level mutable
//     state) and is therefore safe for concurrent use without synchronisation.
//   - Zero-allocation helpers here use unsafe.String / unsafe.Slice
//     (Go 1.20+).  Read the safety contracts on each function before use.
package util

import "unsafe"

// BytesToString converts a byte slice to a string without a heap copy.
//
// Safety contract: the returned string MUST NOT be used after the source slice
// is modified.  The GC keeps the backing array alive as long as strings derived
// from it are reachable, so the risk is mutation, not premature collection.
//
// Intended use: passing exec.Command.Output() bytes to a parser that holds no
// string references past its own call stack (e.g. CollectStats in the parser
// package).
func BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

// StringToBytes returns the backing memory of s as a byte slice without
// allocation.  The caller MUST NOT write to the returned slice; doing so
// violates Go's string immutability guarantee and causes undefined behaviour.
//
// Intended use: passing a string literal or an already-owned string to a
// write-only sink (hash, length check, etc.) where a copy is not needed.
func StringToBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
