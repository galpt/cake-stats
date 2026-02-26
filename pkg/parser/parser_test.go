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
	if !cs.ATMEnabled {
		t.Error("atm_enabled should be true")
	}
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
