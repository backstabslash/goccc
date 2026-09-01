package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PriceFields struct {
	Input               float64 `json:"input"`
	Output              float64 `json:"output"`
	CacheRead           float64 `json:"cache_read,omitempty"`
	CacheWrite5m        float64 `json:"cache_write_5m,omitempty"`
	CacheWrite1h        float64 `json:"cache_write_1h,omitempty"`
	LongCtxInput        float64 `json:"long_ctx_input,omitempty"`
	LongCtxOutput       float64 `json:"long_ctx_output,omitempty"`
	LongCtxCacheRead    float64 `json:"long_ctx_cache_read,omitempty"`
	LongCtxCacheWrite5m float64 `json:"long_ctx_cache_write_5m,omitempty"`
	LongCtxCacheWrite1h float64 `json:"long_ctx_cache_write_1h,omitempty"`
}

type ModelPricing struct {
	PriceFields
	Schedule []PriceChange `json:"schedule,omitempty"`
}

type PriceChange struct {
	From string `json:"from"`
	PriceFields
	parsedFrom time.Time // From resolved to midnight UTC at load; what resolution compares
}

//go:embed pricing.json
var embeddedPricingJSON []byte

type PricingData struct {
	Models                  map[string]ModelPricing `json:"models"`
	FastModels              map[string]ModelPricing `json:"fast_models,omitempty"`
	Families                []PricingFamily         `json:"families"`
	DefaultModel            string                  `json:"default_model"`
	DisplayNames            []PricingDisplayName    `json:"display_names"`
	LongContextThreshold    int                     `json:"long_context_threshold,omitempty"`
	WebSearchCost           float64                 `json:"web_search_cost,omitempty"`
	InferenceGeoMultipliers map[string]float64      `json:"inference_geo_multipliers,omitempty"`
}

type PricingFamily struct {
	Prefix string `json:"prefix"`
	Model  string `json:"model"`
}

type PricingDisplayName struct {
	Prefix string `json:"prefix"`
	Name   string `json:"name"`
}

var (
	pricingTable            map[string]ModelPricing
	fastPricingTable        map[string]ModelPricing
	familyPrefixes          []PricingFamily
	defaultPricing          ModelPricing
	displayNames            []PricingDisplayName
	longCtxThreshold        = 200_000
	webSearchCostPerSearch  = 0.01
	inferenceGeoMultipliers = map[string]float64{"us": 1.1}
)

var pricingCachePath = func() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "goccc", "pricing.json")
}

func loadPricingFrom(data []byte) (*PricingData, error) {
	var pd PricingData
	if err := json.Unmarshal(data, &pd); err != nil {
		return nil, err
	}
	if len(pd.Models) == 0 {
		return nil, fmt.Errorf("pricing data has no models")
	}
	return &pd, nil
}

func fillCacheDefaults(p *PriceFields) {
	if p.CacheRead == 0 && p.Input > 0 {
		p.CacheRead = p.Input * 0.1
	}
	if p.CacheWrite5m == 0 && p.Input > 0 {
		p.CacheWrite5m = p.Input * 1.25
	}
	if p.CacheWrite1h == 0 && p.Input > 0 {
		p.CacheWrite1h = p.Input * 2.0
	}
	if p.LongCtxCacheRead == 0 && p.LongCtxInput > 0 {
		p.LongCtxCacheRead = p.LongCtxInput * 0.1
	}
	if p.LongCtxCacheWrite5m == 0 && p.LongCtxInput > 0 {
		p.LongCtxCacheWrite5m = p.LongCtxInput * 1.25
	}
	if p.LongCtxCacheWrite1h == 0 && p.LongCtxInput > 0 {
		p.LongCtxCacheWrite1h = p.LongCtxInput * 2.0
	}
}

