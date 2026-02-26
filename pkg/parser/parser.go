package parser

// The current implementation of CollectStats shells out to `tc` and prefers
// the JSON output (tc -j).  If even lower latency is required the
// implementation can be swapped to use a netlink client such as
// github.com/jsimonetti/rtnetlink to query qdisc statistics directly,
// avoiding fork/exec entirely.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/galpt/cake-stats/pkg/types"
)

var (
	jsonSupport    bool
	jsonDetectOnce sync.Once
)

// supportsJSON detects whether the local `tc` binary can emit JSON.  The
// result is cached so we only run the probe once.
func supportsJSON() bool {
	jsonDetectOnce.Do(func() {
		cmd := exec.Command("tc", "-j", "-s", "qdisc")
		if err := cmd.Run(); err == nil {
			jsonSupport = true
		}
	})
	return jsonSupport
}

// CollectStats polls the kernel via `tc` and returns a slice of CakeStats.
// It prefers JSON output and falls back to parsing the human-readable text.
func CollectStats(ctx context.Context) ([]types.CakeStats, error) {
	if supportsJSON() {
		out, err := exec.CommandContext(ctx, "tc", "-j", "-s", "qdisc").Output()
		if err == nil {
			return parseJSON(out)
		}
		// if JSON invocation fails for any reason, fall back to text
	}

	out, err := exec.CommandContext(ctx, "tc", "-s", "qdisc").Output()
	if err != nil {
		return nil, fmt.Errorf("tc -s qdisc: %w", err)
	}
	return parseText(string(out)), nil
}

// parseJSON handles the JSON output from "tc -j -s qdisc".  We don't try to
// mirror every field the kernel sends; the goal is to populate a minimal
// CakeStats value with the same information our text parser would produce.
func parseJSON(raw []byte) ([]types.CakeStats, error) {
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, err
	}
	var out []types.CakeStats
	for _, obj := range arr {
		if kind, _ := obj["kind"].(string); kind != "cake" {
			continue
		}
		cs := types.CakeStats{UpdatedAt: time.Now().UTC()}
		if dev, ok := obj["dev"].(string); ok {
			cs.Interface = dev
		}
		if h, ok := obj["handle"].(string); ok {
			cs.Handle = h
		}
		if opts, ok := obj["options"].(map[string]interface{}); ok {
			if bw, ok := opts["bandwidth"].(float64); ok {
				cs.Bandwidth = fmt.Sprintf("%dbit", int64(bw))
			}
			if ds, ok := opts["diffserv"].(string); ok {
				cs.DiffservMode = ds
			}
			if nat, ok := opts["nat"].(bool); ok && nat {
				cs.NATEnabled = true
			}
			if atm, ok := opts["atm"].(string); ok && atm != "" {
				cs.ATMEnabled = true
			}
			if ov, ok := opts["overhead"].(float64); ok {
				cs.Overhead = fmt.Sprintf("%v", int64(ov))
			}
			if rtt, ok := opts["rtt"].(float64); ok {
				cs.RTT = fmt.Sprintf("%dms", int64(rtt/1000))
			}
		}
		if v, ok := getUint(obj, "bytes"); ok {
			cs.SentBytes = v
		}
		if v, ok := getUint(obj, "packets"); ok {
			cs.SentPkts = v
		}
		if v, ok := getUint(obj, "drops"); ok {
			cs.Dropped = v
		}
		if v, ok := getUint(obj, "overlimits"); ok {
			cs.Overlimits = v
		}
		if v, ok := getUint(obj, "requeues"); ok {
			cs.Requeues = v
		}
		if v, ok := getUint(obj, "memory_used"); ok {
			cs.MemoryUsed = fmt.Sprintf("%db", v)
		}
		if v, ok := getUint(obj, "memory_limit"); ok {
			cs.MemoryTotal = fmt.Sprintf("%dMb", v/1024/1024)
		}
		if v, ok := getUint(obj, "capacity_estimate"); ok {
			cs.CapacityEst = fmt.Sprintf("%dMbit", v/1000000)
		}
		if v, ok := getUint(obj, "min_network_size"); ok {
			cs.MinNetSize = fmt.Sprintf("%d", v)
		}
		if v, ok := getUint(obj, "max_network_size"); ok {
			cs.MaxNetSize = fmt.Sprintf("%d", v)
		}
		if v, ok := getUint(obj, "avg_hdr_offset"); ok {
			cs.AvgHdrOffset = fmt.Sprintf("%d", v)
		}
		if tins, ok := obj["tins"].([]interface{}); ok {
			var tiers []types.CakeTier
			for _, ti := range tins {
				if m, ok := ti.(map[string]interface{}); ok {
					var t types.CakeTier
					if thr, ok := getUint(m, "threshold_rate"); ok {
						t.Thresh = fmt.Sprintf("%d", thr)
					}
					if sb, ok := getUint(m, "sent_bytes"); ok {
						t.Bytes = sb
					}
					if dr, ok := getUint(m, "drops"); ok {
						t.Drops = dr
					}
					if ml, ok := getUint(m, "max_pkt_len"); ok {
						t.MaxLen = ml
					}
					if q, ok := getUint(m, "flow_quantum"); ok {
						t.Quantum = q
					}
					tiers = append(tiers, t)
				}
			}
			cs.Tiers = tiers
		}
		out = append(out, cs)
	}
	return out, nil
}

func parseText(raw string) []types.CakeStats {
	lines := strings.Split(raw, "\n")
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
	var result []types.CakeStats
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

// --- helpers below ---

func parseCakeBlock(lines []string) (types.CakeStats, bool) {
	if len(lines) == 0 {
		return types.CakeStats{}, false
	}
	cs := types.CakeStats{UpdatedAt: time.Now().UTC()}
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
			cs.AvgHdrOffset = strings.TrimSpace(strings.TrimPrefix(trimmed, "average network hdr offset:"))
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

func parseHeader(cs *types.CakeStats, line string) {
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

func parseSentLine(cs *types.CakeStats, line string) {
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

func parseBacklogLine(cs *types.CakeStats, line string) {
	fs := strings.Fields(line)
	if len(fs) >= 3 {
		cs.BacklogBytes = fs[1]
		cs.BacklogPkts = parseUint64(strings.TrimSuffix(fs[2], "p"))
	}
}

func parseMemoryLine(cs *types.CakeStats, line string) {
	after := strings.TrimSpace(strings.TrimPrefix(line, "memory used:"))
	parts := strings.Fields(after)
	if len(parts) >= 3 {
		cs.MemoryUsed = parts[0]
		cs.MemoryTotal = parts[2]
	}
}

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

var knownTierWords = map[string]bool{
	"Bulk": true, "Best": true, "Voice": true, "Video": true,
	"CS1": true, "CS2": true, "CS3": true, "CS4": true,
	"CS5": true, "CS6": true, "CS7": true, "BE": true,
}

func isTierHeaderLine(first string) bool {
	return knownTierWords[first]
}

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

func assembleTiers(names []string, buf map[string][]string) []types.CakeTier {
	tiers := make([]types.CakeTier, len(names))
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

func parseUint64(s string) uint64 {
	s = strings.TrimRight(s, "bBkKmMgGpP")
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func getUint(m map[string]interface{}, key string) (uint64, bool) {
	if v, ok := m[key]; ok {
		switch t := v.(type) {
		case float64:
			return uint64(t), true
		case string:
			return parseUint64(t), true
		}
	}
	return 0, false
}
