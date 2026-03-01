package parser

import (
	"testing"
)

const sampleTCOutput = `qdisc noqueue 0: dev lo root refcnt 2 
 Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
qdisc fq_codel 0: dev eth0 root refcnt 2 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms memory_limit 32Mb ecn drop_batch 64 
 Sent 11217682446 bytes 9470558 pkt (dropped 0, overlimits 0 requeues 24) 
 backlog 0b 0p requeues 24
  maxpacket 1494 drop_overlimit 0 new_flow_count 299 ecn_mark 0
  new_flows_len 0 old_flows_len 0
qdisc cake 800d: dev eth1 root refcnt 2 bandwidth 50Mbit diffserv4 dual-srchost nat nowash no-ack-filter split-gso rtt 100ms atm overhead 48 memlimit 32Mb 
 Sent 453393887 bytes 1599017 pkt (dropped 2515, overlimits 2072988 requeues 0) 
 backlog 0b 0p requeues 0
 memory used: 238656b of 32Mb
 capacity estimate: 50Mbit
 min/max network layer size:           28 /    1500
 min/max overhead-adjusted size:      106 /    1749
 average network hdr offset:           14

                   Bulk  Best Effort        Video        Voice
  thresh       3125Kbit       50Mbit       25Mbit    12500Kbit
  target         5.81ms          5ms          5ms          5ms
  interval        101ms        100ms        100ms        100ms
  pk_delay          0us        545us         35us        646us
  av_delay          0us         42us          6us         56us
  sp_delay          0us          5us          2us          1us
  backlog            0b           0b           0b           0b
  pkts                0      1592616          209         8707
  bytes               0    455805269        21362      1223812
  way_inds            0        25972            0           19
  way_miss            0        17449          130          338
  way_cols            0            0            0            0
  drops               0         2515            0            0
  marks               0            0            0            0
  ack_drop            0            0            0            0
  sp_flows            0            1            0            1
  bk_flows            0            1            0            0
  un_flows            0            0            0            0
  max_len             0        32300          551          590
  quantum           300         1514          762          381

qdisc ingress ffff: dev eth1 parent ffff:fff1 ---------------- 
 Sent 3158081766 bytes 2777506 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
qdisc cake 800e: dev ifb4eth1 root refcnt 2 bandwidth 50Mbit diffserv4 dual-dsthost nat nowash ingress no-ack-filter split-gso rtt 100ms atm overhead 48 memlimit 32Mb 
 Sent 3194029040 bytes 2748544 pkt (dropped 28962, overlimits 3328299 requeues 0) 
 backlog 0b 0p requeues 0
 memory used: 1425600b of 32Mb
 capacity estimate: 50Mbit
 min/max network layer size:           46 /    1500
 min/max overhead-adjusted size:      106 /    1749
 average network hdr offset:           14

                   Bulk  Best Effort        Video        Voice
  thresh       3125Kbit       50Mbit       25Mbit    12500Kbit
  target         5.81ms          5ms          5ms          5ms
  interval        101ms        100ms        100ms        100ms
  pk_delay          0us        760us       6.73ms       7.09ms
  av_delay          0us        117us       1.49ms       2.44ms
  sp_delay          0us         11us         33us        113us
  backlog            0b           0b           0b           0b
  pkts                0      2767990         2708         6808
  bytes               0   3226939367      2577105      6440994
  way_inds            0        36687            0            0
  way_miss            0        17134           54           63
  way_cols            0            0            0            0
  drops               0        28926            3           33
  marks               0       117224            0            0
  ack_drop            0            0            0            0
  sp_flows            0            2            1            1
  bk_flows            0            1            0            0
  un_flows            0            0            0            0
  max_len             0        68338        41760        20384
  quantum           300         1514          762          381
`

func TestParseTCOutput_Count(t *testing.T) {
	results := parseText(sampleTCOutput)
	if len(results) != 2 {
		t.Fatalf("expected 2 CAKE interfaces, got %d", len(results))
	}
}

func assertEqual(t *testing.T, field, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %q, got %q", field, want, got)
	}
}

func assertUint(t *testing.T, field string, want, got uint64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: want %d, got %d", field, want, got)
	}
}

