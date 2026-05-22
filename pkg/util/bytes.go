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


