package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEmbeddedPricingLoads(t *testing.T) {
	var data PricingData
	if err := json.Unmarshal(embeddedPricingJSON, &data); err != nil {
		t.Fatalf("embedded pricing.json failed to parse: %v", err)
	}
	if len(data.Models) == 0 {
		t.Fatal("embedded pricing.json has no models")
	}
	if data.DefaultModel == "" {
		t.Fatal("embedded pricing.json has no default_model")
	}
	if _, ok := data.Models[data.DefaultModel]; !ok {
		t.Fatalf("default_model %q not found in models", data.DefaultModel)
	}
}

func TestInitPricingUsesCachedFile(t *testing.T) {
	cachedData := `{
		"models": {
			"claude-test-model": { "input": 99.00, "output": 99.00 }
		},
		"families": [],
		"default_model": "claude-test-model",
		"display_names": [
			{ "prefix": "test-model", "name": "Test Model" }
		],
		"long_context_threshold": 100000,
		"web_search_cost": 0.05
	}`

	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "pricing.json")
	_ = os.WriteFile(cacheFile, []byte(cachedData), 0o644)

	origCachePath := pricingCachePath
	pricingCachePath = func() string { return cacheFile }
	defer func() {
		pricingCachePath = origCachePath
		initPricing()
	}()

	initPricing()

	p := resolvePricing("claude-test-model")
	if p.Input != 99.0 {
		t.Errorf("expected cached input=99.0, got %f", p.Input)
	}
	// Cache fields should be filled from multipliers when absent in JSON
	assertCost(t, "fallback cache_read", p.CacheRead, 9.9)
	assertCost(t, "fallback cache_write_5m", p.CacheWrite5m, 123.75)
	assertCost(t, "fallback cache_write_1h", p.CacheWrite1h, 198.0)
	// Global settings should be applied from JSON
	if longCtxThreshold != 100000 {
		t.Errorf("expected longCtxThreshold=100000, got %d", longCtxThreshold)
	}
	if webSearchCostPerSearch != 0.05 {
		t.Errorf("expected webSearchCostPerSearch=0.05, got %f", webSearchCostPerSearch)
	}
	if name := shortModel("claude-test-model"); name != "Test Model" {
		t.Errorf("expected display name 'Test Model', got %q", name)
	}
}

func TestInitPricingFallsBackToEmbedded(t *testing.T) {
	origCachePath := pricingCachePath
	pricingCachePath = func() string { return "" }
	defer func() {
		pricingCachePath = origCachePath
		initPricing()
	}()

	initPricing()

	p := resolvePricing("claude-opus-4-6")
	if p.Input != 5.0 {
		t.Errorf("expected embedded input=5.0, got %f", p.Input)
	}
}

func TestResolvePricingExactMatch(t *testing.T) {
	p := resolvePricing("claude-opus-4-6")
	if p.Input != 5.0 {
		t.Errorf("expected input=5.0, got %f", p.Input)
	}
	if p.Output != 25.0 {
		t.Errorf("expected output=25.0, got %f", p.Output)
	}
}

func TestResolvePricingFamilyPrefix(t *testing.T) {
	p := resolvePricing("claude-opus-4-5-20260101")
	if p.Input != 5.0 {
		t.Errorf("expected input=5.0 (opus-4-5 pricing), got %f", p.Input)
	}
}

func TestResolvePricingFallback(t *testing.T) {
	p := resolvePricing("claude-unknown-99")
	if p.Input != 3.0 {
		t.Errorf("expected fallback input=3.0, got %f", p.Input)
	}
}

// A future versioned model (higher than any known) should fall back to the
// newest model in its family, not the oldest.
func TestResolvePricingUnknownFutureVersion(t *testing.T) {
	withEmbeddedPricing(t)
	// Hypothetical Opus 4.9 — should price like the latest known Opus 4.x ($5/$25),
	// not like Opus 4.1 ($15/$75) or Opus 4 ($15/$75).
	p := resolvePricing("claude-opus-4-9-20270101")
	if p.Input != 5.0 {
		t.Errorf("expected input=5.0 (newest opus-4.x fallback), got %f", p.Input)
	}
	if p.Output != 25.0 {
		t.Errorf("expected output=25.0, got %f", p.Output)
	}
}

// Original Opus 4 (no minor version, just a date stamp) must still resolve to
// Opus 4.1 pricing — its version is lower than any 4.x in the table.
func TestResolvePricingOriginalOpus4(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-opus-4-20250514")
	if p.Input != 15.0 {
		t.Errorf("expected input=15.0 (opus 4 original), got %f", p.Input)
	}
	if p.Output != 75.0 {
		t.Errorf("expected output=75.0, got %f", p.Output)
	}
}

