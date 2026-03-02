package history

import (
	"sync"
	"time"

	"github.com/galpt/cake-stats/pkg/types"
	"github.com/galpt/cake-stats/pkg/util"
)

// ifaceState tracks per-interface counters and the ring buffer.
type ifaceState struct {
	prevTxBytes uint64
	prevDropped uint64
	prevTime    time.Time
	samples     []types.HistorySample
	head        int
	count       int
}

func newIfaceState(capacity int, cs *types.CakeStats) *ifaceState {
	return &ifaceState{
		prevTxBytes: txBytes(cs),
		prevDropped: cs.Dropped,
		prevTime:    time.Now(),
		samples:     make([]types.HistorySample, capacity),
	}
}

func txBytes(cs *types.CakeStats) uint64 {
	if cs.SentBytes > 0 {
		return cs.SentBytes
	}
	var sum uint64
	for _, t := range cs.Tiers {
		sum += t.Bytes
	}
	return sum
}

func (st *ifaceState) push(s types.HistorySample, capacity int) {
	st.samples[st.head] = s
	st.head = (st.head + 1) % capacity
	if st.count < capacity {
		st.count++
	}
}

func (st *ifaceState) ordered(capacity int) []types.HistorySample {
	if st.count == 0 {
		return nil
	}
	out := make([]types.HistorySample, st.count)
	if st.count < capacity {
		copy(out, st.samples[:st.count])
	} else {
		n := copy(out, st.samples[st.head:])
		copy(out[n:], st.samples[:st.head])
	}
	return out
}

// HistoryStore is a thread-safe collection of per-interface ring buffers.
type HistoryStore struct {
	mu       sync.RWMutex
	ifaces   map[string]*ifaceState
	capacity int
}

func NewHistoryStore(capacity int) *HistoryStore {
	if capacity < 2 {
		capacity = 2
	}
	return &HistoryStore{
		ifaces:   make(map[string]*ifaceState),
		capacity: capacity,
	}
}

func (hs *HistoryStore) Record(stats []types.CakeStats, interval time.Duration) {
	now := time.Now()
	hs.mu.Lock()
	defer hs.mu.Unlock()

	for i := range stats {
		cs := &stats[i]
		key := cs.Interface
		st, exists := hs.ifaces[key]
		if !exists {
			hs.ifaces[key] = newIfaceState(hs.capacity, cs)
			continue
		}
		elapsed := now.Sub(st.prevTime).Seconds()
		if elapsed <= 0 {
			elapsed = interval.Seconds()
		}
		currTx := txBytes(cs)
		var txRate float64
		if currTx >= st.prevTxBytes {
			txRate = float64(currTx-st.prevTxBytes) / elapsed
		}
		var drRate float64
		if cs.Dropped >= st.prevDropped {
			drRate = float64(cs.Dropped-st.prevDropped) / elapsed
		}
		avMs := maxDelayMs(cs.Tiers, func(t types.CakeTier) string { return t.AvDelay })
		pkMs := maxDelayMs(cs.Tiers, func(t types.CakeTier) string { return t.PkDelay })
		cs.TxBytesPerS = txRate
		cs.DropsPerS = drRate
		cs.MaxAvDelayMs = avMs
		cs.MaxPkDelayMs = pkMs
		st.push(types.HistorySample{
			T:  now.Unix(),
			Tx: txRate,
			Av: avMs,
			Pk: pkMs,
			Dr: drRate,
		}, hs.capacity)
		st.prevTxBytes = currTx
		st.prevDropped = cs.Dropped
		st.prevTime = now
	}

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

func (hs *HistoryStore) Snapshot() types.HistoryResponse {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	out := make(types.HistoryResponse, len(hs.ifaces))
	for key, st := range hs.ifaces {
		if samples := st.ordered(hs.capacity); len(samples) > 0 {
			out[key] = samples
		}
	}
	return out
}

func maxDelayMs(tiers []types.CakeTier, field func(types.CakeTier) string) float64 {
	var best float64
	for _, t := range tiers {
		if v := util.ParseDelayMs(field(t)); v > best {
			best = v
		}
	}
	return best
}
