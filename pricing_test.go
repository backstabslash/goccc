package main

import (
	"testing"
)

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
	// (0.1 * 5) + (0.1 * 25) = 0.5 + 2.5 = 3.0
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
	// cache read: (0.1 * 0.5) = 0.05, cache write 1h: (0.1 * 10.0) = 1.0
	// total input: 200K = threshold, NOT exceeded → standard
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
	// All treated as 1h: (0.1 * 10.0) = 1.0
	assertCost(t, "cache breakdown cost", cost, 1.0)
}

func TestCalcCostWebSearch(t *testing.T) {
	usage := Usage{
		InputTokens:   100_000,
		OutputTokens:  10_000,
		ServerToolUse: &ServerToolUse{WebSearchRequests: 5},
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	// Token cost: (0.1 * 5) + (0.01 * 25) = 0.5 + 0.25 = 0.75
	// Web search cost: 5 * 0.01 = 0.05
	assertCost(t, "web search total", cr.Cost, 0.80)
	if cr.WebSearches != 5 {
		t.Errorf("web searches = %d, want 5", cr.WebSearches)
	}
	if cr.LongCtx {
		t.Error("expected LongCtx = false")
	}
}

func TestCalcCostLongContextStandard(t *testing.T) {
	// Total input = 100K + 50K = 150K < 200K threshold → standard pricing
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         1_000_000,
		CacheReadInputTokens: 50_000,
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	// Standard: (0.1 * 5) + (1.0 * 25) + (0.05 * 0.5) = 0.5 + 25.0 + 0.025 = 25.525
	assertCost(t, "standard context", cr.Cost, 25.525)
	if cr.LongCtx {
		t.Error("expected LongCtx = false for <200K")
	}
}

func TestCalcCostLongContextPremium(t *testing.T) {
	// Total input = 100K + 150K = 250K > 200K threshold → premium pricing
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         1_000_000,
		CacheReadInputTokens: 150_000,
	}
	cr := calcCostResult("claude-opus-4-6", usage)
	// Premium: (0.1 * 10) + (1.0 * 37.5) + (0.15 * 1.0) = 1.0 + 37.5 + 0.15 = 38.65
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
	// Total input = 50K + 200K + 10K = 260K > 200K → premium
	// Premium: (0.05 * 6) + (0.5 * 22.5) + (0.2 * 0.6) + (0.01 * 12.0) = 0.3 + 11.25 + 0.12 + 0.12 = 11.79
	assertCost(t, "sonnet long context", cr.Cost, 11.79)
	if !cr.LongCtx {
		t.Error("expected LongCtx = true")
	}
}

func TestCalcCostLongContextNoModel(t *testing.T) {
	// Haiku has no long-context pricing — should stay standard even with >200K
	usage := Usage{
		InputTokens:          100_000,
		OutputTokens:         100_000,
		CacheReadInputTokens: 200_000,
	}
	cr := calcCostResult("claude-haiku-4-5-20251001", usage)
	// Standard: (0.1 * 1) + (0.1 * 5) + (0.2 * 0.1) = 0.1 + 0.5 + 0.02 = 0.62
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
	// With 5m: cache read (0.1 * 0.5) = 0.05, cache write 5m (0.1 * 6.25) = 0.625
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
