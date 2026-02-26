package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// CakeTier holds per-tier statistics from the CAKE table section.
// All counters use uint64 to handle arbitrarily large values without overflow.
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
	FwmarkMask   string `json:"fwmark_mask"` // non-empty when fwmark MASK is present in header
	NATEnabled   bool   `json:"nat_enabled"`
	ATMEnabled   bool   `json:"atm_enabled"`
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

// runTC executes "tc -s qdisc" and returns stdout.
// It searches common sbin paths so it works on both OpenWrt and Linux distros.
func runTC() (string, error) {
	candidates := []string{"tc", "/sbin/tc", "/usr/sbin/tc", "/usr/bin/tc"}
	tcPath := ""
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			tcPath = p
			break
		}
	}
	if tcPath == "" {
		tcPath = "tc"
	}
	cmd := exec.Command(tcPath, "-s", "qdisc")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tc -s qdisc: %w", err)
	}
	return string(out), nil
}

// ParseTCOutput parses the full output of "tc -s qdisc" and returns every
// CAKE qdisc found.
func ParseTCOutput(raw string) []CakeStats {
	lines := strings.Split(raw, "\n")

	// Split into per-qdisc blocks.
	type block struct{ lines []string }
	var blocks []block
	cur := block{}
	for _, l := range lines {
		if strings.HasPrefix(l, "qdisc ") && len(cur.lines) > 0 {
			blocks = append(blocks, cur)
			cur = block{}
		}
		cur.lines = append(cur.lines, l)
	}
	if len(cur.lines) > 0 {
		blocks = append(blocks, cur)
	}

	var result []CakeStats
	for _, b := range blocks {
		if len(b.lines) == 0 || !strings.Contains(b.lines[0], "qdisc cake") {
			continue
		}
		if cs, ok := parseCakeBlock(b.lines); ok {
			result = append(result, cs)
		}
	}
	return result
}

// parseCakeBlock converts one CAKE qdisc block into a CakeStats struct.
func parseCakeBlock(lines []string) (CakeStats, bool) {
	if len(lines) == 0 {
		return CakeStats{}, false
	}
	cs := CakeStats{UpdatedAt: time.Now()}
	cs.RawHeader = strings.TrimSpace(lines[0])
	parseHeader(&cs, lines[0])

	var tierNames []string
	tierFieldBuf := map[string][]string{}
	inTable := false

	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(trimmed, "Sent "):
			parseSentLine(&cs, trimmed)

		case strings.HasPrefix(trimmed, "backlog "):
			parseBacklogLine(&cs, trimmed)

		case strings.HasPrefix(trimmed, "memory used:"):
			parseMemoryLine(&cs, trimmed)

		case strings.HasPrefix(trimmed, "capacity estimate:"):
			cs.CapacityEst = strings.TrimSpace(strings.TrimPrefix(trimmed, "capacity estimate:"))

		case strings.HasPrefix(trimmed, "min/max network layer size:"):
			cs.MinNetSize, cs.MaxNetSize = parseMinMax(trimmed)

		case strings.HasPrefix(trimmed, "min/max overhead-adjusted size:"):
			cs.MinAdjSize, cs.MaxAdjSize = parseMinMax(trimmed)

		case strings.HasPrefix(trimmed, "average network hdr offset:"):
			cs.AvgHdrOffset = strings.TrimSpace(
				strings.TrimPrefix(trimmed, "average network hdr offset:"),
			)

		case isTierHeaderLine(fields[0]):
			tierNames = parseTierNames(fields)
			inTable = true

		case inTable && len(fields) >= 2 && unicode.IsLower(rune(fields[0][0])):
			tierFieldBuf[fields[0]] = fields[1:]
		}
	}

	if len(tierNames) > 0 {
		cs.Tiers = assembleTiers(tierNames, tierFieldBuf)
	}
	return cs, true
}

// parseHeader fills a CakeStats from the qdisc header line.
func parseHeader(cs *CakeStats, line string) {
	fs := strings.Fields(strings.TrimSpace(line))
	if len(fs) < 5 {
		return
	}
	cs.Handle = strings.TrimSuffix(fs[2], ":")
	cs.Interface = fs[4]
	if len(fs) > 5 {
		if fs[5] == "root" {
			cs.Direction = "egress"
		} else {
			cs.Direction = "ingress"
		}
	}
	for i := 5; i < len(fs); i++ {
		tok := fs[i]
		switch tok {
		case "bandwidth":
			if i+1 < len(fs) {
				cs.Bandwidth = fs[i+1]
				i++
			}
		case "diffserv3", "diffserv4", "diffserv8", "besteffort", "precedence":
			cs.DiffservMode = tok
		case "fwmark":
			// fwmark is not a diffserv mode; it takes a bitmask argument.
			if i+1 < len(fs) {
				cs.FwmarkMask = fs[i+1]
				i++
			}
		case "rtt":
			if i+1 < len(fs) {
				cs.RTT = fs[i+1]
				i++
			}
		case "overhead":
			if i+1 < len(fs) {
				cs.Overhead = fs[i+1]
				i++
			}
		case "atm", "ptm":
			cs.ATMEnabled = true
		case "nat":
			cs.NATEnabled = true
		case "dual-srchost", "dual-dsthost", "triple-isolate", "single":
			cs.DualMode = tok
		case "ingress":
			cs.Direction = "ingress"
		case "memlimit":
			if i+1 < len(fs) {
				cs.MemLimit = fs[i+1]
				i++
			}
		}
	}
}