func inheritPrimaries(entry, base PriceFields) PriceFields {
	if entry.Input == 0 {
		entry.Input = base.Input
	}
	if entry.Output == 0 {
		entry.Output = base.Output
	}
	if entry.LongCtxInput == 0 {
		entry.LongCtxInput = base.LongCtxInput
	}
	if entry.LongCtxOutput == 0 {
		entry.LongCtxOutput = base.LongCtxOutput
	}
	return entry
}

// normalizePricing parses schedule dates and fills cache defaults. A bad From
// is dropped rather than fatal, matching the tool's best-effort pricing posture.
func normalizePricing(p ModelPricing) ModelPricing {
	fillCacheDefaults(&p.PriceFields)
	kept := p.Schedule[:0]
	for _, c := range p.Schedule {
		t, err := time.Parse("2006-01-02", c.From)
		if err != nil {
			fmt.Fprintf(os.Stderr, "goccc: warning: skipping pricing schedule entry with invalid from %q: %v\n", c.From, err)
			continue
		}
		c.PriceFields = inheritPrimaries(c.PriceFields, p.PriceFields)
		fillCacheDefaults(&c.PriceFields)
		c.parsedFrom = t
		kept = append(kept, c)
	}
	p.Schedule = kept
	return p
}

func applyPricing(pd *PricingData) {
	pricingTable = pd.Models
	for k, p := range pricingTable {
		pricingTable[k] = normalizePricing(p)
	}
	if pd.FastModels != nil {
		fastPricingTable = pd.FastModels
	} else {
		fastPricingTable = make(map[string]ModelPricing)
	}
	for k, p := range fastPricingTable {
		fastPricingTable[k] = normalizePricing(p)
	}
	familyPrefixes = pd.Families
	sort.Slice(familyPrefixes, func(i, j int) bool {
		return len(familyPrefixes[i].Prefix) > len(familyPrefixes[j].Prefix)
	})
	displayNames = pd.DisplayNames
	sort.Slice(displayNames, func(i, j int) bool {
		return len(displayNames[i].Prefix) > len(displayNames[j].Prefix)
	})
	if p, ok := pricingTable[pd.DefaultModel]; ok {
		defaultPricing = p
	}
	if pd.LongContextThreshold > 0 {
		longCtxThreshold = pd.LongContextThreshold
	}
	if pd.WebSearchCost > 0 {
		webSearchCostPerSearch = pd.WebSearchCost
	}
	if pd.InferenceGeoMultipliers != nil {
		inferenceGeoMultipliers = pd.InferenceGeoMultipliers
	}
}

// pricingRefreshDone is closed once the background refresh started by
// initPricing finishes. Short-lived modes (statusline, session-end) exit as
// soon as their work is done, which would kill the fire-and-forget fetch
// before it lands; they wait on this channel briefly so the update isn't lost.
var pricingRefreshDone chan struct{}

const pricingRefreshWait = 2 * time.Second

func initPricing() {
	cached := pricingCachePath()

	done := make(chan struct{})
	pricingRefreshDone = done
	defer func() {
		go func() {
			defer close(done)
			refreshPricingCache(cached)
		}()
	}()

	if cached != "" {
		if data, err := os.ReadFile(cached); err == nil {
			if pd, err := loadPricingFrom(data); err == nil {
				applyPricing(pd)
				return
			}
			fmt.Fprintf(os.Stderr, "goccc: warning: cached pricing.json invalid, using embedded\n")
		}
	}
	pd, err := loadPricingFrom(embeddedPricingJSON)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: embedded pricing.json is invalid: %v\n", err)
		os.Exit(1)
	}
	applyPricing(pd)
}

// waitForPricingRefresh blocks up to timeout for the background refresh to land.
// A fresh cache returns immediately, so this is effectively free on the common path.
func waitForPricingRefresh(timeout time.Duration) {
	if pricingRefreshDone == nil {
		return
	}
	select {
	case <-pricingRefreshDone:
	case <-time.After(timeout):
	}
}

