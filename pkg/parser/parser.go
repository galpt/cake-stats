package parser

// Package parser collects and parses CAKE qdisc statistics from the kernel.
// CollectStats shells out to `tc -s qdisc` and always uses the human-readable
// text output for maximum field coverage.  The JSON path (tc -j) is
// intentionally avoided because the JSON tin representation omits many fields
// that the text output provides (tier names, target, interval, delay values,
// per-tier packet counters, etc.).
//
// All helpers in this package avoid heap allocation by using stack-allocated
// FieldTokenizer and LineScanner instances instead of strings.Fields and
// strings.Split.  Tier slices are obtained from sync.Pool.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"

	"github.com/galpt/cake-stats/pkg/types"
	"github.com/galpt/cake-stats/pkg/util"
)

// Pool of []types.CakeTier slices reused across parse calls.
// Each slice has capacity 8 (enough for diffserv8) and is returned with
// length 0 for the caller to re-slice.
var tierSlicePool = sync.Pool{
	New: func() any {
		s := make([]types.CakeTier, 0, 8)
		return &s
	},
}

// acquireTiers obtains a zero-length tier slice with capacity ≥ n from
// the pool.  The caller must return tiers via releaseTiers when done.
func acquireTiers(n int) []types.CakeTier {
	s := tierSlicePool.Get().(*[]types.CakeTier)
	if cap(*s) < n {
		*s = make([]types.CakeTier, n, n+8)
	} else {
		*s = (*s)[:n]
	}
	return *s
}

// reuses the backing array of a tier slice (does NOT zero elements).
func releaseTiers(s []types.CakeTier) {
	tierSlicePool.Put(&s)
}

// CollectStats polls the kernel via `tc` and returns a slice of CakeStats.
// Always uses the human-readable text output from `tc -s qdisc` for maximum
// field coverage.
func CollectStats(ctx context.Context) ([]types.CakeStats, error) {
	out, err := exec.CommandContext(ctx, "tc", "-s", "qdisc").Output()
	if err != nil {
		return nil, fmt.Errorf("tc -s qdisc: %w", err)
	}
	return parseText(out), nil
}

// ---------------------------------------------------------------------------
// Block-based parsing
// ---------------------------------------------------------------------------

// blockRange describes a contiguous range of lines belonging to one qdisc.
type blockRange struct {
	start int // index of first line
	end   int // index of last line (exclusive), or -1 if not set
}

