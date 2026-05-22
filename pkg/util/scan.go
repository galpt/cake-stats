package util

import "unsafe"

// LineScanner iterates over the lines in a byte slice without allocating
// any strings or slices.  Each call to Next() advances to the next line;
// Line() returns the current line as a string (backed by the original data).
//
// Safety: the returned string is valid only while the original data slice
// passed to Init() remains alive and unmodified.  Do not retain the string
// past the lifetime of the data.
type LineScanner struct {
	data []byte
	pos  int
	line []byte
}

// Init prepares the scanner to iterate over data.
func (s *LineScanner) Init(data []byte) {
	s.data = data
	s.pos = 0
	s.line = nil
}

// Next advances to the next line and returns true, or false if there are
// no more lines.
func (s *LineScanner) Next() bool {
	if s.pos >= len(s.data) {
		s.line = nil
		return false
	}
	end := s.pos
	for end < len(s.data) && s.data[end] != '\n' {
		end++
	}
	s.line = s.data[s.pos:end]
	// Skip past the newline (or past the end).
	s.pos = end + 1
	return true
}

// Line returns the current line as a string.  The result is a sub-string
// of the original data — it does not allocate.
func (s *LineScanner) Line() string {
	if len(s.line) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(s.line), len(s.line))
}

// Bytes returns the current line as a byte slice that shares the backing
// array of the original data.  Equivalent to Line() but avoids the
// unsafe.String conversion.
func (s *LineScanner) Bytes() []byte {
	return s.line
}

// FieldTokenizer splits a string into fields without heap-allocating a
// new []string.  It reuses an internal fixed-size buffer.
//
// Thread-unsafe — each goroutine must use its own FieldTokenizer.
type FieldTokenizer struct {
	buf [64]string
}

// Tokenise splits s into fields and returns a sub-slice of the internal
// buffer.  Each field is a sub-string of s (no allocation).
//
// The returned slice is valid until the next call to Tokenise.
func (t *FieldTokenizer) Tokenise(s string) []string {
	n := 0
	i := 0
	for i < len(s) {
		// Skip whitespace.
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		for i < len(s) && s[i] != ' ' && s[i] != '\t' {
			i++
		}
		if n < len(t.buf) {
			t.buf[n] = s[start:i]
			n++
		}
	}
	return t.buf[:n]
}