func TestParseTCOutput_EgressHeader(t *testing.T) {
	cs := parseText(sampleTCOutput)[0]
	assertEqual(t, "interface", "eth1", cs.Interface)
	assertEqual(t, "direction", "egress", cs.Direction)
	assertEqual(t, "bandwidth", "50Mbit", cs.Bandwidth)
	assertEqual(t, "diffserv_mode", "diffserv4", cs.DiffservMode)
	assertEqual(t, "rtt", "100ms", cs.RTT)
	assertEqual(t, "overhead", "48", cs.Overhead)
	assertEqual(t, "dual_mode", "dual-srchost", cs.DualMode)
	if !cs.NATEnabled {
		t.Error("nat_enabled should be true")
	}
	assertEqual(t, "atm_mode", "atm", cs.ATMMode)
}

func TestParseTCOutput_EgressGlobalStats(t *testing.T) {
	cs := parseText(sampleTCOutput)[0]
	assertUint(t, "sent_bytes", 453393887, cs.SentBytes)
	assertUint(t, "sent_pkts", 1599017, cs.SentPkts)
	assertUint(t, "dropped", 2515, cs.Dropped)
	assertUint(t, "overlimits", 2072988, cs.Overlimits)
	assertEqual(t, "memory_used", "238656b", cs.MemoryUsed)
	assertEqual(t, "memory_total", "32Mb", cs.MemoryTotal)
	assertEqual(t, "capacity_est", "50Mbit", cs.CapacityEst)
	assertEqual(t, "min_net", "28", cs.MinNetSize)
	assertEqual(t, "max_net", "1500", cs.MaxNetSize)
	assertEqual(t, "avg_hdr", "14", cs.AvgHdrOffset)
}

func TestParseTCOutput_EgressTiers(t *testing.T) {
	cs := parseText(sampleTCOutput)[0]
	if len(cs.Tiers) != 4 {
		t.Fatalf("expected 4 tiers, got %d", len(cs.Tiers))
	}
	assertEqual(t, "tier0.name", "Bulk", cs.Tiers[0].Name)
	assertEqual(t, "tier1.name", "Best Effort", cs.Tiers[1].Name)
	assertEqual(t, "tier2.name", "Video", cs.Tiers[2].Name)
	assertEqual(t, "tier3.name", "Voice", cs.Tiers[3].Name)

	be := cs.Tiers[1]
	assertUint(t, "be.pkts", 1592616, be.Pkts)
	assertUint(t, "be.drops", 2515, be.Drops)
	assertUint(t, "be.max_len", 32300, be.MaxLen)
	assertUint(t, "be.quantum", 1514, be.Quantum)
	assertEqual(t, "be.thresh", "50Mbit", be.Thresh)
	assertEqual(t, "be.pk_delay", "545us", be.PkDelay)
}

func TestParseTCOutput_FloatDelays(t *testing.T) {
	cs := parseText(sampleTCOutput)[1]
	video := cs.Tiers[2]
	if video.PkDelay != "6.73ms" {
		t.Errorf("expected pk_delay=6.73ms, got %q", video.PkDelay)
	}
	if video.AvDelay != "1.49ms" {
		t.Errorf("expected av_delay=1.49ms, got %q", video.AvDelay)
	}
}

func TestParseTCOutput_IngressStats(t *testing.T) {
	cs := parseText(sampleTCOutput)[1]
	assertEqual(t, "interface", "ifb4eth1", cs.Interface)
	assertUint(t, "dropped", 28962, cs.Dropped)
	assertUint(t, "marks", 117224, cs.Tiers[1].Marks)
}

func TestParseHeader_Precedence(t *testing.T) {
	// precedence is a distinct diffserv mode (legacy TOS Precedence field).
	raw := `qdisc cake 800d: dev eth2 root refcnt 2 bandwidth 20Mbit precedence rtt 100ms noatm overhead 0 memlimit 32Mb 
 Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0`
	stats := parseText(raw)
	if len(stats) != 1 {
		t.Fatalf("expected 1 CAKE interface, got %d", len(stats))
	}
	cs := stats[0]
	assertEqual(t, "diffserv_mode", "precedence", cs.DiffservMode)
	assertEqual(t, "fwmark_mask", "", cs.FwmarkMask) // should not be set
}