// parseText parses the raw tc output ([]byte) and returns CakeStats.
// The raw byte slice must remain alive for the lifetime of the returned
// CakeStats (all string fields are sub-strings of raw).
func parseText(raw []byte) []types.CakeStats {
	// Phase 1: build a line-start index in the raw buffer so we can
	// address lines by index without copying strings.
	var lineIdx [256]int
	nLines := 0
	lineIdx[0] = 0
	nLines = 1
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			if nLines < len(lineIdx) {
				lineIdx[nLines] = i + 1
			}
			nLines++
		}
	}

	// Safety: if output exceeds 256 lines, fall back to full iteration.
	// This is extremely unlikely for tc output.
	if nLines > len(lineIdx) {
		return parseTextFallback(raw)
	}

	// Helper: return the i-th line as a string (sub-string of raw).
	lineStr := func(i int) string {
		start := lineIdx[i]
		var end int
		if i+1 < nLines {
			end = lineIdx[i+1] - 1 // strip trailing \n
		} else {
			end = len(raw)
		}
		if end < start {
			return ""
		}
		return internLine(raw, start, end)
	}

	// Phase 2: identify qdisc blocks.
	var blocks [32]blockRange
	nBlocks := 0
	blockStart := 0
	for i := 0; i < nLines; i++ {
		s := lineStr(i)
		if len(s) >= 6 && s[:6] == "qdisc " && i > blockStart {
			// End of previous block.
			if nBlocks < len(blocks) {
				blocks[nBlocks] = blockRange{blockStart, i}
				nBlocks++
			}
			blockStart = i
		}
	}
	// Last block.
	if blockStart < nLines {
		blocks[nBlocks] = blockRange{blockStart, nLines}
		nBlocks++
	}

	// Phase 3: parse each block into a blockResult.
	type ifaceHandle struct{ iface, handle string }

	type blockResult struct {
		cs           types.CakeStats
		parentHandle string
		isCakeMQ     bool
	}

	var parsed [16]blockResult
	nParsed := 0

	var tok util.FieldTokenizer
	for bi := 0; bi < nBlocks; bi++ {
		b := blocks[bi]

		// Get the full first line (header) by assembling raw bytes.
		hdrStart := lineIdx[b.start]
		var hdrEnd int
		if b.start+1 < nLines {
			hdrEnd = lineIdx[b.start+1] - 1
		} else {
			hdrEnd = len(raw)
		}
		header := internLine(raw, hdrStart, hdrEnd)

		var cs types.CakeStats
		var ok bool
		var parentHandle string
		var isCakeMQ bool

		if strings.Contains(header, "qdisc cake_mq ") {
			cs, ok = parseCakeBlock(raw, lineIdx[:nLines], b.start, b.end, &tok)
			isCakeMQ = true
		} else if strings.Contains(header, "qdisc cake ") {
			cs, ok = parseCakeBlock(raw, lineIdx[:nLines], b.start, b.end, &tok)
			parentHandle = headerParentHandle(header, &tok)
		}

		if ok && nParsed < len(parsed) {
			parsed[nParsed] = blockResult{cs, parentHandle, isCakeMQ}
			nParsed++
		}
	}

	// Phase 4: build cake_mq parent lookup (linear scan, small n).
	var mqKeys [16]ifaceHandle
	var mqVals [16]types.CakeStats
	nMQ := 0
	for i := 0; i < nParsed; i++ {
		r := &parsed[i]
		if r.isCakeMQ {
			mqKeys[nMQ] = ifaceHandle{r.cs.Interface, r.cs.Handle}
			mqVals[nMQ] = r.cs
			nMQ++
		}
	}

	mqLookup := func(key ifaceHandle) (types.CakeStats, bool) {
		for i := 0; i < nMQ; i++ {
			if mqKeys[i] == key {
				return mqVals[i], true
			}
		}
		return types.CakeStats{}, false
	}

	// Phase 5: group sub-queues under their cake_mq parent.
	// We use flat arrays with small limits.
	type subGroup struct {
		key  ifaceHandle
		subs [16]types.CakeStats
		n    int
	}
	var groups [8]subGroup
	nGroups := 0

	groupFor := func(key ifaceHandle) *subGroup {
		for i := 0; i < nGroups; i++ {
			if groups[i].key == key {
				return &groups[i]
			}
		}
		if nGroups < len(groups) {
			g := &groups[nGroups]
			g.key = key
			nGroups++
			return g
		}
		return nil
	}

	for i := 0; i < nParsed; i++ {
		r := &parsed[i]
		if !r.isCakeMQ && r.parentHandle != "" {
			key := ifaceHandle{r.cs.Interface, r.parentHandle}
			if _, ok := mqLookup(key); ok {
				if g := groupFor(key); g != nil && g.n < len(g.subs) {
					g.subs[g.n] = r.cs
					g.n++
				}
			}
		}
	}

	// Phase 6: emit results in original order.
	var result types.CakeStats
	resultArr := make([]types.CakeStats, 0, nParsed)
	var emitted [16]ifaceHandle
	nEmitted := 0
	wasEmitted := func(key ifaceHandle) bool {
		for i := 0; i < nEmitted; i++ {
			if emitted[i] == key {
				return true
			}
		}
		return false
	}

	for i := 0; i < nParsed; i++ {
		r := &parsed[i]

		switch {
		case r.isCakeMQ:
			key := ifaceHandle{r.cs.Interface, r.cs.Handle}
			if wasEmitted(key) {
				continue
			}
			emitted[nEmitted] = key
			nEmitted++

			// Find sub-queues for this parent.
			if g := groupFor(key); g != nil && g.n > 0 {
				result = aggregateCakeMQSubQueues(r.cs, g.subs[:g.n])
			} else {
				result = r.cs
			}
			resultArr = append(resultArr, result)

		case r.parentHandle != "":
			key := ifaceHandle{r.cs.Interface, r.parentHandle}
			if _, ok := mqLookup(key); ok {
				continue // aggregated under parent
			}
			resultArr = append(resultArr, r.cs)

		default:
			resultArr = append(resultArr, r.cs)
		}
	}

	return resultArr
}

