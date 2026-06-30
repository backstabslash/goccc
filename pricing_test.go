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

	p := resolvePricing("claude-test-model", time.Time{})
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

	p := resolvePricing("claude-opus-4-6", time.Time{})
	if p.Input != 5.0 {
		t.Errorf("expected embedded input=5.0, got %f", p.Input)
	}
}

func TestResolvePricingExactMatch(t *testing.T) {
	p := resolvePricing("claude-opus-4-6", time.Time{})
	if p.Input != 5.0 {
		t.Errorf("expected input=5.0, got %f", p.Input)
	}
	if p.Output != 25.0 {
		t.Errorf("expected output=25.0, got %f", p.Output)
	}
}

func TestResolvePricingFamilyPrefix(t *testing.T) {
	p := resolvePricing("claude-opus-4-5-20260101", time.Time{})
	if p.Input != 5.0 {
		t.Errorf("expected input=5.0 (opus-4-5 pricing), got %f", p.Input)
	}
}

func TestResolvePricingFallback(t *testing.T) {
	p := resolvePricing("claude-unknown-99", time.Time{})
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
	p := resolvePricing("claude-opus-4-9-20270101", time.Time{})
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
	p := resolvePricing("claude-opus-4-20250514", time.Time{})
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
	p := resolvePricing("claude-sonnet-4-9-20270101", time.Time{})
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
	cost := calcCost("claude-opus-4-6", usage, time.Time{})
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
	cost := calcCost("claude-opus-4-6", usage, time.Time{})
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
	cost := calcCost("claude-opus-4-6", usage, time.Time{})
	// 50K @ 5m tier (1.25x $5) = $0.3125, 50K @ 1h tier (2x $5) = $0.50
	assertCost(t, "cache breakdown cost", cost, 0.8125)
}

func TestCalcCostWebSearch(t *testing.T) {
	usage := Usage{
		InputTokens:   100_000,
		OutputTokens:  10_000,
		ServerToolUse: &ServerToolUse{WebSearchRequests: 5},
	}
	cr := calcCostResult("claude-opus-4-6", usage, time.Time{})
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
	cr := calcCostResult("claude-opus-4-6", usage, time.Time{})
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
	cr := calcCostResult("claude-opus-4-6", usage, time.Time{})
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
	cr := calcCostResult("claude-sonnet-4-6", usage, time.Time{})
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
	cr := calcCostResult("claude-haiku-4-5-20251001", usage, time.Time{})
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
	cost := calcCost("claude-haiku-4-5-20251001", usage, time.Time{})
	// 100K @ 5m tier (1.25x $1) = $0.125
	assertCost(t, "haiku 5m cache", cost, 0.125)

	// Opus: all cache writes at 1h tier
	usage2 := Usage{
		CacheCreationInputTokens: 100_000,
		CacheCreation:            &CacheCreation{Ephemeral1hInputTokens: 100_000},
	}
	cost2 := calcCost("claude-opus-4-6", usage2, time.Time{})
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
	p := resolvePricing("claude-opus-4-6:fast", time.Time{})
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
	std := resolvePricing("claude-opus-4-8", time.Time{})
	if std.Input != 5.0 || std.Output != 25.0 {
		t.Errorf("standard: expected 5/25, got %g/%g", std.Input, std.Output)
	}
	fast := resolvePricing("claude-opus-4-8:fast", time.Time{})
	if fast.Input != 10.0 || fast.Output != 50.0 {
		t.Errorf("fast: expected 10/50, got %g/%g", fast.Input, fast.Output)
	}
}

func TestResolvePricingFastModeFamilyPrefix(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-opus-4-6-20260501:fast", time.Time{})
	if p.Input != 30.0 {
		t.Errorf("expected fast input=30.0 via family prefix, got %f", p.Input)
	}
}

func TestResolvePricingFastModeNonFastModel(t *testing.T) {
	withEmbeddedPricing(t)
	p := resolvePricing("claude-sonnet-4-6:fast", time.Time{})
	if p.Input != 3.0 {
		t.Errorf("expected standard input=3.0 (no fast tier for sonnet), got %f", p.Input)
	}
}

func TestCalcCostFastMode(t *testing.T) {
	withEmbeddedPricing(t)
	usage := Usage{
		InputTokens:  100_000,
		OutputTokens: 100_000,
	}
	cost := calcCost("claude-opus-4-6:fast", usage, time.Time{})
	// 100K input @ $30/M = $3.00, 100K output @ $150/M = $15.00
	assertCost(t, "fast mode cost", cost, 18.0)
}