func TestParseHeader_FwmarkMask(t *testing.T) {
	// fwmark is a separate tin-override parameter, not a diffserv mode.
	// It takes a bitmask argument and may appear alongside a diffserv keyword.
	raw := `qdisc cake 800d: dev eth3 root refcnt 2 bandwidth 20Mbit diffserv4 fwmark 0xfc rtt 100ms noatm overhead 0 memlimit 32Mb 
 Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0`
	stats := parseText(raw)
	if len(stats) != 1 {
		t.Fatalf("expected 1 CAKE interface, got %d", len(stats))
	}
	cs := stats[0]
	assertEqual(t, "diffserv_mode", "diffserv4", cs.DiffservMode)
	assertEqual(t, "fwmark_mask", "0xfc", cs.FwmarkMask)
}

func TestParseJSONMinimal(t *testing.T) {
	jsonData := `[{"kind":"cake","dev":"eth0","handle":"800d:","options":{"bandwidth":5000000,"diffserv":"diffserv4","nat":true,"atm":"atm","overhead":48,"rtt":100000},"bytes":123,"packets":456,"drops":7,"overlimits":8,"requeues":9,"memory_used":100,"memory_limit":33554432,"capacity_estimate":5000000,"min_network_size":28,"max_network_size":1500,"avg_hdr_offset":14,"tins":[{"threshold_rate":3125,"sent_bytes":0,"drops":0,"max_pkt_len":0,"flow_quantum":300}]}]`
	stats, err := parseJSON([]byte(jsonData))
	if err != nil {
		t.Fatalf("parseJSON error: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(stats))
	}
	cs := stats[0]
	assertEqual(t, "interface", "eth0", cs.Interface)
	assertUint(t, "drops", 7, cs.Dropped)
	assertUint(t, "max_len", 0, cs.Tiers[0].MaxLen)
}

func TestParseUint64_Safe(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"0", 0},
		{"1592616", 1592616},
		{"3226939367", 3226939367},
		{"18446744073709551615", 18446744073709551615},
		{"0b", 0},
		{"0p", 0},
		{"", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		if got := parseUint64(c.in); got != c.want {
			t.Errorf("parseUint64(%q)=%d want %d", c.in, got, c.want)
		}
	}
}

// -----------------------------------------------------------------------------
// cake_mq tests
//
// sampleCakeMQOutput simulates the output of "tc -s qdisc" on a system where
// cake_mq is installed on a two-queue NIC.  The structure is:
//
//	qdisc cake_mq 1: dev eth0 root          ← parent (no stats)
//	qdisc cake 0: dev eth0 parent 1:1 …    ← HW-queue 0 (has stats + tier table)
//	qdisc cake 0: dev eth0 parent 1:2 …    ← HW-queue 1 (has stats + tier table)
//
// The parser must collapse the two sub-queues into one CakeStats entry and
// aggregate all counters/delays correctly.
// -----------------------------------------------------------------------------
const sampleCakeMQOutput = `qdisc cake_mq 1: dev eth0 root refcnt 6 
qdisc cake 0: dev eth0 parent 1:1 refcnt 2 bandwidth 100Mbit diffserv4 dual-srchost nat nowash no-ack-filter split-gso rtt 100ms atm overhead 48 memlimit 32Mb 
 Sent 200000000 bytes 700000 pkt (dropped 100, overlimits 1000000 requeues 0) 
 backlog 0b 0p requeues 0
 memory used: 100000b of 32Mb
 capacity estimate: 100Mbit
 min/max network layer size:           28 /    1500
 min/max overhead-adjusted size:      106 /    1749
 average network hdr offset:           14

                   Bulk  Best Effort        Video        Voice
  thresh       6250Kbit      100Mbit       50Mbit    25000Kbit
  target          5.8ms          5ms          5ms          5ms
  interval        101ms        100ms        100ms        100ms
  pk_delay          0us        400us         20us        500us
  av_delay          0us         30us          4us         40us
  sp_delay          0us          3us          1us          1us
  backlog            0b           0b           0b           0b
  pkts                0       697000          100         4200
  bytes               0    201000000        10000       600000
  way_inds            0        10000            0           10
  way_miss            0         8000           50          150
  way_cols            0            0            0            0
  drops               0          100            0            0
  marks               0            0            0            0
  ack_drop            0            0            0            0
  sp_flows            0            1            0            1
  bk_flows            0            1            0            0
  un_flows            0            0            0            0
  max_len             0        16000          300          400
  quantum           300         1514          762          381

qdisc cake 0: dev eth0 parent 1:2 refcnt 2 bandwidth 100Mbit diffserv4 dual-srchost nat nowash no-ack-filter split-gso rtt 100ms atm overhead 48 memlimit 32Mb 
 Sent 250000000 bytes 900000 pkt (dropped 150, overlimits 1200000 requeues 5) 
 backlog 0b 0p requeues 0
 memory used: 120000b of 32Mb
 capacity estimate: 100Mbit
 min/max network layer size:           28 /    1500
 min/max overhead-adjusted size:      106 /    1749
 average network hdr offset:           14

                   Bulk  Best Effort        Video        Voice
  thresh       6250Kbit      100Mbit       50Mbit    25000Kbit
  target          5.8ms          5ms          5ms          5ms
  interval        101ms        100ms        100ms        100ms
  pk_delay          0us        600us         30us        700us
  av_delay          0us         50us          6us         60us
  sp_delay          0us          5us          2us          2us
  backlog            0b           0b           0b           0b
  pkts                0       897000          150         6300
  bytes               0    251000000        15000       900000
  way_inds            0        15000            0           15
  way_miss            0        10000           80          200
  way_cols            0            0            0            0
  drops               0          150            0            0
  marks               0            0            0            0
  ack_drop            0            0            0            0
  sp_flows            0            1            0            1
  bk_flows            0            1            0            0
  un_flows            0            0            0            0
  max_len             0        24000          400          500
  quantum           300         1514          762          381

`