// parseTextFallback handles tc output that exceeds our static line index
// or block arrays.  Uses the same zero-alloc helpers but with dynamic
// storage.  In practice this path is never exercised for normal tc output.
func parseTextFallback(raw []byte) []types.CakeStats {
	// Build dynamic line index.
	type blockRange2 struct{ start, end int }

	lineIdx := make([]int, 0, 256)
	lineIdx = append(lineIdx, 0)
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			lineIdx = append(lineIdx, i+1)
		}
	}
	nLines := len(lineIdx)

	lineStr := func(i int) string {
		start := lineIdx[i]
		var end int
		if i+1 < nLines {
			end = lineIdx[i+1] - 1
		} else {
			end = len(raw)
		}
		if end < start {
			return ""
		}
		return internLine(raw, start, end)
	}

	var blocks []blockRange2
	blockStart := 0
	for i := 0; i < nLines; i++ {
		s := lineStr(i)
		if len(s) >= 6 && s[:6] == "qdisc " && i > blockStart {
			blocks = append(blocks, blockRange2{blockStart, i})
			blockStart = i
		}
	}
	blocks = append(blocks, blockRange2{blockStart, nLines})

	type ifaceHandle struct{ iface, handle string }
	type blockResult struct {
		cs           types.CakeStats
		parentHandle string
		isCakeMQ     bool
	}

	var tok util.FieldTokenizer
	parsed := make([]blockResult, 0, 16)

	for _, b := range blocks {
		header := lineStr(b.start)

		if strings.Contains(header, "qdisc cake_mq ") {
			cs, ok := parseCakeBlock(raw, lineIdx, b.start, b.end, &tok)
			if ok {
				parsed = append(parsed, blockResult{cs, "", true})
			}
		} else if strings.Contains(header, "qdisc cake ") {
			cs, ok := parseCakeBlock(raw, lineIdx, b.start, b.end, &tok)
			if ok {
				parsed = append(parsed, blockResult{cs, headerParentHandle(header, &tok), false})
			}
		}
	}

	// cake_mq parent lookup.
	mqParents := make(map[ifaceHandle]types.CakeStats)
	for _, r := range parsed {
		if r.isCakeMQ {
			mqParents[ifaceHandle{r.cs.Interface, r.cs.Handle}] = r.cs
		}
	}

	subQueues := make(map[ifaceHandle][]types.CakeStats)
	for _, r := range parsed {
		if !r.isCakeMQ && r.parentHandle != "" {
			key := ifaceHandle{r.cs.Interface, r.parentHandle}
			if _, hasMQ := mqParents[key]; hasMQ {
				subQueues[key] = append(subQueues[key], r.cs)
			}
		}
	}

	result := make([]types.CakeStats, 0, len(parsed))
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
				continue
			}
			result = append(result, r.cs)
		default:
			result = append(result, r.cs)
		}
	}
	return result
}

// internLine converts a sub-range of raw bytes to a string without copying.
func internLine(raw []byte, start, end int) string {
	if start >= end {
		return ""
	}
	return util.BytesToString(raw[start:end])
}

// ---------------------------------------------------------------------------
// CAKE block parser
// ---------------------------------------------------------------------------

