package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Resolution and cost tests run against these synthetic rates, never pricing.json,
// so adding or repricing a real model never touches a test. claude-alpha-* covers
// family fall-forward and the "-2" minor that collides with date stamps,
// claude-beta-1 the long-context tier, claude-gamma-1 the unknown-model default.
const testPricingJSON = `{
	"models": {
		"claude-alpha-9":  { "input": 10.00, "output": 100.00 },
		"claude-alpha-7":  { "input": 1.00,  "output": 10.00 },
		"claude-alpha-2":  { "input": 4.00,  "output": 40.00 },
		"claude-beta-1":   { "input": 2.00,  "output": 20.00, "long_ctx_input": 4.00, "long_ctx_output": 40.00 },
		"claude-gamma-1":  { "input": 5.00,  "output": 50.00 }
	},
	"fast_models": {
		"claude-alpha-9":  { "input": 30.00, "output": 300.00 }
	},
	"families": [
		{ "prefix": "claude-alpha-9",  "model": "claude-alpha-9" },
		{ "prefix": "claude-alpha-7",  "model": "claude-alpha-7" },
		{ "prefix": "claude-alpha-2",  "model": "claude-alpha-2" },
		{ "prefix": "claude-alpha",    "model": "claude-alpha-7" },
		{ "prefix": "claude-beta-1",   "model": "claude-beta-1" },
		{ "prefix": "claude-gamma-1",  "model": "claude-gamma-1" }
	],
	"default_model": "claude-gamma-1",
	"display_names": [
		{ "prefix": "alpha-9", "name": "Alpha 9" },
		{ "prefix": "alpha-7", "name": "Alpha 7" },
		{ "prefix": "alpha-2", "name": "Alpha 2" },
		{ "prefix": "alpha",   "name": "Alpha" },
		{ "prefix": "beta-1",  "name": "Beta 1" },
		{ "prefix": "gamma-1", "name": "Gamma 1" }
	],
	"long_context_threshold": 200000,
	"web_search_cost": 0.01
}`

const schedulePricingJSON = `{
	"models": {
		"claude-sched-1": {
			"input": 2.00, "output": 20.00,
			"schedule": [ { "from": "2026-09-01", "input": 3.00, "output": 30.00 } ]
		}
	},
	"families": [ { "prefix": "claude-sched-1", "model": "claude-sched-1" } ],
	"default_model": "claude-sched-1",
	"display_names": [ { "prefix": "sched-1", "name": "Sched 1" } ]
}`

