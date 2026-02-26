package main

import (
	"testing"
	"time"
)

// ─────────────────────────── parseDelayMs ────────────────────────────────────

func TestParseDelayMs(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0", 0},
		{"", 0},
		{"6.73ms", 6.73},
		{"1.49ms", 1.49},
		{"123us", 0.123},
		{"500us", 0.5},
		{"1s", 1000.0},
		{"0.5s", 500.0},
		{"0ms", 0},
		{"  6.73ms  ", 6.73}, // leading/trailing spaces
	}
	for _, c := range cases {
		got := parseDelayMs(c.in)
		if got != c.want {
			t.Errorf("parseDelayMs(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

// ─────────────────────────── HistoryStore ────────────────────────────────────

func TestHistoryStore_BasicDelta(t *testing.T) {
	hs := NewHistoryStore(10)

	// First poll — seeds state, no sample stored yet.
	stats1 := []CakeStats{{
		Interface: "eth0",
		SentBytes: 1_000_000,
		Dropped:   0,
		Tiers:     []CakeTier{{AvDelay: "5ms", PkDelay: "10ms"}},
	}}
	hs.Record(stats1, time.Second)

	snap := hs.Snapshot()
	if _, ok := snap["eth0"]; ok {
		t.Fatalf("expected no sample after first poll, got %v", snap["eth0"])
	}

	// Second poll — a delta should now be recorded.
	stats2 := []CakeStats{{
		Interface: "eth0",
		SentBytes: 2_000_000, // +1 MB
		Dropped:   2,
		Tiers:     []CakeTier{{AvDelay: "6ms", PkDelay: "12ms"}},
	}}
	// Sleep a tiny bit so elapsed > 0
	time.Sleep(10 * time.Millisecond)
	hs.Record(stats2, time.Second)

	// Computed fields should now be nonzero.
	if stats2[0].TxBytesPerS <= 0 {
		t.Errorf("TxBytesPerS should be > 0, got %v", stats2[0].TxBytesPerS)
	}
	if stats2[0].DropsPerS <= 0 {
		t.Errorf("DropsPerS should be > 0, got %v", stats2[0].DropsPerS)
	}
	if stats2[0].MaxAvDelayMs != 6.0 {
		t.Errorf("MaxAvDelayMs want 6.0, got %v", stats2[0].MaxAvDelayMs)
	}
	if stats2[0].MaxPkDelayMs != 12.0 {
		t.Errorf("MaxPkDelayMs want 12.0, got %v", stats2[0].MaxPkDelayMs)
	}

	snap2 := hs.Snapshot()
	if len(snap2["eth0"]) != 1 {
		t.Fatalf("expected 1 sample after second poll, got %d", len(snap2["eth0"]))
	}
}

func TestHistoryStore_CounterReset(t *testing.T) {
	hs := NewHistoryStore(10)

	stats1 := []CakeStats{{Interface: "eth0", SentBytes: 9_000_000, Dropped: 100}}
	hs.Record(stats1, time.Second)
	time.Sleep(10 * time.Millisecond)

	// Simulate a counter reset (new value < previous).
	stats2 := []CakeStats{{Interface: "eth0", SentBytes: 100, Dropped: 5}}
	hs.Record(stats2, time.Second)

	// Delta must not go negative — should be clamped to 0.
	if stats2[0].TxBytesPerS < 0 {
		t.Errorf("TxBytesPerS must be >= 0 after counter reset, got %v", stats2[0].TxBytesPerS)
	}
	if stats2[0].DropsPerS < 0 {
		t.Errorf("DropsPerS must be >= 0 after counter reset, got %v", stats2[0].DropsPerS)
	}
}

func TestHistoryStore_RingBufferWraps(t *testing.T) {
	cap := 5
	hs := NewHistoryStore(cap)

	// Seed with first poll (no sample).
	stats := []CakeStats{{Interface: "eth0", SentBytes: 0}}
	hs.Record(stats, time.Second)

	// Push cap+2 more polls — ring should contain exactly cap samples.
	for i := 1; i <= cap+2; i++ {
		time.Sleep(2 * time.Millisecond)
		stats = []CakeStats{{Interface: "eth0", SentBytes: uint64(i) * 1000}}
		hs.Record(stats, time.Second)
	}

	snap := hs.Snapshot()
	got := len(snap["eth0"])
	if got != cap {
		t.Errorf("expected ring buffer to contain exactly %d samples, got %d", cap, got)
	}
}

func TestHistoryStore_PrunesRemovedIface(t *testing.T) {
	hs := NewHistoryStore(10)

	// Two interfaces on first poll.
	stats := []CakeStats{
		{Interface: "eth0", SentBytes: 0},
		{Interface: "eth1", SentBytes: 0},
	}
	hs.Record(stats, time.Second)
	time.Sleep(10 * time.Millisecond)

	// Second poll — only one interface remains.
	stats2 := []CakeStats{{Interface: "eth0", SentBytes: 1000}}
	hs.Record(stats2, time.Second)

	snap := hs.Snapshot()
	if _, ok := snap["eth1"]; ok {
		t.Error("expected eth1 to be pruned from history after it disappears")
	}
}
