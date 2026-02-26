package main

import (
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─────────────────────────── Data types ─────────────────────────────────────

// HistorySample is one time-series data point for a single CAKE interface.
// All numeric values are float64 so they can be directly consumed by charting
// libraries (uPlot, Chart.js, etc.).
type HistorySample struct {
	T  int64   `json:"t"`  // unix timestamp (seconds)
	Tx float64 `json:"tx"` // bytes transmitted per second (TX throughput)
	Av float64 `json:"av"` // max av_delay across all tiers (milliseconds)
	Pk float64 `json:"pk"` // max pk_delay across all tiers (milliseconds)
	Dr float64 `json:"dr"` // packet drops per second
}

// ─────────────────────────── Ring buffer internals ───────────────────────────

// ifaceState tracks per-interface counters and the ring buffer.
type ifaceState struct {
	prevTxBytes uint64 // sum of per-tier Bytes (or SentBytes when no tiers)
	prevDropped uint64
	prevTime    time.Time

	// Ring buffer — allocated once at capacity; never grows.
	samples []HistorySample
	head    int // index of the next write slot
	count   int // number of valid entries (0..capacity)
}

func newIfaceState(capacity int, cs *CakeStats) *ifaceState {
	return &ifaceState{
		prevTxBytes: txBytes(cs),
		prevDropped: cs.Dropped,
		prevTime:    time.Now(),
		samples:     make([]HistorySample, capacity),
	}
}

// txBytes returns the actual bytes transmitted for cs.
//
// We use cs.SentBytes (the top-level "Sent X bytes" counter from tc) because
// it reflects real IP-layer bytes on the wire.  CAKE's per-tier Bytes counters
// use ATM-overhead-adjusted sizes when "atm overhead N" is configured, which
// inflates the derived rate above the actual bandwidth limit and produces
// inaccurate graphs (e.g. showing throughput > 50 Mbit on a 50 Mbit link).
//
// The tc "Sent X bytes" line always outputs a plain integer on both mainline
// Linux and OpenWrt, so SI-prefix parsing is not a concern here.
func txBytes(cs *CakeStats) uint64 {
	return cs.SentBytes
}

// push appends a sample, overwriting the oldest entry when the buffer is full.
func (st *ifaceState) push(s HistorySample, capacity int) {
	st.samples[st.head] = s
	st.head = (st.head + 1) % capacity
	if st.count < capacity {
		st.count++
	}
}

// ordered returns a slice of samples in chronological order (oldest first).
func (st *ifaceState) ordered(capacity int) []HistorySample {
	if st.count == 0 {
		return nil
	}
	out := make([]HistorySample, st.count)
	if st.count < capacity {
		// Buffer is not yet full; data starts at index 0.
		copy(out, st.samples[:st.count])
	} else {
		// Full ring: oldest entry is at st.head.
		n := copy(out, st.samples[st.head:])
		copy(out[n:], st.samples[:st.head])
	}
	return out
}

// ─────────────────────────── HistoryStore ───────────────────────────────────

// HistoryStore is a thread-safe collection of per-interface ring buffers.
//
// Typical call sequence (from a single goroutine — the tc poller):
//
//	stats := ParseTCOutput(raw)
//	store.Record(stats, pollInterval) // fills computed delta fields in stats
//	// Now stats[i].TxBytesPerS, .DropsPerS, .MaxAvDelayMs, .MaxPkDelayMs are set.
type HistoryStore struct {
	mu       sync.RWMutex
	ifaces   map[string]*ifaceState
	capacity int
}

// NewHistoryStore allocates a HistoryStore whose per-interface ring buffers
// hold at most capacity samples.  For example, capacity=300 gives a 5-minute
// window at a 1-second poll interval.
func NewHistoryStore(capacity int) *HistoryStore {
	if capacity < 2 {
		capacity = 2
	}
	return &HistoryStore{
		ifaces:   make(map[string]*ifaceState),
		capacity: capacity,
	}
}

// Record computes per-interval delta metrics for each interface in stats,
// writes those values back into the CakeStats elements (TxBytesPerS,
// DropsPerS, MaxAvDelayMs, MaxPkDelayMs), and appends a HistorySample to
// each interface's ring buffer.
//
// Record is NOT goroutine-safe with respect to the caller; it must be called
// from exactly one goroutine (the poller).  HTTP handlers that read the store
// must use Snapshot().
func (hs *HistoryStore) Record(stats []CakeStats, interval time.Duration) {
	now := time.Now()

	hs.mu.Lock()
	defer hs.mu.Unlock()

	for i := range stats {
		cs := &stats[i]
		key := cs.Interface

		st, exists := hs.ifaces[key]
		if !exists {
			// First time we see this interface — seed state but don't record a
			// sample (we have no previous values to diff against).
			hs.ifaces[key] = newIfaceState(hs.capacity, cs)
			continue
		}

		// ── Compute elapsed time (use wall clock for accuracy) ───────────
		elapsed := now.Sub(st.prevTime).Seconds()
		if elapsed <= 0 {
			elapsed = interval.Seconds()
		}

		// ── TX throughput ────────────────────────────────────────────────
		// Use per-tier Bytes sum (falls back to SentBytes when no tiers).
		// Guard against counter resets (e.g. interface flap).
		currTx := txBytes(cs)
		var txRate float64
		if currTx >= st.prevTxBytes {
			txRate = float64(currTx-st.prevTxBytes) / elapsed
		}

		// ── Drops per second ─────────────────────────────────────────────
		var drRate float64
		if cs.Dropped >= st.prevDropped {
			drRate = float64(cs.Dropped-st.prevDropped) / elapsed
		}

		// ── Latency — max across all tiers ───────────────────────────────
		avMs := maxDelayMs(cs.Tiers, func(t CakeTier) string { return t.AvDelay })
		pkMs := maxDelayMs(cs.Tiers, func(t CakeTier) string { return t.PkDelay })

		// ── Write back computed fields ───────────────────────────────────
		cs.TxBytesPerS = txRate
		cs.DropsPerS = drRate
		cs.MaxAvDelayMs = avMs
		cs.MaxPkDelayMs = pkMs

		// ── Append to ring buffer ────────────────────────────────────────
		st.push(HistorySample{
			T:  now.Unix(),
			Tx: txRate,
			Av: avMs,
			Pk: pkMs,
			Dr: drRate,
		}, hs.capacity)

		// ── Update previous-value state ──────────────────────────────────
		st.prevTxBytes = currTx
		st.prevDropped = cs.Dropped
		st.prevTime = now
	}

	// Prune state for interfaces that are no longer present.
	active := make(map[string]struct{}, len(stats))
	for _, cs := range stats {
		active[cs.Interface] = struct{}{}
	}
	for key := range hs.ifaces {
		if _, ok := active[key]; !ok {
			delete(hs.ifaces, key)
		}
	}
}

// Snapshot returns a copy of all ring buffers in chronological order.
// The returned map is safe to marshal to JSON and to read concurrently.
func (hs *HistoryStore) Snapshot() map[string][]HistorySample {
	hs.mu.RLock()
	defer hs.mu.RUnlock()

	out := make(map[string][]HistorySample, len(hs.ifaces))
	for key, st := range hs.ifaces {
		if samples := st.ordered(hs.capacity); len(samples) > 0 {
			out[key] = samples
		}
	}
	return out
}

// ─────────────────────────── Helpers ────────────────────────────────────────

// maxDelayMs returns the maximum delay value (in milliseconds) across all
// tiers using the given field accessor.
func maxDelayMs(tiers []CakeTier, field func(CakeTier) string) float64 {
	var best float64
	for _, t := range tiers {
		if v := parseDelayMs(field(t)); v > best {
			best = v
		}
	}
	return best
}

// parseDelayMs converts a tc delay string to milliseconds.
// Accepted suffixes: "us" (microseconds), "ms" (milliseconds), "s" (seconds).
// Examples: "6.73ms" -> 6.73,  "123us" -> 0.123,  "0" -> 0.
// Returns 0 on any parse error.
func parseDelayMs(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0
	}

	var unit, numStr string
	for _, sfx := range []string{"us", "ms", "s"} {
		if strings.HasSuffix(s, sfx) {
			unit = sfx
			numStr = strings.TrimSuffix(s, sfx)
			break
		}
	}
	if unit == "" {
		return 0
	}

	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0
	}

	switch unit {
	case "us":
		return v / 1000.0
	case "ms":
		return v
	case "s":
		return v * 1000.0
	}
	return 0
}
