// Package util is intentionally empty via this file.
// The split.go file previously held utility wrappers (Fields, Split, SplitN,
// SplitLines) that have been replaced by the zero-allocation FieldTokenizer
// and LineScanner in scan.go.  No code in the project calls these wrappers
// anymore, so they have been removed.
package util