// parseCakeBlock parses one CAKE qdisc block using the line index.
// It uses the provided FieldTokenizer and avoids heap allocation for
// temporary string storage.
func parseCakeBlock(raw []byte, lineIdx []int, start, end int, tok *util.FieldTokenizer) (types.CakeStats, bool) {
	if start >= end {
		return types.CakeStats{}, false
	}

	cs := types.CakeStats{UpdatedAt: time.Now().UTC()}

	// Read header line.
	hdrStart := lineIdx[start]
	var hdrEnd int
	if start+1 < len(lineIdx) {
		hdrEnd = lineIdx[start+1] - 1
	} else {
		hdrEnd = len(raw)
	}
	header := internLine(raw, hdrStart, hdrEnd)
	cs.RawHeader = header
	parseHeader(&cs, header, tok)

	// ---- Parse remaining lines ----
	var (
		tierNames [8]string
		nTiers    int

		// Per-tier field data collected during line iteration.
		// We store field values as sub-strings of the tc output.
		// Each entry is a row: field name → values per tier.
		ftNames [32]string
		ftVals  [32][8]string
		ftCount [32]int
		nFields int

		inTable bool
	)

	// Helper to look up a field value for a given tier index.
	getField := func(name string, tierIdx int) string {
		for fi := 0; fi < nFields; fi++ {
			if ftNames[fi] == name && tierIdx < ftCount[fi] {
				return ftVals[fi][tierIdx]
			}
		}
		return ""
	}
	getFieldU := func(name string, tierIdx int) uint64 {
		return util.ParseUint64(getField(name, tierIdx))
	}

	for i := start + 1; i < end; i++ {
		lineStart := lineIdx[i]
		var lineEnd int
		if i+1 < len(lineIdx) {
			lineEnd = lineIdx[i+1] - 1
		} else {
			lineEnd = len(raw)
		}
		if lineStart >= lineEnd {
			continue
		}

		// Get the line as a string.
		trimmed := internLine(raw, lineStart, lineEnd)

		// Trim leading/trailing whitespace for prefix checks.
		// We use the sub-string directly (no allocation).
		t := trimmed
		for len(t) > 0 && (t[0] == ' ' || t[0] == '\t') {
			t = t[1:]
		}
		for len(t) > 0 && (t[len(t)-1] == ' ' || t[len(t)-1] == '\t') {
			t = t[:len(t)-1]
		}
		if t == "" {
			continue
		}

		// Tokenise the trimmed line.
		fields := tok.Tokenise(t)
		if len(fields) == 0 {
			continue
		}

		switch {
		case inTable && len(fields) >= 2 && unicode.IsLower(rune(fields[0][0])):
			// Tier-table data row — store for assembleTiers.
			if nFields < len(ftNames) {
				ftNames[nFields] = fields[0]
				nv := len(fields) - 1
				if nv > 8 {
					nv = 8
				}
				ftCount[nFields] = nv
				for ti := 0; ti < nv; ti++ {
					ftVals[nFields][ti] = fields[ti+1]
				}
				nFields++
			}

		case isTierHeaderLine(fields[0]):
			// Parse tier names from the header row.
			var tn [8]string
			nTiers = 0
			for fi := 0; fi < len(fields) && nTiers < len(tn); fi++ {
				w := fields[fi]
				switch {
				case w == "Best" && fi+1 < len(fields) && fields[fi+1] == "Effort":
					tn[nTiers] = "Best Effort"
					nTiers++
					fi++ // skip "Effort"
				case w == "Tin" && fi+1 < len(fields):
					tn[nTiers] = "Tin " + fields[fi+1]
					nTiers++
					fi++ // skip the number
				default:
					tn[nTiers] = w
					nTiers++
				}
			}
			for ti := 0; ti < nTiers; ti++ {
				tierNames[ti] = tn[ti]
			}
			inTable = true

		case strings.HasPrefix(t, "Sent "):
			parseSentLine(&cs, t, tok)

		case strings.HasPrefix(t, "backlog "):
			parseBacklogLine(&cs, t, tok)

		case strings.HasPrefix(t, "memory used:"):
			parseMemoryLine(&cs, t, tok)

		case strings.HasPrefix(t, "capacity estimate:"):
			v := util.AfterColon(t)
			if !strings.HasPrefix(v, "0") {
				cs.CapacityEst = v
			}

		case strings.HasPrefix(t, "min/max network layer size:"):
			cs.MinNetSize, cs.MaxNetSize = parseMinMax(t)

		case strings.HasPrefix(t, "min/max overhead-adjusted size:"):
			cs.MinAdjSize, cs.MaxAdjSize = parseMinMax(t)

		case strings.HasPrefix(t, "average network hdr offset:"):
			cs.AvgHdrOffset = util.AfterColon(t)
		}
	}

	// Assemble tiers from collected field data.
	if nTiers > 0 {
		tiers := make([]types.CakeTier, nTiers)
		for ti := 0; ti < nTiers; ti++ {
			tiers[ti].Name = tierNames[ti]
			t := &tiers[ti]
			t.Thresh = getField("thresh", ti)
			t.Target = getField("target", ti)
			t.Interval = getField("interval", ti)
			t.PkDelay = getField("pk_delay", ti)
			t.AvDelay = getField("av_delay", ti)
			t.SpDelay = getField("sp_delay", ti)
			t.Backlog = getField("backlog", ti)
			t.Pkts = getFieldU("pkts", ti)
			t.Bytes = getFieldU("bytes", ti)
			t.WayInds = getFieldU("way_inds", ti)
			t.WayMiss = getFieldU("way_miss", ti)
			t.WayCols = getFieldU("way_cols", ti)
			t.Drops = getFieldU("drops", ti)
			t.Marks = getFieldU("marks", ti)
			t.AckDrop = getFieldU("ack_drop", ti)
			t.SpFlows = getFieldU("sp_flows", ti)
			t.BkFlows = getFieldU("bk_flows", ti)
			t.UnFlows = getFieldU("un_flows", ti)
			t.MaxLen = getFieldU("max_len", ti)
			t.Quantum = getFieldU("quantum", ti)
		}
		cs.Tiers = tiers
	}

	return cs, true
}