// TestCakeMQ_Count verifies that two cake sub-queues under one cake_mq parent
// are collapsed into a single CakeStats entry.
func TestCakeMQ_Count(t *testing.T) {
	results := parseText(sampleCakeMQOutput)
	if len(results) != 1 {
		t.Fatalf("expected 1 aggregated CakeStats for cake_mq, got %d", len(results))
	}
}

// TestCakeMQ_Identity verifies that identity fields come from the cake_mq
// parent (handle, interface) while CAKE config is inherited from sub-queues.
func TestCakeMQ_Identity(t *testing.T) {
	cs := parseText(sampleCakeMQOutput)[0]
	assertEqual(t, "interface", "eth0", cs.Interface)
	assertEqual(t, "handle", "1", cs.Handle)
	assertEqual(t, "direction", "egress", cs.Direction)
	assertEqual(t, "bandwidth", "100Mbit", cs.Bandwidth)
	assertEqual(t, "diffserv_mode", "diffserv4", cs.DiffservMode)
	assertEqual(t, "rtt", "100ms", cs.RTT)
	assertEqual(t, "overhead", "48", cs.Overhead)
	if !cs.NATEnabled {
		t.Error("nat_enabled should be true")
	}
	assertEqual(t, "atm_mode", "atm", cs.ATMMode)
}

// TestCakeMQ_GlobalCounters verifies that global counters are summed across
// all hardware queues.
func TestCakeMQ_GlobalCounters(t *testing.T) {
	cs := parseText(sampleCakeMQOutput)[0]
	assertUint(t, "sent_bytes", 450000000, cs.SentBytes)
	assertUint(t, "sent_pkts", 1600000, cs.SentPkts)
	assertUint(t, "dropped", 250, cs.Dropped)
	assertUint(t, "overlimits", 2200000, cs.Overlimits)
	assertUint(t, "requeues", 5, cs.Requeues)
	assertUint(t, "backlog_pkts", 0, cs.BacklogPkts)
	assertEqual(t, "backlog_bytes", "0b", cs.BacklogBytes)
	assertEqual(t, "memory_used", "220000b", cs.MemoryUsed)
	// MemoryTotal is the per-queue limit, kept from the first sub-queue.
	assertEqual(t, "memory_total", "32Mb", cs.MemoryTotal)
	assertEqual(t, "capacity_est", "100Mbit", cs.CapacityEst)
}

// TestCakeMQ_TierCount verifies that four tiers are present after aggregation.
func TestCakeMQ_TierCount(t *testing.T) {
	cs := parseText(sampleCakeMQOutput)[0]
	if len(cs.Tiers) != 4 {
		t.Fatalf("expected 4 tiers, got %d", len(cs.Tiers))
	}
}