// hasModelPrefix reports whether model starts with prefix at a version boundary.
// Date stamps start with "2", so a plain strings.HasPrefix lets a "-2" minor
// (claude-opus-5-2) swallow its own family's dated IDs (claude-opus-5-2026...).
func hasModelPrefix(model, prefix string) bool {
	if !strings.HasPrefix(model, prefix) {
		return false
	}
	rest := model[len(prefix):]
	return rest == "" || rest[0] == '-'
}

func resolveBaseModel(model string) (string, ModelPricing) {
	if p, ok := pricingTable[model]; ok {
		return model, p
	}
	for _, fp := range familyPrefixes {
		if !hasModelPrefix(model, fp.Prefix) {
			continue
		}
		pick := fp.Model
		if minorAfter(model, fp.Prefix) > minorAfter(fp.Model, fp.Prefix) {
			bestV := -1
			for k := range pricingTable {
				if !hasModelPrefix(k, fp.Prefix) {
					continue
				}
				if v := minorAfter(k, fp.Prefix); v > bestV {
					bestV, pick = v, k
				}
			}
		}
		if p, ok := pricingTable[pick]; ok {
			return pick, p
		}
	}
	return "", defaultPricing
}

func minorAfter(model, prefix string) int {
	rest := strings.TrimPrefix(strings.TrimPrefix(model, prefix), "-")
	if i := strings.IndexByte(rest, '-'); i >= 0 {
		rest = rest[:i]
	}
	if len(rest) >= 8 {
		return 0
	}
	n, _ := strconv.Atoi(rest)
	return n
}

// priceAt returns the price effective at ts — the schedule entry with the
// greatest From <= ts, else the base. A zero ts predates every From, so it
// yields the base price.
func (m ModelPricing) priceAt(ts time.Time) PriceFields {
	best := m.PriceFields
	var bestFrom time.Time
	for _, c := range m.Schedule {
		if ts.Before(c.parsedFrom) || !c.parsedFrom.After(bestFrom) {
			continue
		}
		best, bestFrom = c.PriceFields, c.parsedFrom
	}
	return best
}

func resolvePricing(model string, ts time.Time) ModelPricing {
	baseModel, isFast := strings.CutSuffix(model, ":fast")
	resolved, fallback := resolveBaseModel(baseModel)
	chosen := fallback
	if isFast && len(fastPricingTable) > 0 {
		if p, ok := fastPricingTable[resolved]; ok {
			chosen = p
		} else if p, ok := fastPricingTable[baseModel]; ok {
			chosen = p
		}
	}
	chosen.PriceFields = chosen.priceAt(ts)
	return chosen
}

type CacheCreation struct {
	Ephemeral5mInputTokens int `json:"ephemeral_5m_input_tokens"`
	Ephemeral1hInputTokens int `json:"ephemeral_1h_input_tokens"`
}

type ServerToolUse struct {
	WebSearchRequests int `json:"web_search_requests"`
}

type Usage struct {
	InputTokens              int            `json:"input_tokens"`
	OutputTokens             int            `json:"output_tokens"`
	CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
	CacheCreation            *CacheCreation `json:"cache_creation,omitempty"`
	ServerToolUse            *ServerToolUse `json:"server_tool_use,omitempty"`
	Speed                    string         `json:"speed,omitempty"`
	InferenceGeo             string         `json:"inference_geo,omitempty"`
	Iterations               []Usage        `json:"iterations,omitempty"`
}