// applyTestPricing swaps in custom pricing JSON, restoring the embedded data after.
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
		"web_search_cost": 0.05,
		"inference_geo_multipliers": { "us": 1.5 }
	}`

	cacheFile := filepath.Join(t.TempDir(), "pricing.json")
	if err := os.WriteFile(cacheFile, []byte(cachedData), 0o644); err != nil {
		t.Fatal(err)
	}

	origCachePath := pricingCachePath
	pricingCachePath = func() string { return cacheFile }
	t.Cleanup(func() {
		pricingCachePath = origCachePath
		initPricing()
	})
	initPricing()

	p := resolvePricing("claude-test-model", time.Time{})
	assertCost(t, "cached input", p.Input, 99.0)
	assertCost(t, "derived cache_read", p.CacheRead, 9.9)
	assertCost(t, "derived cache_write_5m", p.CacheWrite5m, 123.75)
	assertCost(t, "derived cache_write_1h", p.CacheWrite1h, 198.0)
	assertInt(t, "longCtxThreshold", longCtxThreshold, 100000)
	assertCost(t, "webSearchCostPerSearch", webSearchCostPerSearch, 0.05)
	assertCost(t, "inferenceGeoMultipliers[us]", inferenceGeoMultipliers["us"], 1.5)
	if name := shortModel("claude-test-model"); name != "Test Model" {
		t.Errorf("shortModel = %q, want %q", name, "Test Model")
	}
}

func TestInitPricingFallsBackToEmbedded(t *testing.T) {
	var embedded PricingData
	if err := json.Unmarshal(embeddedPricingJSON, &embedded); err != nil {
		t.Fatalf("embedded pricing.json: %v", err)
	}

	origCachePath := pricingCachePath
	pricingCachePath = func() string { return "" }
	t.Cleanup(func() {
		pricingCachePath = origCachePath
		initPricing()
	})
	initPricing()

	assertInt(t, "loaded models", len(pricingTable), len(embedded.Models))
}

// A future pricing.json carrying keys this binary doesn't know must still decode.
// Guards against a regression that tightened loadPricingFrom (e.g. DisallowUnknownFields).
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

// wantPricing lets cases name the entry they should land on instead of its prices.
func wantPricing(t *testing.T, entry string) PriceFields {
	t.Helper()
	key, isFast := strings.CutSuffix(entry, ":fast")
	table := pricingTable
	if isFast {
		table = fastPricingTable
	}
	p, ok := table[key]
	if !ok {
		t.Fatalf("test names unknown pricing entry %q", entry)
	}
	return p.PriceFields
}

func TestResolvePricingRules(t *testing.T) {
	applyTestPricing(t, testPricingJSON)
	tests := []struct{ name, model, entry string }{
		{"exact match", "claude-alpha-9", "claude-alpha-9"},
		{"date suffix", "claude-alpha-9-20260101", "claude-alpha-9"},
		{"unknown minor takes the newest in the family", "claude-alpha-8-20270101", "claude-alpha-9"},
		{"date stamp is not read as the -2 minor", "claude-alpha-20250101", "claude-alpha-7"},
		{"unknown family takes the default", "claude-delta-9", "claude-gamma-1"},
		{"fast tier", "claude-alpha-9:fast", "claude-alpha-9:fast"},
		{"fast tier through a date suffix", "claude-alpha-9-20260101:fast", "claude-alpha-9:fast"},
		{"no fast tier stays standard", "claude-beta-1:fast", "claude-beta-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolvePricing(tt.model, time.Time{}).PriceFields; got != wantPricing(t, tt.entry) {
				t.Errorf("%s resolved to %g/%g, want %s", tt.model, got.Input, got.Output, tt.entry)
			}
		})
	}
}

func TestCalcCost(t *testing.T) {
	applyTestPricing(t, testPricingJSON)
	tests := []struct {
		name     string
		model    string
		usage    Usage
		cost     float64
		longCtx  bool
		searches int
	}{
		{
			name:  "input and output",
			model: "claude-alpha-9",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000},
			cost:  11,
		},
		{
			name:  "cache read plus flat cache write defaults to the 1h tier",
			model: "claude-alpha-9",
			usage: Usage{CacheReadInputTokens: 100_000, CacheCreationInputTokens: 100_000},
			cost:  2.1,
		},
		{
			name:  "cache write split across tiers",
			model: "claude-alpha-9",
			usage: Usage{CacheCreationInputTokens: 100_000, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 50_000, Ephemeral1hInputTokens: 50_000}},
			cost:  1.625,
		},
		{
			name:     "web searches",
			model:    "claude-alpha-9",
			usage:    Usage{InputTokens: 100_000, OutputTokens: 10_000, ServerToolUse: &ServerToolUse{WebSearchRequests: 5}},
			cost:     2.05,
			searches: 5,
		},
		{
			name:     "us inference geo multiplies tokens but not searches",
			model:    "claude-alpha-9",
			usage:    Usage{InputTokens: 100_000, OutputTokens: 100_000, InferenceGeo: "us", ServerToolUse: &ServerToolUse{WebSearchRequests: 5}},
			cost:     12.15,
			searches: 5,
		},
		{
			name:  "other inference geo is standard",
			model: "claude-alpha-9",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000, InferenceGeo: "not_available"},
			cost:  11,
		},
		{
			name:  "fast tier",
			model: "claude-alpha-9:fast",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000},
			cost:  33,
		},
		{
			name:    "over the threshold with a long-context tier",
			model:   "claude-beta-1",
			usage:   Usage{InputTokens: 100_000, OutputTokens: 100_000, CacheReadInputTokens: 150_000},
			cost:    4.46,
			longCtx: true,
		},
		{
			name:  "under the threshold",
			model: "claude-beta-1",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000, CacheReadInputTokens: 50_000},
			cost:  2.21,
		},
		{
			name:  "over the threshold without a long-context tier",
			model: "claude-alpha-9",
			usage: Usage{InputTokens: 100_000, OutputTokens: 100_000, CacheReadInputTokens: 200_000},
			cost:  11.2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := calcCostResult(tt.model, tt.usage, time.Time{})
			assertCost(t, "cost", cr.Cost, tt.cost)
			if cr.LongCtx != tt.longCtx {
				t.Errorf("LongCtx = %v, want %v", cr.LongCtx, tt.longCtx)
			}
			assertInt(t, "WebSearches", cr.WebSearches, tt.searches)
		})
	}
}

func TestScheduleResolution(t *testing.T) {
	applyTestPricing(t, schedulePricingJSON)
	sep := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		model string
		ts    time.Time
		input float64
	}{
		{"zero timestamp predates every entry", "claude-sched-1", time.Time{}, 2},
		{"before the boundary", "claude-sched-1", time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC), 2},
		{"at the boundary", "claude-sched-1", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), 3},
		{"after the boundary", "claude-sched-1", sep, 3},
		{"dated model ID inherits the schedule", "claude-sched-1-20260901", sep, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if p := resolvePricing(tt.model, tt.ts); p.Input != tt.input {
				t.Errorf("input = %g, want %g", p.Input, tt.input)
			}
		})
	}
}

// A fast tier carries its own schedule — how a withdrawn fast mode drops back to
// standard rates on a date without losing the premium on earlier logs.
func TestScheduleOnFastTier(t *testing.T) {
	applyTestPricing(t, `{
		"models": {
			"claude-sched-1": { "input": 5.00, "output": 25.00 }
		},
		"fast_models": {
			"claude-sched-1": {
				"input": 30.00, "output": 150.00,
				"schedule": [ { "from": "2026-06-29", "input": 5.00, "output": 25.00 } ]
			}
		},
		"families": [ { "prefix": "claude-sched-1", "model": "claude-sched-1" } ],
		"default_model": "claude-sched-1",
		"display_names": [ { "prefix": "sched-1", "name": "Sched 1" } ]
	}`)
	tests := []struct {
		name         string
		ts           time.Time
		input, cache float64
	}{
		{"premium before withdrawal", time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC), 30, 3},
		{"standard at the withdrawal date", time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC), 5, 0.5},
		{"standard after withdrawal", time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), 5, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := resolvePricing("claude-sched-1:fast", tt.ts)
			if p.Input != tt.input {
				t.Errorf("input = %g, want %g", p.Input, tt.input)
			}
			if p.CacheRead != tt.cache {
				t.Errorf("cache_read = %g, want %g", p.CacheRead, tt.cache)
			}
		})
	}
}

// The boundary is an absolute UTC instant, not a local calendar day.
func TestScheduleBoundaryIsUTC(t *testing.T) {
	applyTestPricing(t, schedulePricingJSON)
	west := time.FixedZone("UTC-7", -7*3600)
	east := time.FixedZone("UTC+5", 5*3600)

	localAug31ButUTCSep1 := time.Date(2026, 8, 31, 20, 0, 0, 0, west)
	localSep1ButUTCAug31 := time.Date(2026, 9, 1, 2, 0, 0, 0, east)

	if p := resolvePricing("claude-sched-1", localAug31ButUTCSep1); p.Input != 3.0 {
		t.Errorf("western Aug 31 (UTC Sep 1): input = %g, want 3", p.Input)
	}
	if p := resolvePricing("claude-sched-1", localSep1ButUTCAug31); p.Input != 2.0 {
		t.Errorf("eastern Sep 1 (UTC Aug 31): input = %g, want 2", p.Input)
	}
}

// Out-of-order, non-monotonic entries (price rises, dips back for a month, rises
// again) must each resolve to the greatest From <= ts.
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

	tests := []struct {
		name  string
		ts    time.Time
		input float64
	}{
		{"August, before every entry", time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), 2},
		{"September, first rise", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC), 3},
		{"November, dips back", time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC), 2},
		{"December, rises again", time.Date(2026, 12, 15, 0, 0, 0, 0, time.UTC), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if p := resolvePricing("claude-window-test", tt.ts); p.Input != tt.input {
				t.Errorf("input = %g, want %g", p.Input, tt.input)
			}
		})
	}
}

// A schedule entry omitting cache fields derives them from its own input price,
// and an entry with an unparseable from is dropped rather than fatal.
func TestScheduleNormalization(t *testing.T) {
	applyTestPricing(t, `{
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
	}`)

	assertInt(t, "kept schedule entries", len(pricingTable["claude-sched-test"].Schedule), 1)

	p := resolvePricing("claude-sched-test", time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	assertCost(t, "entry input", p.Input, 2.0)
	assertCost(t, "entry derived cache_write_5m", p.CacheWrite5m, 2.5)
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

	sep := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	cr := calcCostResult("claude-lc-test", Usage{InputTokens: 250_000}, sep)
	if !cr.LongCtx {
		t.Fatal("long-context tier vanished after the schedule boundary")
	}
	assertCost(t, "inherited long-ctx input, not the bumped base", cr.Cost, 1.5)
}

func TestNoScheduleRegression(t *testing.T) {
	applyTestPricing(t, testPricingJSON)
	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	aug := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	sep := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	if a, s := calcCost("claude-alpha-9", usage, aug), calcCost("claude-alpha-9", usage, sep); a != s {
		t.Errorf("unscheduled model changed price across a boundary: %g vs %g", a, s)
	}
}

// Identical usage either side of the boundary must sum to base+scheduled; a parser
// that dropped the timestamp would yield all-base (44) or all-scheduled (66).
func TestScheduleEndToEndByTimestamp(t *testing.T) {
	applyTestPricing(t, schedulePricingJSON)
	base := setupProject(t, "sched-project", []string{
		makeRecord("req_aug", "claude-sched-1", "2026-08-15T00:00:00Z", 1_000_000, 1_000_000, 0, 0, 0),
		makeRecord("req_sep", "claude-sched-1", "2026-09-15T00:00:00Z", 1_000_000, 1_000_000, 0, 0, 0),
	})
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	m := data.ModelUsage["claude-sched-1"]
	if m == nil {
		t.Fatal("expected claude-sched-1 bucket")
	}
	assertCost(t, "per-timestamp end-to-end cost", m.Cost, 55.0)
}

func TestFoldIterations(t *testing.T) {
	iters := []Usage{
		{InputTokens: 10, OutputTokens: 100, CacheReadInputTokens: 1000, CacheCreationInputTokens: 60, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 20, Ephemeral1hInputTokens: 40}},
		{InputTokens: 5, OutputTokens: 50, CacheReadInputTokens: 500, CacheCreationInputTokens: 30, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10, Ephemeral1hInputTokens: 20}},
	}
	tests := []struct {
		name string
		in   Usage
		want Usage
	}{
		{
			name: "no iterations is unchanged",
			in:   Usage{InputTokens: 1, OutputTokens: 2},
			want: Usage{InputTokens: 1, OutputTokens: 2},
		},
		{
			name: "top level equal to the sum is unchanged",
			in:   Usage{InputTokens: 15, OutputTokens: 150, CacheReadInputTokens: 1500, CacheCreationInputTokens: 90, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 30, Ephemeral1hInputTokens: 60}, Iterations: iters},
			want: Usage{InputTokens: 15, OutputTokens: 150, CacheReadInputTokens: 1500, CacheCreationInputTokens: 90, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 30, Ephemeral1hInputTokens: 60}},
		},
		{
			name: "top level reporting only the last iteration is raised to the sum",
			in:   Usage{InputTokens: 5, OutputTokens: 50, CacheReadInputTokens: 500, CacheCreationInputTokens: 30, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10, Ephemeral1hInputTokens: 20}, Iterations: iters},
			want: Usage{InputTokens: 15, OutputTokens: 150, CacheReadInputTokens: 1500, CacheCreationInputTokens: 90, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 30, Ephemeral1hInputTokens: 60}},
		},
		{
			name: "top level above the sum is kept",
			in:   Usage{InputTokens: 20, OutputTokens: 200, CacheReadInputTokens: 2000, CacheCreationInputTokens: 100, Iterations: iters},
			want: Usage{InputTokens: 20, OutputTokens: 200, CacheReadInputTokens: 2000, CacheCreationInputTokens: 100},
		},
		{
			name: "iterations carrying only the cache breakdown still raise the cache write",
			in: Usage{CacheCreationInputTokens: 30, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10, Ephemeral1hInputTokens: 20}, Iterations: []Usage{
				{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 20, Ephemeral1hInputTokens: 40}},
				{CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 10, Ephemeral1hInputTokens: 20}},
			}},
			want: Usage{CacheCreationInputTokens: 90, CacheCreation: &CacheCreation{Ephemeral5mInputTokens: 30, Ephemeral1hInputTokens: 60}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.foldIterations()
			if got.Iterations != nil {
				t.Errorf("iterations not cleared")
			}
			got.Iterations = nil
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v (cache %+v), want %+v (cache %+v)", got, got.CacheCreation, tt.want, tt.want.CacheCreation)
			}
		})
	}
}

func TestShortModel(t *testing.T) {
	applyTestPricing(t, testPricingJSON)
	tests := []struct{ model, want string }{
		{"claude-alpha-9", "Alpha 9"},
		{"claude-alpha-9-20260101", "Alpha 9"},
		{"claude-alpha-20250101", "Alpha"},
		{"claude-alpha-9:fast", "Alpha 9 ⚡"},
		{"claude-alpha-9-20260101:fast", "Alpha 9 ⚡"},
		{"claude-beta-1", "Beta 1"},
		{"claude-alpha-9[1m]", "Alpha 9"},
		{"claude-alpha-9-20260101[1m]", "Alpha 9"},
		{"unknown-model", "unknown-model"},
	}
	for _, tt := range tests {
		if got := shortModel(tt.model); got != tt.want {
			t.Errorf("shortModel(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

// The checks below are invariants every entry in pricing.json must satisfy, so a
// new model is covered by adding data alone. Prices are not asserted — pricing.json
// is the source of truth for what a model costs; these check that it is wired up.

// checkPriceTiers asserts the cache tiers are the documented multiples of the
// entry's own input price.
func checkPriceTiers(t *testing.T, label string, p PriceFields) {
	t.Helper()
	check := func(tier string, got, want float64) {
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: %s = %g, want %g", label, tier, got, want)
		}
	}
	if p.Input <= 0 || p.Output <= 0 {
		t.Errorf("%s: input/output must be positive, got %g/%g", label, p.Input, p.Output)
		return
	}
	// Fable 5.1 and Mythos 5.1 read cache at 0.025x; everything else at 0.1x.
	if math.Abs(p.CacheRead-p.Input*0.025) > 1e-9 {
		check("cache_read", p.CacheRead, p.Input*0.1)
	}
	check("cache_write_5m", p.CacheWrite5m, p.Input*1.25)
	check("cache_write_1h", p.CacheWrite1h, p.Input*2)
	if p.LongCtxInput == 0 {
		return
	}
	if p.LongCtxOutput <= 0 {
		t.Errorf("%s: long_ctx_input set without long_ctx_output", label)
	}
	check("long_ctx_cache_read", p.LongCtxCacheRead, p.LongCtxInput*0.1)
	check("long_ctx_cache_write_5m", p.LongCtxCacheWrite5m, p.LongCtxInput*1.25)
	check("long_ctx_cache_write_1h", p.LongCtxCacheWrite1h, p.LongCtxInput*2)
}

func someModelHasPrefix(prefix string) bool {
	for model := range pricingTable {
		if hasModelPrefix(model, prefix) {
			return true
		}
	}
	return false
}

func TestPricingDataTiers(t *testing.T) {
	for model, p := range pricingTable {
		checkPriceTiers(t, model, p.PriceFields)
		for _, c := range p.Schedule {
			checkPriceTiers(t, model+" schedule from="+c.From, c.PriceFields)
		}
	}
	for model, fast := range fastPricingTable {
		checkPriceTiers(t, model+":fast", fast.PriceFields)
		for _, c := range fast.Schedule {
			checkPriceTiers(t, model+":fast schedule from="+c.From, c.PriceFields)
		}
	}
}

func TestPricingDataModelsAreWiredUp(t *testing.T) {
	for model, p := range pricingTable {
		if shortModel(model) == model {
			t.Errorf("%s: no display_names entry", model)
		}
		// Logs carry dated IDs, so every model must resolve to itself through the
		// family prefixes as well as by exact match.
		for _, dated := range []string{model + "-20200101", model + "-20991231"} {
			if got := resolvePricing(dated, time.Time{}).PriceFields; got != p.PriceFields {
				t.Errorf("%s resolves to %g/%g, want %s at %g/%g",
					dated, got.Input, got.Output, model, p.Input, p.Output)
			}
		}
	}
}

func TestPricingDataFastModels(t *testing.T) {
	for model, fast := range fastPricingTable {
		std, ok := pricingTable[model]
		if !ok {
			t.Errorf("fast_models[%q] has no matching models entry", model)
			continue
		}
		if fast.Input < std.Input || fast.Output < std.Output {
			t.Errorf("%s: fast tier cheaper than standard (%g/%g vs %g/%g)",
				model, fast.Input, fast.Output, std.Input, std.Output)
		}
		for _, id := range []string{model + ":fast", model + "-20260101:fast"} {
			if shortModel(id) == shortModel(model) {
				t.Errorf("%s displays as %q, indistinguishable from its standard bucket",
					id, shortModel(id))
			}
		}
	}
}

// Equal-length prefixes sort arbitrarily, so a duplicate would resolve at random.
func TestPricingDataPrefixes(t *testing.T) {
	seen := map[string]bool{}
	for _, fp := range familyPrefixes {
		if _, ok := pricingTable[fp.Model]; !ok {
			t.Errorf("families[%q] points at unknown model %q", fp.Prefix, fp.Model)
		}
		if !someModelHasPrefix(fp.Prefix) {
			t.Errorf("families[%q] matches no model", fp.Prefix)
		}
		if seen[fp.Prefix] {
			t.Errorf("families[%q] is declared twice", fp.Prefix)
		}
		seen[fp.Prefix] = true
	}

	seen = map[string]bool{}
	for _, dn := range displayNames {
		if !someModelHasPrefix("claude-" + dn.Prefix) {
			t.Errorf("display_names[%q] matches no model", dn.Prefix)
		}
		if seen[dn.Prefix] {
			t.Errorf("display_names[%q] is declared twice", dn.Prefix)
		}
		seen[dn.Prefix] = true
	}
}