// TestCakeMQ_TierCounters verifies that per-tier counters are summed and
// delay strings reflect the worst-case (maximum) value across queues.
func TestCakeMQ_TierCounters(t *testing.T) {
	cs := parseText(sampleCakeMQOutput)[0]
	be := cs.Tiers[1] // "Best Effort"
	assertEqual(t, "tier1.name", "Best Effort", be.Name)

	// Counters: queue-1 + queue-2
	assertUint(t, "be.pkts", 1594000, be.Pkts)
	assertUint(t, "be.bytes", 452000000, be.Bytes)
	assertUint(t, "be.drops", 250, be.Drops)
	assertUint(t, "be.way_inds", 25000, be.WayInds)
	assertUint(t, "be.way_miss", 18000, be.WayMiss)

	// Delays: maximum across the two queues.
	assertEqual(t, "be.pk_delay", "600us", be.PkDelay)
	assertEqual(t, "be.av_delay", "50us", be.AvDelay)
	assertEqual(t, "be.sp_delay", "5us", be.SpDelay)

	// MaxLen: maximum across the two queues.
	assertUint(t, "be.max_len", 24000, be.MaxLen)

	// Config: taken from the first sub-queue (shared).
	assertEqual(t, "be.thresh", "100Mbit", be.Thresh)
	assertUint(t, "be.quantum", 1514, be.Quantum)
}

// TestCakeMQ_VoiceTierDelayMax verifies pick-max across queues for Voice tier.
func TestCakeMQ_VoiceTierDelayMax(t *testing.T) {
	cs := parseText(sampleCakeMQOutput)[0]
	voice := cs.Tiers[3]
	assertEqual(t, "voice.name", "Voice", voice.Name)
	assertEqual(t, "voice.pk_delay", "700us", voice.PkDelay)
}

// TestCakeMQ_StandaloneUnaffected verifies that ordinary (non-cake_mq) cake
// qdiscs in the same tc output are still emitted as independent entries.
func TestCakeMQ_StandaloneUnaffected(t *testing.T) {
	combined := sampleCakeMQOutput + sampleTCOutput
	results := parseText(combined)
	// cake_mq on eth0 → 1, plus eth1 egress + ifb4eth1 ingress → 2 = 3 total.
	if len(results) != 3 {
		t.Fatalf("expected 3 CakeStats entries (1 cake_mq + 2 standalone), got %d", len(results))
	}
}

// -----------------------------------------------------------------------------
// Besteffort (single-tin / "Tin 0") tests — the JackH case:
// user has CAKE on a single interface (egress only, no IFB) running in
// besteffort mode.  The tier header is "Tin 0", not the named multi-tier
// format, so the parser must recognise it and emit correct delay/counter data.
// -----------------------------------------------------------------------------

const sampleBesteffortOutput = `qdisc noqueue 0: dev lo root refcnt 2 
 Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
qdisc mq 0: dev eth0 root 
 Sent 2945616358 bytes 1973175 pkt (dropped 0, overlimits 0 requeues 749) 
 backlog 0b 0p requeues 749
qdisc cake 8005: dev eth1 root refcnt 17 bandwidth 22500Kbit besteffort triple-isolate nat nowash no-ack-filter split-gso rtt 100ms raw overhead 0 
 Sent 137306352 bytes 1053588 pkt (dropped 1449, overlimits 1694970 requeues 49) 
 backlog 0b 0p requeues 49
 memory used: 4097Kb of 4Mb
 capacity estimate: 22500Kbit
 min/max network layer size:           42 /    1514
 min/max overhead-adjusted size:       42 /    1514
 average network hdr offset:           14

                  Tin 0
  thresh      22500Kbit
  target            5ms
  interval        100ms
  pk_delay       3.26ms
  av_delay       1.21ms
  sp_delay          4us
  backlog            0b
  pkts          1055037
  bytes       137632238
  way_inds           86
  way_miss          274
  way_cols            0
  drops            1449
  marks               0
  ack_drop            0
  sp_flows            1
  bk_flows            1
  un_flows            0
  max_len         16654
  quantum           686
`

// TestBesteffort_Count verifies that a single-interface egress-only CAKE setup
// (no IFB, besteffort mode) is detected as exactly one entry.
func TestBesteffort_Count(t *testing.T) {
	results := parseText(sampleBesteffortOutput)
	if len(results) != 1 {
		t.Fatalf("expected 1 CAKE interface (egress only), got %d", len(results))
	}
}