// parseSentLine handles: Sent X bytes Y pkt (dropped A, overlimits B requeues C)
func parseSentLine(cs *CakeStats, line string) {
	fs := strings.Fields(line)
	if len(fs) >= 4 {
		cs.SentBytes = parseUint64(fs[1])
		cs.SentPkts = parseUint64(fs[3])
	}
	s, e := strings.Index(line, "("), strings.Index(line, ")")
	if s != -1 && e != -1 && e > s {
		for _, part := range strings.Split(line[s+1:e], ",") {
			kv := strings.Fields(strings.TrimSpace(part))
			if len(kv) >= 2 {
				switch kv[0] {
				case "dropped":
					cs.Dropped = parseUint64(kv[1])
				case "overlimits":
					cs.Overlimits = parseUint64(kv[1])
				case "requeues":
					cs.Requeues = parseUint64(kv[1])
				}
			}
		}
	}
}

// parseBacklogLine handles: backlog Xb Yp requeues Z
func parseBacklogLine(cs *CakeStats, line string) {
	fs := strings.Fields(line)
	if len(fs) >= 3 {
		cs.BacklogBytes = fs[1]
		cs.BacklogPkts = parseUint64(strings.TrimSuffix(fs[2], "p"))
	}
}

// parseMemoryLine handles: memory used: Xb of YMb
func parseMemoryLine(cs *CakeStats, line string) {
	after := strings.TrimSpace(strings.TrimPrefix(line, "memory used:"))
	parts := strings.Fields(after)
	if len(parts) >= 3 {
		cs.MemoryUsed = parts[0]
		cs.MemoryTotal = parts[2]
	}
}

// parseMinMax extracts lo/hi from "...label: X / Y".
func parseMinMax(line string) (lo, hi string) {
	i := strings.Index(line, ":")
	if i == -1 {
		return
	}
	parts := strings.SplitN(strings.TrimSpace(line[i+1:]), "/", 2)
	if len(parts) == 2 {
		lo = strings.TrimSpace(parts[0])
		hi = strings.TrimSpace(parts[1])
	}
	return
}

// knownTierWords are the first tokens that identify a CAKE tier-name header line.
var knownTierWords = map[string]bool{
	"Bulk": true, "Best": true, "Voice": true, "Video": true,
	"CS1": true, "CS2": true, "CS3": true, "CS4": true,
	"CS5": true, "CS6": true, "CS7": true, "BE": true,
}

// isTierHeaderLine returns true when the token matches a known tier name.
func isTierHeaderLine(first string) bool {
	return knownTierWords[first]
}

// parseTierNames reconstructs tier names from a whitespace-split header row.
// "Best Effort" is the only two-word tier name.
func parseTierNames(words []string) []string {
	var names []string
	for i := 0; i < len(words); i++ {
		if words[i] == "Best" && i+1 < len(words) && words[i+1] == "Effort" {
			names = append(names, "Best Effort")
			i++
		} else {
			names = append(names, words[i])
		}
	}
	return names
}

// assembleTiers builds a CakeTier slice from the collected field->values map.
func assembleTiers(names []string, buf map[string][]string) []CakeTier {
	tiers := make([]CakeTier, len(names))
	for i, name := range names {
		tiers[i].Name = name
	}

	get := func(field string, idx int) string {
		v, ok := buf[field]
		if !ok || idx >= len(v) {
			return ""
		}
		return v[idx]
	}
	getU := func(field string, idx int) uint64 {
		return parseUint64(get(field, idx))
	}

	for i := range tiers {
		t := &tiers[i]
		t.Thresh = get("thresh", i)
		t.Target = get("target", i)
		t.Interval = get("interval", i)
		t.PkDelay = get("pk_delay", i)
		t.AvDelay = get("av_delay", i)
		t.SpDelay = get("sp_delay", i)
		t.Backlog = get("backlog", i)
		t.Pkts = getU("pkts", i)
		t.Bytes = getU("bytes", i)
		t.WayInds = getU("way_inds", i)
		t.WayMiss = getU("way_miss", i)
		t.WayCols = getU("way_cols", i)
		t.Drops = getU("drops", i)
		t.Marks = getU("marks", i)
		t.AckDrop = getU("ack_drop", i)
		t.SpFlows = getU("sp_flows", i)
		t.BkFlows = getU("bk_flows", i)
		t.UnFlows = getU("un_flows", i)
		t.MaxLen = getU("max_len", i)
		t.Quantum = getU("quantum", i)
	}
	return tiers
}

// parseUint64 safely converts a string to uint64.
// Returns 0 on error -- never wraps or goes negative.
func parseUint64(s string) uint64 {
	s = strings.TrimRight(s, "bBkKmMgGpP")
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