// Future Sonnet 4.7 should fall back to the newest Sonnet 4.x pricing.
func TestResolvePricingUnknownFutureSonnet(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-sonnet-4-9-20270101")
	if p.Input != 3.0 {
		t.Errorf("expected input=3.0 (sonnet-4.x fallback), got %f", p.Input)
	}
}

func TestCalcCostBasic(t *testing.T) {
	usage := Usage{
		InputTokens:              100_000,
		OutputTokens:             100_000,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
	}
	cost := calcCost("claude-opus-4-6", usage)
	assertCost(t, "basic opus cost", cost, 3.0)
}

func TestCalcCostWithCache(t *testing.T) {
	// Fallback path: no cache_creation sub-object → defaults to 1h tier
	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     100_000,
		CacheCreationInputTokens: 100_000,
	}
	cost := calcCost("claude-opus-4-6", usage)
	// 100K cache read @ 0.1x ($5 input) = $0.05, 100K cache write 1h @ 2x = $1.00
	assertCost(t, "cache cost", cost, 1.05)
}

func TestCalcCostWithCacheBreakdown(t *testing.T) {
	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 100_000,
		CacheCreation: &CacheCreation{
			Ephemeral5mInputTokens: 50_000,
			Ephemeral1hInputTokens: 50_000,
		},
	}
	cost := calcCost("claude-opus-4-6", usage)
	// 50K @ 5m tier (1.25x $5) = $0.3125, 50K @ 1h tier (2x $5) = $0.50
	assertCost(t, "cache breakdown cost", cost, 0.8125)
}

func TestCalcCostWebSearch(t *testing.T) {
	usage := Usage{
		InputTokens:   100_000,
		OutputTokens:  10_000,
		ServerToolUse: &ServerToolUse{WebSearchRequests: 5},
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	assertCost(t, "web search total", cr.Cost, 0.80)
	if cr.WebSearches != 5 {
		t.Errorf("web searches = %d, want 5", cr.WebSearches)
	}
	if cr.LongCtx {
		t.Error("expected LongCtx = false")
	}
}

func TestCalcCostLongContextStandard(t *testing.T) {
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         1_000_000,
		CacheReadInputTokens: 50_000,
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	assertCost(t, "standard context", cr.Cost, 25.525)
	if cr.LongCtx {
		t.Error("expected LongCtx = false for <200K")
	}
}

func TestCalcCostLongContextPremium(t *testing.T) {
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         1_000_000,
		CacheReadInputTokens: 150_000,
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	// Opus 4.6 has flat pricing across full 1M window (no long ctx surcharge)
	assertCost(t, "opus flat rate >200K", cr.Cost, 25.575)
	if cr.LongCtx {
		t.Error("expected LongCtx = false (opus 4.6 has no long ctx pricing)")
	}
}

func TestCalcCostLongContextSonnet(t *testing.T) {
	usage := Usage{
		InputTokens:              50_000,
		OutputTokens:             500_000,
		CacheReadInputTokens:     200_000,
		CacheCreationInputTokens: 10_000,
		CacheCreation:            &CacheCreation{Ephemeral1hInputTokens: 10_000},
	}
	cr := calcCostResult("claude-sonnet-4-6", usage)
	// Sonnet 4.6 has flat pricing across full 1M window (no long ctx surcharge)
	// 50K input @ $3 = $0.15, 500K output @ $15 = $7.50, 200K cache read @ 0.1x = $0.06, 10K cache write 1h @ 2x = $0.06
	assertCost(t, "sonnet flat rate >200K", cr.Cost, 7.77)
	if cr.LongCtx {
		t.Error("expected LongCtx = false (sonnet 4.6 has no long ctx pricing)")
	}
}

func TestCalcCostLongContextNoModel(t *testing.T) {
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         100_000,
		CacheReadInputTokens: 200_000,
	}
	cr := calcCostResult("claude-haiku-4-5-20251001", usage)
	assertCost(t, "haiku no long ctx", cr.Cost, 0.62)
	if cr.LongCtx {
		t.Error("expected LongCtx = false for haiku (no premium)")
	}
}

func TestCalcCostCacheTiersSeparate(t *testing.T) {
	// Haiku: all cache writes at 5m tier
	usage := Usage{
		CacheCreationInputTokens: 100_000,
		CacheCreation:            &CacheCreation{Ephemeral5mInputTokens: 100_000},
	}
	cost := calcCost("claude-haiku-4-5-20251001", usage)
	// 100K @ 5m tier (1.25x $1) = $0.125
	assertCost(t, "haiku 5m cache", cost, 0.125)

	// Opus: all cache writes at 1h tier
	usage2 := Usage{
		CacheCreationInputTokens: 100_000,
		CacheCreation:            &CacheCreation{Ephemeral1hInputTokens: 100_000},
	}
	cost2 := calcCost("claude-opus-4-6", usage2)
	// 100K @ 1h tier (2x $5) = $1.00
	assertCost(t, "opus 1h cache", cost2, 1.0)
}

