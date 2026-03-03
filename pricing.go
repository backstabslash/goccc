package main

import (
	"sort"
	"strings"
)

const (
	longCtxThreshold       = 200_000
	webSearchCostPerSearch = 0.01
)

// cacheWriteAs1h treats all cache writes as 1-hour tier (2x input price).
// Claude Code JSONL logs report all cache writes as ephemeral_5m, but
// Anthropic billing matches 1-hour pricing. Default true.
var cacheWriteAs1h = true

type ModelPricing struct {
	Input         float64
	Output        float64
	LongCtxInput  float64 // 0 = no long-context pricing for this model
	LongCtxOutput float64
}

func (p ModelPricing) CacheWrite5m() float64 { return p.Input * 1.25 }
func (p ModelPricing) CacheWrite1h() float64 { return p.Input * 2.0 }
func (p ModelPricing) CacheRead() float64    { return p.Input * 0.1 }

func (p ModelPricing) LongCtxCacheWrite5m() float64 { return p.LongCtxInput * 1.25 }
func (p ModelPricing) LongCtxCacheWrite1h() float64 { return p.LongCtxInput * 2.0 }
func (p ModelPricing) LongCtxCacheRead() float64    { return p.LongCtxInput * 0.1 }

// Source: https://platform.claude.com/docs/en/about-claude/pricing
var pricingTable = map[string]ModelPricing{
	"claude-opus-4-6":            {Input: 5.00, Output: 25.00, LongCtxInput: 10.00, LongCtxOutput: 37.50},
	"claude-opus-4-5-20251101":   {Input: 5.00, Output: 25.00},
	"claude-opus-4-1-20250414":   {Input: 15.00, Output: 75.00},
	"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00, LongCtxInput: 6.00, LongCtxOutput: 22.50},
	"claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00, LongCtxInput: 6.00, LongCtxOutput: 22.50},
	"claude-sonnet-4-20250514":   {Input: 3.00, Output: 15.00, LongCtxInput: 6.00, LongCtxOutput: 22.50},
	"claude-haiku-4-5-20251001":  {Input: 1.00, Output: 5.00},
	"claude-haiku-3-5-20241022":  {Input: 0.80, Output: 4.00},
}

// Sorted longest-first at init for correct prefix matching.
var familyPrefixes = []struct {
	Prefix string
	Key    string
}{
	{"claude-opus-4-6", "claude-opus-4-6"},
	{"claude-opus-4-5", "claude-opus-4-5-20251101"},
	{"claude-opus-4-1", "claude-opus-4-1-20250414"},
	{"claude-opus-4", "claude-opus-4-1-20250414"},
	{"claude-sonnet-4-6", "claude-sonnet-4-6"},
	{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"},
	{"claude-sonnet-4", "claude-sonnet-4-20250514"},
	{"claude-sonnet-3", "claude-sonnet-4-5-20250929"},
	{"claude-haiku-4-5", "claude-haiku-4-5-20251001"},
	{"claude-haiku-3-5", "claude-haiku-3-5-20241022"},
	{"claude-haiku-3", "claude-haiku-3-5-20241022"},
}

func init() {
	sort.Slice(familyPrefixes, func(i, j int) bool {
		return len(familyPrefixes[i].Prefix) > len(familyPrefixes[j].Prefix)
	})
}

var defaultPricing = pricingTable["claude-sonnet-4-6"]

func resolvePricing(model string) ModelPricing {
	if p, ok := pricingTable[model]; ok {
		return p
	}
	for _, fp := range familyPrefixes {
		if strings.HasPrefix(model, fp.Prefix) {
			return pricingTable[fp.Key]
		}
	}
	return defaultPricing
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
	if cache5m == 0 && cache1h == 0 && u.CacheCreationInputTokens > 0 {
		cache5m = u.CacheCreationInputTokens
	}
	if cacheWriteAs1h {
		cache1h += cache5m
		cache5m = 0
	}
	return
}

type CostResult struct {
	Cost        float64
	LongCtx     bool
	WebSearches int
}

func calcCostResult(model string, usage Usage) CostResult {
	p := resolvePricing(model)
	const mtok = 1_000_000.0
	cache5m, cache1h := usage.CacheWriteTokens()

	longCtx := p.LongCtxInput > 0 && usage.TotalInputTokens() > longCtxThreshold

	var tokenCost float64
	if longCtx {
		tokenCost = (float64(usage.InputTokens)/mtok)*p.LongCtxInput +
			(float64(usage.OutputTokens)/mtok)*p.LongCtxOutput +
			(float64(cache5m)/mtok)*p.LongCtxCacheWrite5m() +
			(float64(cache1h)/mtok)*p.LongCtxCacheWrite1h() +
			(float64(usage.CacheReadInputTokens)/mtok)*p.LongCtxCacheRead()
	} else {
		tokenCost = (float64(usage.InputTokens)/mtok)*p.Input +
			(float64(usage.OutputTokens)/mtok)*p.Output +
			(float64(cache5m)/mtok)*p.CacheWrite5m() +
			(float64(cache1h)/mtok)*p.CacheWrite1h() +
			(float64(usage.CacheReadInputTokens)/mtok)*p.CacheRead()
	}

	ws := usage.WebSearches()
	return CostResult{
		Cost:        tokenCost + float64(ws)*webSearchCostPerSearch,
		LongCtx:     longCtx,
		WebSearches: ws,
	}
}

func calcCost(model string, usage Usage) float64 {
	return calcCostResult(model, usage).Cost
}

func shortModel(model string) string {
	m := strings.TrimPrefix(strings.ToLower(model), "claude-")
	switch {
	case strings.HasPrefix(m, "opus-4-6"):
		return "Opus 4.6"
	case strings.HasPrefix(m, "opus-4-5"):
		return "Opus 4.5"
	case strings.HasPrefix(m, "opus-4-1"):
		return "Opus 4.1"
	case strings.HasPrefix(m, "opus-4"):
		return "Opus 4"
	case strings.HasPrefix(m, "opus-3"):
		return "Opus 3"
	case strings.HasPrefix(m, "sonnet-4-6"):
		return "Sonnet 4.6"
	case strings.HasPrefix(m, "sonnet-4-5"):
		return "Sonnet 4.5"
	case strings.HasPrefix(m, "sonnet-4"):
		return "Sonnet 4"
	case strings.HasPrefix(m, "sonnet-3"):
		return "Sonnet 3.x"
	case strings.HasPrefix(m, "haiku-4-5"):
		return "Haiku 4.5"
	case strings.HasPrefix(m, "haiku-3-5"):
		return "Haiku 3.5"
	case strings.HasPrefix(m, "haiku-3"):
		return "Haiku 3"
	default:
		return model
	}
}
