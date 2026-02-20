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
		InputTokens:              1_000_000,
		OutputTokens:             1_000_000,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
	}
	cost := calcCost("claude-opus-4-6", usage)
	assertCost(t, "basic opus cost", cost, 30.0)
}

func TestCalcCostWithCache(t *testing.T) {
	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     1_000_000,
		CacheCreationInputTokens: 1_000_000,
	}
	cost := calcCost("claude-opus-4-6", usage)
	assertCost(t, "cache cost", cost, 6.75)
}

func TestCalcCostWithCacheBreakdown(t *testing.T) {
	usage := Usage{
		InputTokens:              0,
		OutputTokens:             0,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 2_000_000,
		CacheCreation: &CacheCreation{
			Ephemeral5mInputTokens: 1_000_000,
			Ephemeral1hInputTokens: 1_000_000,
		},
	}
	cost := calcCost("claude-opus-4-6", usage)
	assertCost(t, "cache breakdown cost", cost, 16.25)
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