func withEmbeddedPricing(t *testing.T) {
	t.Helper()
	origCachePath := pricingCachePath
	pricingCachePath = func() string { return "" }
	initPricing()
	t.Cleanup(func() {
		pricingCachePath = origCachePath
		initPricing()
	})
}

func TestWaitForPricingRefresh(t *testing.T) {
	orig := pricingRefreshDone
	t.Cleanup(func() { pricingRefreshDone = orig })

	closed := make(chan struct{})
	close(closed)
	pricingRefreshDone = closed
	start := time.Now()
	waitForPricingRefresh(time.Second)
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("closed channel: returned after %v, want near-instant", elapsed)
	}

	pricingRefreshDone = make(chan struct{}) // never closed
	start = time.Now()
	waitForPricingRefresh(50 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("open channel: returned after %v, want >= timeout", elapsed)
	}
}

func TestResolvePricingFastMode(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-opus-4-6:fast")
	if p.Input != 30.0 {
		t.Errorf("expected fast input=30.0, got %f", p.Input)
	}
	if p.Output != 150.0 {
		t.Errorf("expected fast output=150.0, got %f", p.Output)
	}
}

// Opus 4.8: its fast rate ($10/$50) is lower than older Opus fast tiers ($30/$150).
func TestResolvePricingLatestOpus(t *testing.T) {
	withEmbeddedPricing(t)
	std := resolvePricing("claude-opus-4-8")
	if std.Input != 5.0 || std.Output != 25.0 {
		t.Errorf("standard: expected 5/25, got %g/%g", std.Input, std.Output)
	}
	fast := resolvePricing("claude-opus-4-8:fast")
	if fast.Input != 10.0 || fast.Output != 50.0 {
		t.Errorf("fast: expected 10/50, got %g/%g", fast.Input, fast.Output)
	}
}

func TestResolvePricingFastModeFamilyPrefix(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-opus-4-6-20260501:fast")
	if p.Input != 30.0 {
		t.Errorf("expected fast input=30.0 via family prefix, got %f", p.Input)
	}
}

func TestResolvePricingFastModeNonFastModel(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-sonnet-4-6:fast")
	if p.Input != 3.0 {
		t.Errorf("expected standard input=3.0 (no fast tier for sonnet), got %f", p.Input)
	}
}

func TestResolvePricingStandardSpeed(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-opus-4-6")
	if p.Input != 5.0 {
		t.Errorf("expected standard input=5.0, got %f", p.Input)
	}
}

func TestCalcCostFastMode(t *testing.T) {
	withEmbeddedPricing(t)
	usage := Usage{
		InputTokens:  100_000,
		OutputTokens: 100_000,
	}
	cost := calcCost("claude-opus-4-6:fast", usage)
	// 100K input @ $30/M = $3.00, 100K output @ $150/M = $15.00
	assertCost(t, "fast mode cost", cost, 18.0)
}

func TestCalcCostFastVsStandard(t *testing.T) {
	withEmbeddedPricing(t)
	usage := Usage{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	}
	standardCost := calcCost("claude-opus-4-6", usage)
	fastCost := calcCost("claude-opus-4-6:fast", usage)

	ratio := fastCost / standardCost
	if ratio < 5.9 || ratio > 6.1 {
		t.Errorf("expected ~6x ratio, got %.2f (fast=$%.2f, standard=$%.2f)", ratio, fastCost, standardCost)
	}
}

func TestShortModel(t *testing.T) {
	withEmbeddedPricing(t)
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-opus-4-8", "Opus 4.8"},
		{"claude-opus-4-8:fast", "Opus 4.8 ⚡"},
		{"claude-opus-4-6", "Opus 4.6"},
		{"claude-opus-4-6:fast", "Opus 4.6 ⚡"},
		{"claude-opus-4-5-20251101", "Opus 4.5"},
		{"claude-opus-4-1-20250414", "Opus 4.1"},
		{"claude-sonnet-4-6", "Sonnet 4.6"},
		{"claude-sonnet-4-5-20250929", "Sonnet 4.5"},
		{"claude-sonnet-4-20250514", "Sonnet 4"},
		{"claude-haiku-4-5-20251001", "Haiku 4.5"},
		{"claude-haiku-3-5-20241022", "Haiku 3.5"},
		{"unknown-model", "unknown-model"},
	}
	for _, tt := range tests {
		got := shortModel(tt.input)
		if got != tt.expected {
			t.Errorf("shortModel(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
