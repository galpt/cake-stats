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
// Always uses the human-readable text output from `tc -s qdisc` for maximum
// field coverage.  The JSON path (tc -j) is intentionally avoided because the
// JSON tin representation omits many fields that the text output provides
// (tier names, target, interval, delay values, per-tier packet counters, etc.).
func CollectStats(ctx context.Context) ([]types.CakeStats, error) {
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
			if nat, ok := opts["nat"].(bool); ok {
				cs.NATEnabled = nat
			}
			if wash, ok := opts["wash"].(bool); ok {
				cs.WashEnabled = wash
			}
			// The tc JSON output does not currently emit an "atm" key, but handle
			// it defensively in case future iproute2 versions add it.
			if atm, ok := opts["atm"].(string); ok && atm != "" {
				cs.ATMMode = atm
			}
			if v, ok := getUint(opts, "mpu"); ok && v > 0 {
				cs.MPU = fmt.Sprintf("%d", v)
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

	// Intermediate parse result annotated with routing metadata.
	type blockResult struct {
		cs           types.CakeStats
		parentHandle string // non-empty for cake sub-queues attached to a cake_mq
		isCakeMQ     bool   // true for the cake_mq parent qdisc block
	}
	var parsed []blockResult

	for _, b := range blocks {
		if len(b.lines) == 0 {
			continue
		}
		header := b.lines[0]
		switch {
		case strings.Contains(header, "qdisc cake_mq "):
			// cake_mq parent block: parse header only for handle/interface/direction.
			if cs, ok := parseCakeBlock(b.lines); ok {
				parsed = append(parsed, blockResult{cs: cs, isCakeMQ: true})
			}
		case strings.Contains(header, "qdisc cake "):
			// Traditional standalone cake OR a cake sub-qdisc under cake_mq.
			if cs, ok := parseCakeBlock(b.lines); ok {
				parsed = append(parsed, blockResult{
					cs:           cs,
					parentHandle: headerParentHandle(header),
				})
			}
		}
	}

	// Build a lookup from (interface, major-handle) to the cake_mq parent entry.
	type ifaceHandle struct{ iface, handle string }
	mqParents := make(map[ifaceHandle]types.CakeStats)
	for _, r := range parsed {
		if r.isCakeMQ {
			mqParents[ifaceHandle{r.cs.Interface, r.cs.Handle}] = r.cs
		}
	}

	// Group cake sub-queue instances by their cake_mq parent key.
	subQueues := make(map[ifaceHandle][]types.CakeStats)
	for _, r := range parsed {
		if !r.isCakeMQ && r.parentHandle != "" {
			key := ifaceHandle{r.cs.Interface, r.parentHandle}
			if _, hasMQ := mqParents[key]; hasMQ {
				subQueues[key] = append(subQueues[key], r.cs)
			}
		}
	}

	// Emit results, preserving original block order.
	var result []types.CakeStats
	emittedMQ := make(map[ifaceHandle]bool)
	for _, r := range parsed {
		switch {
		case r.isCakeMQ:
			key := ifaceHandle{r.cs.Interface, r.cs.Handle}
			if emittedMQ[key] {
				continue
			}
			emittedMQ[key] = true
			if subs := subQueues[key]; len(subs) > 0 {
				result = append(result, aggregateCakeMQSubQueues(r.cs, subs))
			} else {
				result = append(result, r.cs)
			}
		case r.parentHandle != "":
			key := ifaceHandle{r.cs.Interface, r.parentHandle}
			if _, hasMQ := mqParents[key]; hasMQ {
				// Already aggregated under its cake_mq parent; skip.
				continue
			}
			// Orphaned sub-qdisc (no cake_mq parent visible) – emit verbatim.
			result = append(result, r.cs)
		default:
			// Standalone root cake qdisc.
			result = append(result, r.cs)
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
	// Default to egress; the "ingress" CAKE option keyword overrides this below.
	// We intentionally do not infer direction from the attachment point (root vs
	// parent), because cake_mq sub-queues appear as "parent X:N" yet are egress.
	cs.Direction = "egress"
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
		case "atm":
			cs.ATMMode = "atm"
		case "ptm":
			cs.ATMMode = "ptm"
		case "noatm", "raw":
			cs.ATMMode = "noatm"
		case "mpu":
			if i+1 < len(fs) {
				cs.MPU = fs[i+1]
				i++
			}
		case "autorate-ingress":
			cs.Bandwidth = "autorate-ingress"
		case "flowblind", "srchost", "dsthost", "hosts", "flows":
			cs.DualMode = tok
		case "nat":
			cs.NATEnabled = true
		case "nonat":
			cs.NATEnabled = false
		case "wash":
			cs.WashEnabled = true
		case "nowash":
			cs.WashEnabled = false
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
		// The content is of the form "dropped N, overlimits M requeues R".
		// Each comma-separated segment may contain multiple space-separated
		// key-value pairs (e.g. "overlimits M requeues R").
		for _, part := range strings.Split(line[s+1:e], ",") {
			tokens := strings.Fields(strings.TrimSpace(part))
			for j := 0; j+1 < len(tokens); j += 2 {
				switch tokens[j] {
				case "dropped":
					cs.Dropped = parseUint64(tokens[j+1])
				case "overlimits":
					cs.Overlimits = parseUint64(tokens[j+1])
				case "requeues":
					cs.Requeues = parseUint64(tokens[j+1])
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
	// "Tin" is used by CAKE when running in besteffort mode (single tin = "Tin 0")
	// and in some diffserv8 configurations ("Tin 0" through "Tin 7").
	"Tin": true,
}

func isTierHeaderLine(first string) bool {
	return knownTierWords[first]
}

func parseTierNames(words []string) []string {
	var names []string
	for i := 0; i < len(words); i++ {
		switch {
		case words[i] == "Best" && i+1 < len(words) && words[i+1] == "Effort":
			// "Best Effort" is a two-word tier name used in diffserv4.
			names = append(names, "Best Effort")
			i++
		case words[i] == "Tin" && i+1 < len(words):
			// "Tin N" is a single tier name used by besteffort (single tin) and
			// generic diffserv8 configurations.  Treat it as one compound name.
			names = append(names, "Tin "+words[i+1])
			i++
		default:
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

// headerParentHandle extracts the major handle from a "parent X:N" token pair
// in a tc qdisc header line.  For example, "parent 1:2" returns "1".
// Returns an empty string when no parent token is present (root qdisc).
func headerParentHandle(line string) string {
	fs := strings.Fields(line)
	for i := 0; i < len(fs)-1; i++ {
		if fs[i] == "parent" {
			ref := fs[i+1]
			if colon := strings.IndexByte(ref, ':'); colon > 0 {
				return ref[:colon]
			}
		}
	}
	return ""
}

// aggregateCakeMQSubQueues merges per-hardware-queue CakeStats from a cake_mq
// setup into a single logical CakeStats that represents the whole interface.
//
// Shared CAKE configuration (bandwidth, diffserv mode, RTT, overhead, NAT/ATM
// flags, direction) is taken from the first sub-queue, since cake_mq stores one
// config object that is referenced by all sub-queues.  Identity fields (handle,
// interface, raw header) come from the cake_mq parent qdisc.  Monotonic
// counters (bytes sent, packets, drops, …) are summed across queues.  Delay
// metrics are reported as the maximum across all queues (worst-case latency).
func aggregateCakeMQSubQueues(parent types.CakeStats, subs []types.CakeStats) types.CakeStats {
	if len(subs) == 0 {
		return parent
	}
	// Bootstrap from first sub-queue to inherit all shared CAKE configuration.
	agg := subs[0]
	// Override identity fields with values from the cake_mq parent.
	agg.Handle = parent.Handle
	agg.Interface = parent.Interface
	agg.RawHeader = parent.RawHeader
	agg.UpdatedAt = parent.UpdatedAt
	// Direction is determined by the CAKE "ingress" option that lives in the
	// sub-queue options string, already parsed correctly into agg (= subs[0]).

	// Sum global counters across all sub-queues.
	agg.SentBytes, agg.SentPkts = 0, 0
	agg.Dropped, agg.Overlimits, agg.Requeues = 0, 0, 0
	agg.BacklogPkts = 0
	var backlogBytes, memUsed uint64
	for _, s := range subs {
		agg.SentBytes += s.SentBytes
		agg.SentPkts += s.SentPkts
		agg.Dropped += s.Dropped
		agg.Overlimits += s.Overlimits
		agg.Requeues += s.Requeues
		agg.BacklogPkts += s.BacklogPkts
		backlogBytes += parseBytesStr(s.BacklogBytes)
		memUsed += parseBytesStr(s.MemoryUsed)
	}
	agg.BacklogBytes = fmt.Sprintf("%db", backlogBytes)
	agg.MemoryUsed = fmt.Sprintf("%db", memUsed)
	// MemoryTotal is the per-queue limit identical across all queues; keep first.

	// Aggregate per-tier statistics.
	if len(subs[0].Tiers) > 0 {
		queueTiers := make([][]types.CakeTier, len(subs))
		for i, s := range subs {
			queueTiers[i] = s.Tiers
		}
		agg.Tiers = aggregateCakeTiers(queueTiers)
	}
	return agg
}

// parseBytesStr parses a byte-count string emitted by tc (e.g. "238656b",
// "4097Kb", "32Mb", "1Gb") and returns the value in bytes.
// Returns 0 for empty or unrecognised input.
func parseBytesStr(s string) uint64 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "Gb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Gb"), 10, 64)
		return v * 1024 * 1024 * 1024
	case strings.HasSuffix(s, "Mb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Mb"), 10, 64)
		return v * 1024 * 1024
	case strings.HasSuffix(s, "Kb"):
		v, _ := strconv.ParseUint(strings.TrimSuffix(s, "Kb"), 10, 64)
		return v * 1024
	default:
		s = strings.TrimSuffix(s, "b")
		v, _ := strconv.ParseUint(s, 10, 64)
		return v
	}
}

// aggregateCakeTiers combines per-tier statistics from N cake sub-queues into
// a single tier slice.  Configuration values (thresh, target, interval,
// quantum, name) are taken from the first queue since they are shared.  All
// packet/byte counters are summed.  Delay strings report the maximum across
// queues (worst-case latency view).  Backlog is summed.
func aggregateCakeTiers(queues [][]types.CakeTier) []types.CakeTier {
	if len(queues) == 0 || len(queues[0]) == 0 {
		return nil
	}
	nTiers := len(queues[0])
	out := make([]types.CakeTier, nTiers)
	for ti := 0; ti < nTiers; ti++ {
		// Seed with shared config from the first queue.
		out[ti] = queues[0][ti]
		// Zero all mutable counters before summation.
		out[ti].Pkts = 0
		out[ti].Bytes = 0
		out[ti].WayInds = 0
		out[ti].WayMiss = 0
		out[ti].WayCols = 0
		out[ti].Drops = 0
		out[ti].Marks = 0
		out[ti].AckDrop = 0
		out[ti].SpFlows = 0
		out[ti].BkFlows = 0
		out[ti].UnFlows = 0
		out[ti].MaxLen = 0
		out[ti].Backlog = ""
		for _, q := range queues {
			if ti >= len(q) {
				continue
			}
			t := q[ti]
			out[ti].Pkts += t.Pkts
			out[ti].Bytes += t.Bytes
			out[ti].WayInds += t.WayInds
			out[ti].WayMiss += t.WayMiss
			out[ti].WayCols += t.WayCols
			out[ti].Drops += t.Drops
			out[ti].Marks += t.Marks
			out[ti].AckDrop += t.AckDrop
			out[ti].SpFlows += t.SpFlows
			out[ti].BkFlows += t.BkFlows
			out[ti].UnFlows += t.UnFlows
			if t.MaxLen > out[ti].MaxLen {
				out[ti].MaxLen = t.MaxLen
			}
		}
		// Delays: return the string from whichever queue had the highest value.
		out[ti].PkDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.PkDelay })
		out[ti].AvDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.AvDelay })
		out[ti].SpDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.SpDelay })
		// Backlog: sum byte values across queues.
		var backlogSum uint64
		for _, q := range queues {
			if ti < len(q) {
				backlogSum += parseBytesStr(q[ti].Backlog)
			}
		}
		out[ti].Backlog = fmt.Sprintf("%db", backlogSum)
	}
	return out
}

// maxDelayStr returns the delay string with the highest numeric value from the
// given tier index across all queue tier slices.
func maxDelayStr(queues [][]types.CakeTier, tierIdx int, field func(types.CakeTier) string) string {
	var best float64
	var bestStr string
	for _, q := range queues {
		if tierIdx >= len(q) {
			continue
		}
		s := field(q[tierIdx])
		if v := cakeParseDelayUsec(s); v > best || bestStr == "" {
			best = v
			bestStr = s
		}
	}
	return bestStr
}

// cakeParseDelayUsec converts a CAKE delay string (e.g. "500us", "1.5ms",
// "2s") to a float64 in microseconds.  Returns 0 for empty or unknown input.
func cakeParseDelayUsec(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0
	}
	for _, sfx := range []string{"us", "ms", "s"} {
		if strings.HasSuffix(s, sfx) {
			v, err := strconv.ParseFloat(strings.TrimSuffix(s, sfx), 64)
			if err != nil {
				return 0
			}
			switch sfx {
			case "us":
				return v
			case "ms":
				return v * 1e3
			case "s":
				return v * 1e6
			}
		}
	}
	return 0
}
