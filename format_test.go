package main

import (
	"testing"
	"time"
)

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{500, "500"},
		{1_500, "1.5K"},
		{1_500_000, "1.5M"},
		{1_500_000_000, "1.5B"},
	}
	for _, tt := range tests {
		got := fmtTokens(tt.input)
		if got != tt.expected {
			t.Errorf("fmtTokens(%d) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFmtCost(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{0.0001, "$0.00"},
		{0.5, "$0.50"},
		{1.0, "$1.00"},
		{52.18, "$52.18"},
	}
	for _, tt := range tests {
		got := fmtCost(tt.input)
		if got != tt.expected {
			t.Errorf("fmtCost(%f) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestShortProject(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// macOS paths: strip -Users-<user>- prefix
		{"-Users-john-repos-myproject", "repos/myproject"},
		{"-Users-john-repos-org-subdir", "repos/org-subdir"},
		// Linux paths: strip -home-<user>- prefix
		{"-home-john-repos-myproject", "repos/myproject"},
		// No prefix to strip — first hyphen becomes /
		{"simple-project", "simple/project"},
		// Double hyphens collapsed
		{"a--b", "a/b"},
		// Truncation at 40 chars
		{"a-very-long-project-name-that-exceeds-the-forty-character-limit", "...hat-exceeds-the-forty-character-limit"},
		// Empty slug returns original
		{"", ""},
	}
	for _, tt := range tests {
		got := shortProject(tt.input)
		if got != tt.expected {
			t.Errorf("shortProject(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestFmtDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{500 * time.Microsecond, "500µs"},
		{999 * time.Microsecond, "999µs"},
		{1 * time.Millisecond, "1ms"},
		{150 * time.Millisecond, "150ms"},
		{1500 * time.Millisecond, "1500ms"},
	}
	for _, tt := range tests {
		got := fmtDuration(tt.input)
		if got != tt.expected {
			t.Errorf("fmtDuration(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestTotalCacheWrite(t *testing.T) {
	b := &Bucket{CacheWrite5m: 100, CacheWrite1h: 200}
	if got := b.TotalCacheWrite(); got != 300 {
		t.Errorf("TotalCacheWrite() = %d, want 300", got)
	}
}

func TestAggregateMonthly(t *testing.T) {
	daily := map[string]map[string]*Bucket{
		"2025-01-15": {
			"claude-sonnet": {InputTokens: 100, OutputTokens: 50, Requests: 1, Cost: 1.0},
		},
		"2025-01-20": {
			"claude-sonnet": {InputTokens: 200, OutputTokens: 100, Requests: 2, Cost: 2.0},
			"claude-opus":   {InputTokens: 300, OutputTokens: 150, Requests: 1, Cost: 5.0},
		},
		"2025-02-05": {
			"claude-sonnet": {InputTokens: 400, OutputTokens: 200, Requests: 3, Cost: 3.0},
		},
	}

	monthly := aggregateMonthly(daily)

	tests := []struct {
		name     string
		month    string
		model    string
		wantReqs int
		wantCost float64
	}{
		{"jan sonnet combined", "2025-01", "claude-sonnet", 3, 3.0},
		{"jan opus", "2025-01", "claude-opus", 1, 5.0},
		{"feb sonnet", "2025-02", "claude-sonnet", 3, 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, ok := monthly[tt.month][tt.model]
			if !ok {
				t.Fatalf("missing bucket for %s / %s", tt.month, tt.model)
			}
			if b.Requests != tt.wantReqs {
				t.Errorf("requests = %d, want %d", b.Requests, tt.wantReqs)
			}
			if b.Cost != tt.wantCost {
				t.Errorf("cost = %f, want %f", b.Cost, tt.wantCost)
			}
		})
	}

	if len(monthly) != 2 {
		t.Errorf("expected 2 months, got %d", len(monthly))
	}
}

func TestAggregateMonthlyTokens(t *testing.T) {
	daily := map[string]map[string]*Bucket{
		"2025-03-01": {
			"model-a": {InputTokens: 100, OutputTokens: 50, CacheRead: 10, CacheWrite5m: 5, CacheWrite1h: 15, Requests: 1, Cost: 1.0},
		},
		"2025-03-15": {
			"model-a": {InputTokens: 200, OutputTokens: 100, CacheRead: 20, CacheWrite5m: 10, CacheWrite1h: 30, Requests: 2, Cost: 2.0},
		},
	}

	monthly := aggregateMonthly(daily)
	b := monthly["2025-03"]["model-a"]

	if b.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", b.InputTokens)
	}
	if b.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", b.OutputTokens)
	}
	if b.CacheRead != 30 {
		t.Errorf("CacheRead = %d, want 30", b.CacheRead)
	}
	if b.CacheWrite5m != 15 {
		t.Errorf("CacheWrite5m = %d, want 15", b.CacheWrite5m)
	}
	if b.CacheWrite1h != 45 {
		t.Errorf("CacheWrite1h = %d, want 45", b.CacheWrite1h)
	}
}