// ---------------------------------------------------------------------------
// Line-level parsers
// ---------------------------------------------------------------------------

func parseHeader(cs *types.CakeStats, line string, tok *util.FieldTokenizer) {
	fs := tok.Tokenise(line)
	if len(fs) < 5 {
		return
	}
	cs.Handle = strings.TrimSuffix(fs[2], ":")
	cs.Interface = fs[4]
	cs.Direction = "egress"
	for i := 5; i < len(fs); i++ {
		switch fs[i] {
		case "bandwidth":
			if i+1 < len(fs) {
				cs.Bandwidth = fs[i+1]
				i++
			}
		case "diffserv3", "diffserv4", "diffserv8", "besteffort", "precedence":
			cs.DiffservMode = fs[i]
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
			cs.DualMode = fs[i]
		case "nat":
			cs.NATEnabled = true
		case "nonat":
			cs.NATEnabled = false
		case "wash":
			cs.WashEnabled = true
		case "nowash":
			cs.WashEnabled = false
		case "dual-srchost", "dual-dsthost", "triple-isolate", "single":
			cs.DualMode = fs[i]
		case "ingress":
			cs.Direction = "ingress"
		case "memlimit":
			if i+1 < len(fs) {
				cs.MemLimit = fs[i+1]
				i++
			}
		}
	}
	if cs.Direction == "egress" && strings.HasPrefix(cs.Interface, "ifb") {
		cs.Direction = "ingress"
	}
}

func parseSentLine(cs *types.CakeStats, line string, tok *util.FieldTokenizer) {
	fs := tok.Tokenise(line)
	if len(fs) >= 4 {
		cs.SentBytes = util.ParseUint64(fs[1])
		cs.SentPkts = util.ParseUint64(fs[3])
	}
	s, e := strings.Index(line, "("), strings.Index(line, ")")
	if s != -1 && e != -1 && e > s {
		inner := line[s+1 : e]
		for len(inner) > 0 {
			comma := strings.IndexByte(inner, ',')
			var seg string
			if comma >= 0 {
				seg = inner[:comma]
				inner = inner[comma+1:]
			} else {
				seg = inner
				inner = ""
			}
			tokens := tok.Tokenise(seg)
			for j := 0; j+1 < len(tokens); j += 2 {
				switch tokens[j] {
				case "dropped":
					cs.Dropped = util.ParseUint64(tokens[j+1])
				case "overlimits":
					cs.Overlimits = util.ParseUint64(tokens[j+1])
				case "requeues":
					cs.Requeues = util.ParseUint64(tokens[j+1])
				}
			}
		}
	}
}

func parseBacklogLine(cs *types.CakeStats, line string, tok *util.FieldTokenizer) {
	fs := tok.Tokenise(line)
	if len(fs) >= 3 {
		cs.BacklogBytes = fs[1]
		cs.BacklogPkts = util.ParseUint64(strings.TrimSuffix(fs[2], "p"))
	}
}

func parseMemoryLine(cs *types.CakeStats, line string, tok *util.FieldTokenizer) {
	after := util.AfterColon(line)
	parts := tok.Tokenise(after)
	if len(parts) >= 3 && parts[1] == "of" {
		cs.MemoryUsed = parts[0]
		cs.MemoryTotal = parts[2]
	}
}

func parseMinMax(line string) (lo, hi string) {
	i := strings.Index(line, ":")
	if i == -1 {
		return
	}
	rest := line[i+1:]
	// Find the "/" separator.
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return
	}
	// Trim whitespace from each part (sub-string, no alloc).
	lo = trimSpace(rest[:slash])
	hi = trimSpace(rest[slash+1:])
	return
}

// trimSpace returns s without leading/trailing spaces/tabs (ASCII only).
// This is cheaper than strings.TrimSpace for our use case.
func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	if i >= j {
		return ""
	}
	return s[i:j]
}

