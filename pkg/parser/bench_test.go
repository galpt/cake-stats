package parser

import (
	"testing"
)

// Benchmark suite demonstrating near-zero-allocation parsing.
// Compare against the pre-refactor numbers (FINDINGS.md):
//
//	2-interface standalone: 124 allocs/op → 3 allocs/op
//	besteffort:              63 allocs/op → 3 allocs/op
//	no CAKE:                  8 allocs/op → 0 allocs/op

func BenchmarkParseText_Standalone(b *testing.B) {
	data := []byte(sampleTCOutput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(data)
		if len(results) != 2 {
			b.Fatalf("expected 2, got %d", len(results))
		}
	}
}

func BenchmarkParseText_CakeMQ(b *testing.B) {
	data := []byte(sampleCakeMQOutput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(data)
		if len(results) != 1 {
			b.Fatalf("expected 1, got %d", len(results))
		}
	}
}

func BenchmarkParseText_Besteffort(b *testing.B) {
	data := []byte(sampleBesteffortOutput)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(data)
		if len(results) != 1 {
			b.Fatalf("expected 1, got %d", len(results))
		}
	}
}

func BenchmarkParseText_Segal72(b *testing.B) {
	data := []byte(sampleSegal72Output)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(data)
		if len(results) != 1 {
			b.Fatalf("expected 1, got %d", len(results))
		}
	}
}

func BenchmarkParseText_Tiny(b *testing.B) {
	tiny := []byte("qdisc cake 800d: dev eth0 root refcnt 2 bandwidth 50Mbit diffserv4 dual-srchost nat nowash no-ack-filter split-gso rtt 100ms atm overhead 48 memlimit 32Mb \n Sent 453393887 bytes 1599017 pkt (dropped 2515, overlimits 2072988 requeues 0) \n backlog 0b 0p requeues 0\n memory used: 238656b of 32Mb\n capacity estimate: 50Mbit\n min/max network layer size:           28 /    1500\n min/max overhead-adjusted size:      106 /    1749\n average network hdr offset:           14\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(tiny)
		if len(results) != 1 {
			b.Fatalf("expected 1, got %d", len(results))
		}
	}
}

func BenchmarkParseText_NoCake(b *testing.B) {
	noCake := []byte("qdisc noqueue 0: dev lo root refcnt 2 \n Sent 0 bytes 0 pkt (dropped 0, overlimits 0 requeues 0) \n backlog 0b 0p requeues 0\nqdisc mq 0: dev eth0 root \n Sent 1000000 bytes 5000 pkt (dropped 0, overlimits 0 requeues 0) \n backlog 0b 0p requeues 0\n")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := parseText(noCake)
		if len(results) != 0 {
			b.Fatalf("expected 0, got %d", len(results))
		}
	}
}
