package history

import (
	"testing"
	"time"

	"github.com/galpt/cake-stats/pkg/types"
)

func BenchmarkHistoryRecord(b *testing.B) {
	store := NewHistoryStore(10)
	stats := []types.CakeStats{{Interface: "eth0"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Record(stats, time.Second)
	}
}

func TestHistorySnapshot(t *testing.T) {
	store := NewHistoryStore(3)
	stats := []types.CakeStats{{Interface: "eth0"}}
	// first record establishes state, no sample
	store.Record(stats, time.Second)
	store.Record(stats, time.Second)
	snap := store.Snapshot()
	if _, ok := snap["eth0"]; !ok {
		t.Fatal("expected snapshot for eth0")
	}
}
