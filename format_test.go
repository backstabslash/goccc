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
		{0.0001, "$0.0001"},
		{0.5, "$0.5000"},
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
