package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	longCtxThreshold       = 200_000
	webSearchCostPerSearch = 0.01
)

var cacheWriteAs1h = true

type ModelPricing struct {
	Input         float64 `json:"input"`
	Output        float64 `json:"output"`
	LongCtxInput  float64 `json:"long_ctx_input,omitempty"`
	LongCtxOutput float64 `json:"long_ctx_output,omitempty"`
}

func (p ModelPricing) CacheWrite5m() float64 { return p.Input * 1.25 }
func (p ModelPricing) CacheWrite1h() float64 { return p.Input * 2.0 }
func (p ModelPricing) CacheRead() float64    { return p.Input * 0.1 }

func (p ModelPricing) LongCtxCacheWrite5m() float64 { return p.LongCtxInput * 1.25 }
func (p ModelPricing) LongCtxCacheWrite1h() float64 { return p.LongCtxInput * 2.0 }
func (p ModelPricing) LongCtxCacheRead() float64    { return p.LongCtxInput * 0.1 }

//go:embed pricing.json
var embeddedPricingJSON []byte

type PricingData struct {
	Models       map[string]ModelPricing `json:"models"`
	Families     []PricingFamily         `json:"families"`
	DefaultModel string                  `json:"default_model"`
	DisplayNames []PricingDisplayName    `json:"display_names"`
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
	pricingTable   map[string]ModelPricing
	familyPrefixes []PricingFamily
	defaultPricing ModelPricing
	displayNames   []PricingDisplayName
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

func applyPricing(pd *PricingData) {
	pricingTable = pd.Models
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
}

func initPricing() {
	cached := pricingCachePath()
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

func resolvePricing(model string) ModelPricing {
	if p, ok := pricingTable[model]; ok {
		return p
	}
	for _, fp := range familyPrefixes {
		if strings.HasPrefix(model, fp.Prefix) {
			if p, ok := pricingTable[fp.Model]; ok {
				return p
			}
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
	for _, dn := range displayNames {
		if strings.HasPrefix(m, dn.Prefix) {
			return dn.Name
		}
	}
	return model
}