// TestBesteffort_Header verifies that header fields are parsed correctly for a
// besteffort CAKE qdisc with the "raw overhead 0" variant of the header line.
// TestBesteffort_Header verifies that header fields are parsed correctly for a
// besteffort CAKE qdisc with the "raw overhead 0" variant of the header line.
func TestBesteffort_Header(t *testing.T) {
	cs := parseText(sampleBesteffortOutput)[0]
	assertEqual(t, "interface", "eth1", cs.Interface)
	assertEqual(t, "direction", "egress", cs.Direction)
	assertEqual(t, "bandwidth", "22500Kbit", cs.Bandwidth)
	assertEqual(t, "diffserv_mode", "besteffort", cs.DiffservMode)
	assertEqual(t, "rtt", "100ms", cs.RTT)
	assertEqual(t, "overhead", "0", cs.Overhead)
	// raw/noatm → ATMMode must be empty (no ATM or PTM framing compensation)
	assertEqual(t, "atm_mode", "", cs.ATMMode)
	// mpu not specified in this header — must be empty
	assertEqual(t, "mpu", "", cs.MPU)
	if !cs.NATEnabled {
		t.Error("nat_enabled should be true")
	}
}

// TestBesteffort_GlobalStats verifies global counters for the besteffort case.
func TestBesteffort_GlobalStats(t *testing.T) {
	cs := parseText(sampleBesteffortOutput)[0]
	assertUint(t, "sent_bytes", 137306352, cs.SentBytes)
	assertUint(t, "sent_pkts", 1053588, cs.SentPkts)
	assertUint(t, "dropped", 1449, cs.Dropped)
	assertUint(t, "overlimits", 1694970, cs.Overlimits)
	assertUint(t, "requeues", 49, cs.Requeues)
	assertEqual(t, "capacity_est", "22500Kbit", cs.CapacityEst)
	assertEqual(t, "memory_used", "4097Kb", cs.MemoryUsed)
	assertEqual(t, "memory_total", "4Mb", cs.MemoryTotal)
}

// TestBesteffort_SingleTier verifies that the "Tin 0" tier header is recognised
// and that all per-tier fields (including delays) are parsed correctly.
// This is the core fix: before the patch, Tiers was always empty for
// besteffort mode, causing latency to show as 0 ms permanently.
func TestBesteffort_SingleTier(t *testing.T) {
	cs := parseText(sampleBesteffortOutput)[0]
	if len(cs.Tiers) != 1 {
		t.Fatalf("expected 1 tier (besteffort = single Tin 0), got %d", len(cs.Tiers))
	}
	tin := cs.Tiers[0]
	assertEqual(t, "tier.name", "Tin 0", tin.Name)
	assertEqual(t, "tier.thresh", "22500Kbit", tin.Thresh)
	assertEqual(t, "tier.target", "5ms", tin.Target)
	assertEqual(t, "tier.pk_delay", "3.26ms", tin.PkDelay)
	assertEqual(t, "tier.av_delay", "1.21ms", tin.AvDelay)
	assertEqual(t, "tier.sp_delay", "4us", tin.SpDelay)
	assertUint(t, "tier.pkts", 1055037, tin.Pkts)
	assertUint(t, "tier.bytes", 137632238, tin.Bytes)
	assertUint(t, "tier.way_inds", 86, tin.WayInds)
	assertUint(t, "tier.way_miss", 274, tin.WayMiss)
	assertUint(t, "tier.drops", 1449, tin.Drops)
	assertUint(t, "tier.max_len", 16654, tin.MaxLen)
	assertUint(t, "tier.quantum", 686, tin.Quantum)
}

// ---------------------------------------------------------------------------
// Header-field tests: ATM mode, PTM mode, noatm, MPU, flow modes,
// autorate-ingress.  Each test uses the minimal tc output needed to exercise
// a specific set of header tokens without importing a full sample.
// ---------------------------------------------------------------------------

// minimalCakeHeader builds a minimal tc -s qdisc snippet for a cake qdisc on
// "eth0" with the caller-supplied parameter tokens appended to the first line.
func minimalCakeHeader(params string) string {
	return "qdisc cake 8011: dev eth0 root refcnt 2 bandwidth 100Mbit diffserv4 hosts rtt 20ms " + params + "\n" +
		" Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0)\n" +
		" backlog 0b 0p requeues 0\n" +
		" memory used: 14Kb of 4Mb\n" +
		" capacity estimate: 100Mbit\n"
}

