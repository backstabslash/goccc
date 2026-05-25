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
		// Long names preserved
		{"a-very-long-project-name-that-exceeds-the-forty-character-limit", "a/very-long-project-name-that-exceeds-the-forty-character-limit"},
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

func TestShortenRealPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/Users/john/code/_org/proj", "code/_org/proj"},
		{"/home/john/code/.tools/sub", "code/.tools/sub"},
		{"/Users/john", "john"},
		{"/Users/john/", "john"},
		{"/opt/project", "/opt/project"},
		{`C:\Users\john\code\proj`, "code/proj"},
		{`D:\code\proj`, "/code/proj"},
	}
	for _, tt := range tests {
		got := shortenRealPath(tt.input)
		if got != tt.expected {
			t.Errorf("shortenRealPath(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDisplayProject(t *testing.T) {
	paths := map[string]string{
		"-Users-john-code--org-proj": "/Users/john/code/_org/proj",
	}
	if got := displayProject("-Users-john-code--org-proj", paths); got != "code/_org/proj" {
		t.Errorf("with cwd map: got %q, want %q", got, "code/_org/proj")
	}
	if got := displayProject("-Users-john-code-other", paths); got != "code/other" {
		t.Errorf("fallback: got %q, want %q", got, "code/other")
	}
	if got := displayProject("-Users-john-code-other", nil); got != "code/other" {
		t.Errorf("nil map: got %q, want %q", got, "code/other")
	}
}

func TestWrapName(t *testing.T) {
	tests := []struct {
		name     string
		chunk    int
		expected []string
	}{
		{"short", 10, []string{"short"}},
		{"abcdefghij", 10, []string{"abcdefghij"}},
		{"abcdefghijklmnopqrst", 10, []string{"abcdefghij", "klmnopqrst"}},
		{"abcdefghijklmnopqrstuvwxyz12345", 10, []string{"abcdefghij", "klmnopqrst", "uvwxyz1234", "5"}},
		{"", 10, nil},
	}
	for _, tt := range tests {
		got := wrapName(tt.name, tt.chunk)
		if len(got) != len(tt.expected) {
			t.Errorf("wrapName(%q, %d) len = %d, want %d", tt.name, tt.chunk, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("wrapName(%q, %d)[%d] = %q, want %q", tt.name, tt.chunk, i, got[i], tt.expected[i])
			}
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

func TestColorizeCustomThresholds(t *testing.T) {
	origNoColor := noColorFlag
	noColorFlag = false
	origWarn := costThresholdYellow
	origAlert := costThresholdRed
	origCurrency := activeCurrency
	defer func() {
		noColorFlag = origNoColor
		costThresholdYellow = origWarn
		costThresholdRed = origAlert
		activeCurrency = origCurrency
	}()

	costThresholdYellow = 10.0
	costThresholdRed = 20.0

	if colorize("test", 5.0) != "test" {
		t.Error("below warn threshold should not colorize")
	}
	if colorize("test", 15.0) == "test" {
		t.Error("between warn and alert should colorize (yellow)")
	}
	if colorize("test", 25.0) == "test" {
		t.Error("above alert should colorize (red)")
	}

	// With currency active, colorize converts USD cost before comparing
	activeCurrency.Rate = 0.92
	costThresholdYellow = 25.0
	costThresholdRed = 50.0

	if colorize("test", 20.0) != "test" { // 20 * 0.92 = 18.4 < 25
		t.Error("€18.40 should be plain (below €25 warn)")
	}
	if colorize("test", 30.0) == "test" { // 30 * 0.92 = 27.6, between 25-50
		t.Error("€27.60 should be yellow")
	}
	if colorize("test", 60.0) == "test" { // 60 * 0.92 = 55.2 > 50
		t.Error("€55.20 should be red")
	}
}

func TestColorizeDefaultThresholds(t *testing.T) {
	origNoColor := noColorFlag
	noColorFlag = false
	defer func() { noColorFlag = origNoColor }()

	if colorize("test", 20.0) != "test" {
		t.Error("$20 should be plain with default $25 warn threshold")
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
