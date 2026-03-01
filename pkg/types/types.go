package types

import (
	"encoding/json"
	"time"
)

//go:generate easyjson -all

// CakeTier holds per-tier statistics from the CAKE table section. All counters
// use uint64 to handle arbitrarily large values without overflow.
// (Fields are identical to the original parser package.)
type CakeTier struct {
	Name     string `json:"name"`
	Thresh   string `json:"thresh"`
	Target   string `json:"target"`
	Interval string `json:"interval"`
	PkDelay  string `json:"pk_delay"`
	AvDelay  string `json:"av_delay"`
	SpDelay  string `json:"sp_delay"`
	Backlog  string `json:"backlog"`
	Pkts     uint64 `json:"pkts"`
	Bytes    uint64 `json:"bytes"`
	WayInds  uint64 `json:"way_inds"`
	WayMiss  uint64 `json:"way_miss"`
	WayCols  uint64 `json:"way_cols"`
	Drops    uint64 `json:"drops"`
	Marks    uint64 `json:"marks"`
	AckDrop  uint64 `json:"ack_drop"`
	SpFlows  uint64 `json:"sp_flows"`
	BkFlows  uint64 `json:"bk_flows"`
	UnFlows  uint64 `json:"un_flows"`
	MaxLen   uint64 `json:"max_len"`
	Quantum  uint64 `json:"quantum"`
}

// CakeStats holds all parsed information for a single CAKE qdisc instance.
type CakeStats struct {
	Interface    string `json:"interface"`
	Handle       string `json:"handle"`
	Direction    string `json:"direction"`
	Bandwidth    string `json:"bandwidth"`
	DiffservMode string `json:"diffserv_mode"`
	RTT          string `json:"rtt"`
	Overhead     string `json:"overhead"`
	DualMode     string `json:"dual_mode"`
	FwmarkMask   string `json:"fwmark_mask"`
	NATEnabled   bool   `json:"nat_enabled"`
	// ATMMode stores the framing-compensation mode string exactly as tc prints
	// it: "atm" for ATM cell framing (ADSL), "ptm" for PTM encoding (VDSL2),
	// or "" (empty) when no ATM/PTM compensation is active (noatm / raw).
	// Replaces the old ATMEnabled bool which collapsed atm and ptm into one.
	ATMMode      string `json:"atm_mode"`
	// MPU stores the minimum packet unit value when configured (e.g. "84").
	// Empty string means the mpu parameter was absent or zero.
	MPU          string `json:"mpu"`
	MemLimit     string `json:"memlimit"`
	RawHeader    string `json:"raw_header"`

	SentBytes  uint64 `json:"sent_bytes"`
	SentPkts   uint64 `json:"sent_pkts"`
	Dropped    uint64 `json:"dropped"`
	Overlimits uint64 `json:"overlimits"`
	Requeues   uint64 `json:"requeues"`

	BacklogBytes string `json:"backlog_bytes"`
	BacklogPkts  uint64 `json:"backlog_pkts"`

	MemoryUsed  string `json:"memory_used"`
	MemoryTotal string `json:"memory_total"`
	CapacityEst string `json:"capacity_estimate"`

	MinNetSize   string `json:"min_net_size"`
	MaxNetSize   string `json:"max_net_size"`
	MinAdjSize   string `json:"min_adj_size"`
	MaxAdjSize   string `json:"max_adj_size"`
	AvgHdrOffset string `json:"avg_hdr_offset"`

	Tiers     []CakeTier `json:"tiers"`
	UpdatedAt time.Time  `json:"updated_at"`

	// Computed per-poll by HistoryStore.Record — not parsed from tc output.
	// Zero on the first poll (no previous sample to diff against).
	TxBytesPerS  float64 `json:"tx_bytes_per_s"`
	DropsPerS    float64 `json:"drops_per_s"`
	MaxAvDelayMs float64 `json:"max_av_delay_ms"`
	MaxPkDelayMs float64 `json:"max_pk_delay_ms"`
}

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

// StatsResponse is the JSON message sent to clients containing the current
// interface statistics along with a timestamp.
type StatsResponse struct {
	Interfaces []CakeStats `json:"interfaces"`
	UpdatedAt  string      `json:"updated_at"`
}

// HistoryResponse is the serializable representation of the in-memory history
// store.  It's a map from interface name to an ordered slice of samples.
type HistoryResponse map[string][]HistorySample

// MarshalJSON implements json.Marshaler using a manually allocated buffer.
// It mirrors the allocation behaviour that easyjson would produce; we
// include it here so the repository can build without requiring codegen.
// In a real release, run `go generate ./...` to produce optimized functions.
func (r StatsResponse) MarshalJSON() ([]byte, error) {
	// simple manual serialization without allocations beyond the returned
	// slice.  It is not completely zero-allocation but avoids intermediate
	// maps and strings.
	buf := make([]byte, 0, 256)
	buf = append(buf, '{')
	buf = append(buf, `"interfaces":`...)
	// marshal interfaces using json.Marshal (cheap for slice)
	if v, err := jsonMarshal(r.Interfaces); err == nil {
		buf = append(buf, v...)
	} else {
		return nil, err
	}
	buf = append(buf, ',')
	buf = append(buf, `"updated_at":`...)
	buf = append(buf, '"')
	buf = append(buf, r.UpdatedAt...)
	buf = append(buf, '"')
	buf = append(buf, '}')
	return buf, nil
}

// jsonMarshal is a thin wrapper around the stdlib json package.  It's defined
// here so the StatsResponse.MarshalJSON method can reference it without
// creating an import cycle.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