// TestParseHeader_ATMMode verifies that the "atm" keyword sets ATMMode to "atm"
// and that the overhead value is captured correctly alongside it.
func TestParseHeader_ATMMode(t *testing.T) {
	cs := parseText(minimalCakeHeader("atm overhead 40"))[0]
	assertEqual(t, "atm_mode", "atm", cs.ATMMode)
	assertEqual(t, "overhead", "40", cs.Overhead)
	assertEqual(t, "mpu", "", cs.MPU)
}

// TestParseHeader_PTMMode verifies that the "ptm" keyword sets ATMMode to "ptm"
// (distinct from "atm") so the dashboard can label it correctly.
func TestParseHeader_PTMMode(t *testing.T) {
	cs := parseText(minimalCakeHeader("ptm overhead 30"))[0]
	assertEqual(t, "atm_mode", "ptm", cs.ATMMode)
	assertEqual(t, "overhead", "30", cs.Overhead)
	assertEqual(t, "mpu", "", cs.MPU)
}

// TestParseHeader_NoATM verifies that the "noatm" keyword leaves ATMMode empty
// (i.e. the dashboard should not display any ATM/PTM indicator).
func TestParseHeader_NoATM(t *testing.T) {
	cs := parseText(minimalCakeHeader("noatm overhead 0"))[0]
	assertEqual(t, "atm_mode", "", cs.ATMMode)
	assertEqual(t, "overhead", "0", cs.Overhead)
}

// TestParseHeader_Raw verifies that the "raw" keyword (alias for noatm) also
// leaves ATMMode empty.
func TestParseHeader_Raw(t *testing.T) {
	cs := parseText(minimalCakeHeader("raw overhead 0"))[0]
	assertEqual(t, "atm_mode", "", cs.ATMMode)
}

// TestParseHeader_MPU verifies that "mpu N" stores the numeric string in MPU
// and that it can coexist with noatm and an explicit overhead.
func TestParseHeader_MPU(t *testing.T) {
	cs := parseText(minimalCakeHeader("mpu 84 noatm overhead 38"))[0]
	assertEqual(t, "mpu", "84", cs.MPU)
	assertEqual(t, "overhead", "38", cs.Overhead)
	assertEqual(t, "atm_mode", "", cs.ATMMode)
}

// TestParseHeader_MPU_WithATM verifies MPU + ATM framing coexist correctly.
func TestParseHeader_MPU_WithATM(t *testing.T) {
	cs := parseText(minimalCakeHeader("mpu 64 atm overhead 40"))[0]
	assertEqual(t, "mpu", "64", cs.MPU)
	assertEqual(t, "atm_mode", "atm", cs.ATMMode)
}

// TestParseHeader_FlowModes verifies that each flow-mode keyword is stored in
// DualMode (or left empty for flowblind which disables flow classification).
func TestParseHeader_FlowModes(t *testing.T) {
	cases := []struct {
		keyword  string
		wantMode string
	}{
		{"flowblind", "flowblind"},
		{"srchost", "srchost"},
		{"dsthost", "dsthost"},
		{"hosts", "hosts"},
		{"flows", "flows"},
		{"dual-srchost", "dual-srchost"},
		{"dual-dsthost", "dual-dsthost"},
		{"triple-isolate", "triple-isolate"},
	}
	for _, c := range cases {
		t.Run(c.keyword, func(t *testing.T) {
			snippet := "qdisc cake 8011: dev eth0 root refcnt 2 bandwidth 100Mbit diffserv4 " +
				c.keyword + " rtt 20ms noatm overhead 0\n" +
				" Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0)\n" +
				" backlog 0b 0p requeues 0\n" +
				" memory used: 14Kb of 4Mb\n" +
				" capacity estimate: 100Mbit\n"
			res := parseText(snippet)
			if len(res) == 0 {
				t.Fatal("expected 1 result, got 0")
			}
			assertEqual(t, "dual_mode", c.wantMode, res[0].DualMode)
		})
	}
}

// TestParseHeader_AutorateIngress verifies that "autorate-ingress" is stored
// as the Bandwidth value (matching what tc prints for ingress-autorate qdiscs).
func TestParseHeader_AutorateIngress(t *testing.T) {
	snippet := "qdisc cake 8011: dev ifb4eth0 root refcnt 2 bandwidth autorate-ingress diffserv4 dual-dsthost rtt 20ms noatm overhead 22\n" +
		" Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0)\n" +
		" backlog 0b 0p requeues 0\n" +
		" memory used: 14Kb of 4Mb\n" +
		" capacity estimate: 22000Kbit\n"
	cs := parseText(snippet)[0]
	assertEqual(t, "bandwidth", "autorate-ingress", cs.Bandwidth)
	assertEqual(t, "dual_mode", "dual-dsthost", cs.DualMode)
	assertEqual(t, "overhead", "22", cs.Overhead)
}

