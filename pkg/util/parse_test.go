package util

import "testing"

func TestParseUint64(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint64
	}{
		{"0", 0}, {"42", 42}, {"42p", 42}, {"1024b", 1024}, {"abc", 0}, {"", 0},
	} {
		if got := ParseUint64(tc.in); got != tc.want {
			t.Errorf("ParseUint64(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseBytesStr(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want uint64
	}{
		{"0b", 0}, {"512b", 512}, {"1Kb", 1024}, {"2Mb", 2097152}, {"1Gb", 1073741824},
	} {
		if got := ParseBytesStr(tc.in); got != tc.want {
			t.Errorf("ParseBytesStr(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseDelayMs(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{"", 0}, {"0", 0}, {"500us", 0.5}, {"1ms", 1}, {"100ms", 100}, {"2s", 2000},
	} {
		if got := ParseDelayMs(tc.in); got != tc.want {
			t.Errorf("ParseDelayMs(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestBytesToString(t *testing.T) {
	if s := BytesToString([]byte("hi")); s != "hi" {
		t.Errorf("BytesToString=%q want hi", s)
	}
	if BytesToString(nil) != "" {
		t.Error("BytesToString(nil) must be empty")
	}
}

func TestAfterColon(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{" capacity estimate: 100Mbit", "100Mbit"}, {"no colon", ""},
	} {
		if got := AfterColon(tc.in); got != tc.want {
			t.Errorf("AfterColon(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