func TestCalcCostFastVsStandard(t *testing.T) {
	withEmbeddedPricing(t)
	usage := Usage{
		InputTokens:  1_000_000,
		OutputTokens: 1_000_000,
	}
	standardCost := calcCost("claude-opus-4-6", usage, time.Time{})
	fastCost := calcCost("claude-opus-4-6:fast", usage, time.Time{})

	ratio := fastCost / standardCost
	if ratio < 5.9 || ratio > 6.1 {
		t.Errorf("expected ~6x ratio, got %.2f (fast=$%.2f, standard=$%.2f)", ratio, fastCost, standardCost)
	}
}

func TestScheduleResolutionSonnet5(t *testing.T) {
	withEmbeddedPricing(t)
	aug := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	boundary := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	sep := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name          string
		ts            time.Time
		input, output float64
	}{
		{"before boundary -> intro", aug, 2.0, 10.0},
		{"at boundary -> standard", boundary, 3.0, 15.0},
		{"after boundary -> standard", sep, 3.0, 15.0},
		{"zero ts -> base/intro", time.Time{}, 2.0, 10.0},
	}
	for _, tc := range cases {
		p := resolvePricing("claude-sonnet-5", tc.ts)
		if p.Input != tc.input || p.Output != tc.output {
			t.Errorf("%s: got %g/%g, want %g/%g", tc.name, p.Input, p.Output, tc.input, tc.output)
		}
	}
}

// A dated model ID must inherit its family's schedule via prefix resolution.
func TestScheduleResolutionDatedModelID(t *testing.T) {
	withEmbeddedPricing(t)
	sep := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	p := resolvePricing("claude-sonnet-5-20260901", sep)
	if p.Input != 3.0 || p.Output != 15.0 {
		t.Errorf("dated sonnet-5 in september: got %g/%g, want 3/15", p.Input, p.Output)
	}
}

// Models without a schedule must price identically regardless of timestamp.
func TestNoScheduleRegression(t *testing.T) {
	withEmbeddedPricing(t)
	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	aug := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	sep := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	if a, s := calcCost("claude-opus-4-6", usage, aug), calcCost("claude-opus-4-6", usage, sep); a != s {
		t.Errorf("opus-4-6 cost changed across schedule boundary: %g vs %g", a, s)
	}
}

// A schedule entry omitting cache fields derives them from its own input price,
// and an entry with an unparseable from is dropped (best-effort, not fatal).
func TestScheduleNormalization(t *testing.T) {
	data := `{
		"models": {
			"claude-sched-test": {
				"input": 1.00, "output": 5.00,
				"schedule": [
					{ "from": "2026-09-01", "input": 2.00, "output": 10.00 },
					{ "from": "not-a-date", "input": 99.00, "output": 99.00 }
				]
			}
		},
		"families": [],
		"default_model": "claude-sched-test",
		"display_names": []
	}`
	applyTestPricing(t, data)

	m := pricingTable["claude-sched-test"]
	if len(m.Schedule) != 1 {
		t.Fatalf("expected 1 schedule entry after dropping bad from, got %d", len(m.Schedule))
	}
	sep := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	p := resolvePricing("claude-sched-test", sep)
	// cache_write_5m defaults to 1.25x input ($2) = $2.50
	assertCost(t, "schedule entry derived cache_write_5m", p.CacheWrite5m, 2.5)
	assertCost(t, "schedule entry input", p.Input, 2.0)
}

// The boundary is an absolute UTC instant, not a local calendar day: a message
// stamped Aug 31 in a western zone but Sep 1 in UTC prices as standard, and one
// stamped Sep 1 in an eastern zone but Aug 31 in UTC prices as intro.
func TestScheduleBoundaryIsUTC(t *testing.T) {
	withEmbeddedPricing(t)
	west := time.FixedZone("UTC-7", -7*3600)
	east := time.FixedZone("UTC+5", 5*3600)

	localAug31ButUTCSep1 := time.Date(2026, 8, 31, 20, 0, 0, 0, west) // = Sep 1 03:00 UTC
	localSep1ButUTCAug31 := time.Date(2026, 9, 1, 2, 0, 0, 0, east)   // = Aug 31 21:00 UTC

	if p := resolvePricing("claude-sonnet-5", localAug31ButUTCSep1); p.Input != 3.0 {
		t.Errorf("western Aug 31 (UTC Sep 1): got input %g, want 3.0 (standard)", p.Input)
	}
	if p := resolvePricing("claude-sonnet-5", localSep1ButUTCAug31); p.Input != 2.0 {
		t.Errorf("eastern Sep 1 (UTC Aug 31): got input %g, want 2.0 (intro)", p.Input)
	}
}