// foldIterations raises each counter to its sum across usage.iterations. Logs
// so far carry one iteration equal to the top level; this guards against a
// top level that only reflects the last pass of a multi-iteration request.
func (u Usage) foldIterations() Usage {
	if len(u.Iterations) == 0 {
		return u
	}
	var sum Usage
	var cc CacheCreation
	for _, it := range u.Iterations {
		sum.InputTokens += it.InputTokens
		sum.OutputTokens += it.OutputTokens
		sum.CacheReadInputTokens += it.CacheReadInputTokens
		flat := it.CacheCreationInputTokens
		if it.CacheCreation != nil {
			cc.Ephemeral5mInputTokens += it.CacheCreation.Ephemeral5mInputTokens
			cc.Ephemeral1hInputTokens += it.CacheCreation.Ephemeral1hInputTokens
			flat = max(flat, it.CacheCreation.Ephemeral5mInputTokens+it.CacheCreation.Ephemeral1hInputTokens)
		}
		sum.CacheCreationInputTokens += flat
	}
	u.InputTokens = max(u.InputTokens, sum.InputTokens)
	u.OutputTokens = max(u.OutputTokens, sum.OutputTokens)
	u.CacheReadInputTokens = max(u.CacheReadInputTokens, sum.CacheReadInputTokens)
	if sum.CacheCreationInputTokens > u.CacheCreationInputTokens {
		u.CacheCreationInputTokens = sum.CacheCreationInputTokens
		u.CacheCreation = nil
		if cc != (CacheCreation{}) {
			u.CacheCreation = &cc
		}
	}
	u.Iterations = nil
	return u
}

func (u Usage) TotalInputTokens() int {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens
}

func (u Usage) WebSearches() int {
	if u.ServerToolUse != nil {
		return u.ServerToolUse.WebSearchRequests
	}
	return 0
}

func (u Usage) CacheWriteTokens() (cache5m, cache1h int) {
	if u.CacheCreation != nil {
		cache5m = u.CacheCreation.Ephemeral5mInputTokens
		cache1h = u.CacheCreation.Ephemeral1hInputTokens
	}
	// Fallback for old logs without cache_creation sub-object: default to 1h
	if cache5m == 0 && cache1h == 0 && u.CacheCreationInputTokens > 0 {
		cache1h = u.CacheCreationInputTokens
	}
	return
}

type CostResult struct {
	Cost        float64
	LongCtx     bool
	WebSearches int
}

func calcCostResult(model string, usage Usage, ts time.Time) CostResult {
	p := resolvePricing(model, ts)
	const mtok = 1_000_000.0
	cache5m, cache1h := usage.CacheWriteTokens()

	longCtx := p.LongCtxInput > 0 && usage.TotalInputTokens() > longCtxThreshold

	var tokenCost float64
	if longCtx {
		tokenCost = (float64(usage.InputTokens)/mtok)*p.LongCtxInput +
			(float64(usage.OutputTokens)/mtok)*p.LongCtxOutput +
			(float64(cache5m)/mtok)*p.LongCtxCacheWrite5m +
			(float64(cache1h)/mtok)*p.LongCtxCacheWrite1h +
			(float64(usage.CacheReadInputTokens)/mtok)*p.LongCtxCacheRead
	} else {
		tokenCost = (float64(usage.InputTokens)/mtok)*p.Input +
			(float64(usage.OutputTokens)/mtok)*p.Output +
			(float64(cache5m)/mtok)*p.CacheWrite5m +
			(float64(cache1h)/mtok)*p.CacheWrite1h +
			(float64(usage.CacheReadInputTokens)/mtok)*p.CacheRead
	}

	if m, ok := inferenceGeoMultipliers[usage.InferenceGeo]; ok {
		tokenCost *= m
	}

	ws := usage.WebSearches()
	return CostResult{
		Cost:        tokenCost + float64(ws)*webSearchCostPerSearch,
		LongCtx:     longCtx,
		WebSearches: ws,
	}
}

func calcCost(model string, usage Usage, ts time.Time) float64 {
	return calcCostResult(model, usage, ts).Cost
}

const fastMarker = " ⚡"

func shortModel(model string) string {
	base, isFast := strings.CutSuffix(model, ":fast")
	// Claude Code's statusline reports 1M-context aliases as "claude-opus-5[1m]"; drop it.
	base, _, _ = strings.Cut(base, "[")
	m := strings.TrimPrefix(strings.ToLower(base), "claude-")
	for _, dn := range displayNames {
		if !hasModelPrefix(m, dn.Prefix) {
			continue
		}
		name := dn.Name
		if isFast {
			name += fastMarker
		}
		return name
	}
	return model
}
