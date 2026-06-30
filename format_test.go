package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestColorCostAlignment(t *testing.T) {
	origNoColor := noColorFlag
	origCurrency := activeCurrency
	noColorFlag = true
	defer func() {
		noColorFlag = origNoColor
		activeCurrency = origCurrency
	}()

	// Multi-byte symbol: € is 3 bytes but 1 visible column. Padding must be
	// computed by visible width so different magnitudes right-align.
	activeCurrency.Rate = 1.0
	activeCurrency.Symbol = "€"
	activeCurrency.Suffix = false

	const width = 8
	for _, cost := range []float64{1.23, 12.34, 123.45} {
		got := colorCost(cost, width)
		if w := utf8.RuneCountInString(got); w != width {
			t.Errorf("colorCost(%v, %d) = %q has visible width %d, want %d", cost, width, got, w, width)
		}
	}

	// A value wider than the column keeps its full width (no truncation).
	wide := colorCost(123456.78, width)
	if w := utf8.RuneCountInString(wide); w < width {
		t.Errorf("colorCost(123456.78, %d) = %q width %d, want >= %d", width, wide, w, width)
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

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy: %v", err)
	}
	return buf.String()
}

func fixtureData(t *testing.T) *ParseResult {
	t.Helper()
	data, err := parseLogs("testdata", 0, "")
	if err != nil {
		t.Fatalf("parseLogs: %v", err)
	}
	return data
}

func assertContainsAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("output missing %q\n--- output ---\n%s", w, got)
		}
	}
}

func TestPrintDailyBreakdown(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	data := fixtureData(t)
	out := captureStdout(t, func() { printDailyBreakdown(data, OutputOptions{ShowDaily: true}) })

	assertContainsAll(t, out, "DAILY BREAKDOWN", "Date", "2026-02-18", "2026-02-19", "Opus 4.6", "Haiku 4.5")
	// Two days, each ending in a subtotal row (blank label + cost).
	if n := strings.Count(out, "$"); n < 2 {
		t.Errorf("expected cost figures in daily breakdown, got %d $ signs", n)
	}
}

func TestPrintMonthlyBreakdown(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	data := fixtureData(t)
	out := captureStdout(t, func() { printMonthlyBreakdown(data, OutputOptions{ShowMonthly: true}) })

	assertContainsAll(t, out, "MONTHLY BREAKDOWN", "Month", "2026-02", "Opus 4.6", "Haiku 4.5")
}

func TestPrintProjectBreakdown(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	data := fixtureData(t)
	name := displayProject("C--Users-alice-git-webapp", data.ProjectPaths)
	out := captureStdout(t, func() { printProjectBreakdown(data, OutputOptions{ShowProjects: true}) })

	assertContainsAll(t, out, "PROJECT BREAKDOWN", "Project", name, "Opus 4.6", "Haiku 4.5", "SUBTOTAL")
}

func TestPrintBranchBreakdown(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	data := fixtureData(t)
	out := captureStdout(t, func() { printBranchBreakdown(data, OutputOptions{ShowBranches: true}) })

	assertContainsAll(t, out, "BRANCH BREAKDOWN", "Branch", "main", "Opus 4.6", "Haiku 4.5", "SUBTOTAL")
}

func TestPrintDailyBreakdown_TopN(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	data := fixtureData(t)
	out := captureStdout(t, func() { printDailyBreakdown(data, OutputOptions{ShowDaily: true, TopN: 1}) })

	// TopN=1 keeps only the most recent day.
	if strings.Contains(out, "2026-02-18") {
		t.Errorf("TopN=1 should drop the older day\n%s", out)
	}
	assertContainsAll(t, out, "2026-02-19")
}

func TestBuildJSONDaily(t *testing.T) {
	data := fixtureData(t)
	rows := buildJSONDaily(data)
	if len(rows) == 0 {
		t.Fatal("expected daily rows")
	}
	// Sorted by date descending, then cost descending.
	for i := 1; i < len(rows); i++ {
		if rows[i-1].Date < rows[i].Date {
			t.Errorf("rows not date-descending at %d: %s < %s", i, rows[i-1].Date, rows[i].Date)
		}
	}
}

func TestBuildJSONMonthly(t *testing.T) {
	data := fixtureData(t)
	rows := buildJSONMonthly(data)
	if len(rows) == 0 {
		t.Fatal("expected monthly rows")
	}
	for _, r := range rows {
		if r.Month != "2026-02" {
			t.Errorf("unexpected month %q", r.Month)
		}
	}
}
