package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
		]
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
	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     100_000,
		CacheCreationInputTokens: 100_000,
	}
	cost := calcCost("claude-opus-4-6", usage)
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
	assertCost(t, "cache breakdown cost", cost, 1.0)
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
	assertCost(t, "long context premium", cr.Cost, 38.65)
	if !cr.LongCtx {
		t.Error("expected LongCtx = true for >200K")
	}
}

func TestCalcCostLongContextSonnet(t *testing.T) {
	usage := Usage{
		InputTokens:              50_000,
		OutputTokens:             500_000,
		CacheReadInputTokens:     200_000,
		CacheCreationInputTokens: 10_000,
		CacheCreation:            &CacheCreation{Ephemeral5mInputTokens: 10_000},
	}
	cr := calcCostResult("claude-sonnet-4-6", usage)
	assertCost(t, "sonnet long context", cr.Cost, 11.79)
	if !cr.LongCtx {
		t.Error("expected LongCtx = true")
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

func TestCalcCostCache5mFlag(t *testing.T) {
	cacheWriteAs1h = false
	defer func() { cacheWriteAs1h = true }()

	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     100_000,
		CacheCreationInputTokens: 100_000,
	}
	cost := calcCost("claude-opus-4-6", usage)
	assertCost(t, "cache 5m flag", cost, 0.675)
}

func TestShortModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-opus-4-6", "Opus 4.6"},
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