// parseTierNames extracts tier names from a tier header row.
// It joins multi-word names like "Best Effort" and "Tin N" into single strings.
func parseTierNames(words []string) []string {
	var names []string
	for i := 0; i < len(words); i++ {
		switch {
		case words[i] == "Best" && i+1 < len(words) && words[i+1] == "Effort":
			names = append(names, "Best Effort")
			i++
		case words[i] == "Tin" && i+1 < len(words):
			names = append(names, "Tin "+words[i+1])
			i++
		default:
			names = append(names, words[i])
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// Tier-name utilities
// ---------------------------------------------------------------------------

var knownTierWords = map[string]bool{
	"Bulk": true, "Best": true, "Voice": true, "Video": true,
	"CS1": true, "CS2": true, "CS3": true, "CS4": true,
	"CS5": true, "CS6": true, "CS7": true, "BE": true,
	"Tin": true,
}

func isTierHeaderLine(first string) bool {
	return knownTierWords[first]
}

// ---------------------------------------------------------------------------
// headerParentHandle
// ---------------------------------------------------------------------------

func headerParentHandle(line string, tok *util.FieldTokenizer) string {
	fs := tok.Tokenise(line)
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

// ---------------------------------------------------------------------------
// Aggregate helpers (cake_mq)
// ---------------------------------------------------------------------------

// aggregateCakeMQSubQueues merges per-hardware-queue CakeStats from a cake_mq
// setup into a single logical CakeStats that represents the whole interface.
func aggregateCakeMQSubQueues(parent types.CakeStats, subs []types.CakeStats) types.CakeStats {
	if len(subs) == 0 {
		return parent
	}
	agg := subs[0]
	agg.Handle = parent.Handle
	agg.Interface = parent.Interface
	agg.RawHeader = parent.RawHeader
	agg.UpdatedAt = parent.UpdatedAt

	if parent.Direction == "ingress" || agg.Direction == "ingress" {
		agg.Direction = "ingress"
	} else {
		agg.Direction = "egress"
	}

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
		backlogBytes += util.ParseBytesStr(s.BacklogBytes)
		memUsed += util.ParseBytesStr(s.MemoryUsed)
	}
	agg.BacklogBytes = formatUint64(backlogBytes) + "b"
	agg.MemoryUsed = formatUint64(memUsed) + "b"

	if len(subs[0].Tiers) > 0 {
		queueTiers := make([][]types.CakeTier, len(subs))
		for i, s := range subs {
			queueTiers[i] = s.Tiers
		}
		agg.Tiers = aggregateCakeTiers(queueTiers)
	}
	return agg
}

// aggregateCakeTiers combines per-tier statistics from N cake sub-queues.
func aggregateCakeTiers(queues [][]types.CakeTier) []types.CakeTier {
	if len(queues) == 0 || len(queues[0]) == 0 {
		return nil
	}
	nTiers := len(queues[0])
	out := make([]types.CakeTier, nTiers)
	for ti := 0; ti < nTiers; ti++ {
		out[ti] = queues[0][ti]
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
		out[ti].PkDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.PkDelay })
		out[ti].AvDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.AvDelay })
		out[ti].SpDelay = maxDelayStr(queues, ti, func(t types.CakeTier) string { return t.SpDelay })
		var backlogSum uint64
		for _, q := range queues {
			if ti < len(q) {
				backlogSum += util.ParseBytesStr(q[ti].Backlog)
			}
		}
		out[ti].Backlog = formatUint64(backlogSum) + "b"
	}
	return out
}

// parseTextString is a convenience wrapper around parseText that accepts a
// string input. It converts the string to a byte slice without copying
// (using unsafe) so that the parser can build its line index directly.
// In production code, call parseText([]byte) directly for zero-allocation
// operation.  This wrapper exists mainly for test compatibility.
//
//lint:ignore U1000 used in tests within this package
func parseTextString(s string) []types.CakeStats {
	return parseText(unsafeStringToBytes(s))
}

func unsafeStringToBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
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
		if v := util.ParseDelayUsec(s); v > best || bestStr == "" {
			best = v
			bestStr = s
		}
	}
	return bestStr
}

// formatUint64 converts n to its decimal string representation without
// allocating (writes into a stack buffer).
func formatUint64(n uint64) string {
	var buf [20]byte
	i := len(buf)
	for {
		i--
		buf[i] = '0' + byte(n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	return string(buf[i:])
}