// applyTestPricing loads custom pricing JSON, applies it, and restores the
// embedded pricing after the test.
func applyTestPricing(t *testing.T, data string) {
	t.Helper()
	origCachePath := pricingCachePath
	pd, err := loadPricingFrom([]byte(data))
	if err != nil {
		t.Fatalf("loadPricingFrom: %v", err)
	}
	applyPricing(pd)
	t.Cleanup(func() {
		pricingCachePath = origCachePath
		initPricing()
	})
}

// Forward compatibility: a future pricing.json carrying keys this binary doesn't
// know must decode without error and still read the base price. Guards against a
// regression that tightened loadPricingFrom (e.g. DisallowUnknownFields).
func TestLoadPricingForwardCompatible(t *testing.T) {
	pd, err := loadPricingFrom([]byte(`{
		"models": { "claude-fwd-test": { "input": 2.00, "output": 10.00, "future_unknown_key": 42 } },
		"families": [],
		"default_model": "claude-fwd-test",
		"display_names": [],
		"some_unknown_top_level": true
	}`))
	if err != nil {
		t.Fatalf("unknown future key must not error: %v", err)
	}
	if m := pd.Models["claude-fwd-test"]; m.Input != 2.0 || m.Output != 10.0 {
		t.Errorf("got %g/%g, want 2/10", m.Input, m.Output)
	}
}

// A schedule with out-of-order, non-monotonic entries (price rises, dips back to
// intro for a month, then rises again) resolves to the greatest From <= ts at
// every point. Covers ordering-independence and the "grant one more month of
// lower pricing" case in one test.
func TestScheduleResolutionWindows(t *testing.T) {
	applyTestPricing(t, `{
		"models": {
			"claude-window-test": {
				"input": 2.00, "output": 2.00,
				"schedule": [
					{ "from": "2026-12-01", "input": 3.00, "output": 3.00 },
					{ "from": "2026-09-01", "input": 3.00, "output": 3.00 },
					{ "from": "2026-11-01", "input": 2.00, "output": 2.00 }
				]
			}
		},
		"families": [],
		"default_model": "claude-window-test",
		"display_names": []
	}`)

	cases := []struct {
		month string
		ts    time.Time
		want  float64
	}{
		{"Aug (before all -> base intro)", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), 2.0},
		{"Sep (standard)", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), 3.0},
		{"Nov (reprieve, dips back)", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), 2.0},
		{"Dec (standard again)", time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), 3.0},
	}
	for _, c := range cases {
		if p := resolvePricing("claude-window-test", c.ts); p.Input != c.want {
			t.Errorf("%s: got input %g, want %g", c.month, p.Input, c.want)
		}
	}
}

// End-to-end: each record's timestamp must flow from the JSONL through the parser
// into cost resolution. Identical usage in August (intro) and September (standard)
// sums to intro+standard; a wiring that ignored timestamps would yield all-intro
// ($24) or all-standard ($36) instead of $30.
func TestScheduleEndToEndByTimestamp(t *testing.T) {
	withEmbeddedPricing(t)
	lines := []string{
		makeRecord("req_aug", "claude-sonnet-5", "2026-08-15T00:00:00Z", 1_000_000, 1_000_000, 0, 0, 0),
		makeRecord("req_sep", "claude-sonnet-5", "2026-09-15T00:00:00Z", 1_000_000, 1_000_000, 0, 0, 0),
	}
	base := setupProject(t, "sonnet5-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	m := data.ModelUsage["claude-sonnet-5"]
	if m == nil {
		t.Fatal("expected claude-sonnet-5 bucket")
	}
	assertCost(t, "per-timestamp end-to-end cost", m.Cost, 30.0)
}

// Bumping the base price must not drop a long-context tier the entry omits.
func TestScheduleInheritsLongContext(t *testing.T) {
	applyTestPricing(t, `{
		"models": {
			"claude-lc-test": {
				"input": 3.00, "output": 15.00,
				"long_ctx_input": 6.00, "long_ctx_output": 22.50,
				"schedule": [
					{ "from": "2026-09-01", "input": 4.00, "output": 18.00 }
				]
			}
		},
		"families": [],
		"default_model": "claude-lc-test",
		"display_names": [],
		"long_context_threshold": 200000
	}`)

	usage := Usage{InputTokens: 250_000}
	sep := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	cr := calcCostResult("claude-lc-test", usage, sep)
	if !cr.LongCtx {
		t.Fatal("long-context tier vanished after schedule boundary")
	}
	// 250K input @ inherited long_ctx $6/M = $1.50, not standard $4/M = $1.00
	assertCost(t, "long-ctx input after base price bump", cr.Cost, 1.5)
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