// TestZeroCakeInterfaces verifies that a tc output with no CAKE qdiscs (e.g.
// a system using fq_codel or mq instead) returns an empty slice rather than
// panicking or returning nil.
func TestZeroCakeInterfaces(t *testing.T) {
	noCakeOutput := `qdisc noqueue 0: dev lo root refcnt 2 
 Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
qdisc mq 0: dev eth0 root 
 Sent 1000000 bytes 5000 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
qdisc fq_codel 0: dev eth0 parent :1 limit 10240p flows 1024 quantum 1514 target 5ms interval 100ms memory_limit 32Mb ecn drop_batch 64 
 Sent 1000000 bytes 5000 pkt (dropped 0, overlimits 0 requeues 0) 
 backlog 0b 0p requeues 0
`
	results := parseText(noCakeOutput)
	if len(results) != 0 {
		t.Fatalf("expected 0 CAKE interfaces (none configured), got %d", len(results))
	}
}

// TestParseTierNames_TinFormat verifies that the "Tin N" compound tier name
// is parsed as a single name, not split into two separate names.
func TestParseTierNames_TinFormat(t *testing.T) {
	cases := []struct {
		words []string
		want  []string
	}{
		{[]string{"Tin", "0"}, []string{"Tin 0"}},
		{[]string{"Tin", "0", "Tin", "1", "Tin", "2"}, []string{"Tin 0", "Tin 1", "Tin 2"}},
		{[]string{"Bulk", "Best", "Effort", "Video", "Voice"}, []string{"Bulk", "Best Effort", "Video", "Voice"}},
		{[]string{"CS1", "CS2", "CS3", "CS4", "CS5", "CS6", "CS7", "BE"}, []string{"CS1", "CS2", "CS3", "CS4", "CS5", "CS6", "CS7", "BE"}},
	}
	for _, c := range cases {
		got := parseTierNames(c.words)
		if len(got) != len(c.want) {
			t.Errorf("parseTierNames(%v): got %v, want %v", c.words, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("parseTierNames(%v)[%d]: got %q, want %q", c.words, i, got[i], c.want[i])
			}
		}
	}
}

// TestCakeParseDelayUsec exercises the delay-string parser used by aggregation.
func TestCakeParseDelayUsec(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"0us", 0},
		{"500us", 500},
		{"1ms", 1000},
		{"1.5ms", 1500},
		{"1s", 1e6},
		{"", 0},
		{"0", 0},
	}
	for _, c := range cases {
		if got := cakeParseDelayUsec(c.in); got != c.want {
			t.Errorf("cakeParseDelayUsec(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestParseBytesStr exercises the byte-string parser used in aggregation.
// It must handle both raw-byte strings ("Nb"), and the SI-prefix variants
// that tc emits for memory fields ("NKb", "NMb", "NGb").
func TestParseBytesStr(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"0b", 0},
		{"100000b", 100000},
		{"238656b", 238656},
		{"", 0},
		// SI-prefix variants — seen in tc memory output e.g. "4097Kb of 4Mb"
		{"4097Kb", 4097 * 1024},
		{"4Mb", 4 * 1024 * 1024},
		{"32Mb", 32 * 1024 * 1024},
		{"1Gb", 1 * 1024 * 1024 * 1024},
	}
	for _, c := range cases {
		if got := parseBytesStr(c.in); got != c.want {
			t.Errorf("parseBytesStr(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestHeaderParentHandle exercises the parent-handle extractor.
func TestHeaderParentHandle(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"qdisc cake 0: dev eth0 parent 1:1 refcnt 2 bandwidth unlimited", "1"},
		{"qdisc cake 0: dev eth0 parent 2:3 refcnt 2 bandwidth unlimited", "2"},
		{"qdisc cake 800d: dev eth1 root refcnt 2 bandwidth 50Mbit", ""},
		{"qdisc cake_mq 1: dev eth0 root refcnt 6", ""},
	}
	for _, c := range cases {
		if got := headerParentHandle(c.line); got != c.want {
			t.Errorf("headerParentHandle(%q) = %q, want %q", c.line, got, c.want)
		}
	}
}
