package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"v1.2.3", "1.2.3"},
		{"1.2.3", "1.2.3"},
		{"v0.0.1", "0.0.1"},
		{"v0.1.2+dirty", "0.1.2"},
		{"1.0.0+build.123", "1.0.0"},
		{"v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", "0.1.7"},
		{"v0.0.9-0.20260222164611-f6b538b59c20", "0.0.9"},
		{"v1.2.3-rc1", "1.2.3"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.input); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name            string
		latest, current string
		want            bool
	}{
		{"patch bump", "v0.0.9", "v0.0.8", true},
		{"minor bump", "v0.1.0", "v0.0.8", true},
		{"major bump", "v1.0.0", "v0.9.9", true},
		{"same version", "v0.1.0", "v0.1.0", false},
		{"current is newer patch", "v0.0.8", "v0.1.0", false},
		{"current is newer major", "v0.9.9", "v1.0.0", false},
		{"without v prefix", "0.2.0", "0.1.0", true},
		{"mixed prefix", "v0.2.0", "0.1.0", true},
		{"dirty suffix same version", "v0.1.2", "v0.1.2+dirty", false},
		{"dirty suffix older", "v0.1.3", "v0.1.2+dirty", true},
		{"pseudo-version same base", "v0.1.7", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", false},
		{"pseudo-version newer release", "v0.2.0", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", true},
		{"pseudo-version older release", "v0.1.6", "v0.1.7-0.20260310152227-e2b82a9a71aa+dirty", false},
		{"unparseable latest falls back to string compare", "abc", "v0.1.0", true},
		{"unparseable same", "abc", "abc", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewer(tt.latest, tt.current); got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestPrintUpdateNoticeSuppressedForDev(t *testing.T) {
	origVersion := version
	version = "dev"
	defer func() { version = origVersion }()

	// Should not panic or print — just silently return
	printUpdateNotice(&updateResult{Latest: "v9.9.9", Stale: true})
}

func TestPrintUpdateNoticeShownForRelease(t *testing.T) {
	origVersion := version
	version = "v0.1.0"
	defer func() { version = origVersion }()

	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = origStderr }()

	printUpdateNotice(&updateResult{Latest: "v0.2.0", Stale: true})
	_ = w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	if n == 0 {
		t.Error("expected update notice output for release version")
	}
}

// servePricing points pricingURL at a local server returning body with status.
func servePricing(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	orig := pricingURL
	pricingURL = srv.URL
	t.Cleanup(func() { pricingURL = orig })
}

// countFetches swaps the background fetch for a counter.
func countFetches(t *testing.T) *int {
	t.Helper()
	var n int
	orig := startPricingFetch
	startPricingFetch = func() error { n++; return nil }
	t.Cleanup(func() { startPricingFetch = orig })
	return &n
}

func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.WriteFile(path, []byte(testPricingJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// A failed fetch never refreshes the cache's mtime, so the attempt stamp is what
// keeps a hanging upstream from being retried on every run.
func TestRefreshPricingCacheThrottle(t *testing.T) {
	tests := []struct {
		name       string
		cacheAge   time.Duration // 0 = no cache yet
		attemptAge time.Duration // 0 = no earlier attempt
		wantFetch  bool
	}{
		{"fresh cache", 5 * time.Hour, 0, false},
		{"stale cache", 7 * time.Hour, 0, true},
		{"no cache", 0, 0, true},
		{"stale cache, attempt minutes ago", 7 * time.Hour, 5 * time.Minute, false},
		{"no cache, attempt minutes ago", 0, 5 * time.Minute, false},
		{"stale cache, attempt long ago", 7 * time.Hour, time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetches := countFetches(t)
			cached := filepath.Join(t.TempDir(), "pricing.json")
			if tt.cacheAge > 0 {
				writeAged(t, cached, tt.cacheAge)
			}
			if tt.attemptAge > 0 {
				writeAged(t, cached+".attempt", tt.attemptAge)
			}
			refreshPricingCache(cached, pricingRefreshInterval)
			refreshPricingCache(cached, pricingRefreshInterval)
			want := 0
			if tt.wantFetch {
				want = 1
			}
			if *fetches != want {
				t.Errorf("fetches = %d, want %d (the second call must hit the stamp)", *fetches, want)
			}
		})
	}
}

// A model newer than the cached table refetches ahead of the 6h age limit, at most
// once per run and once per 15 minutes, so a model that never lands can't refetch forever.
func TestUnknownModelRefreshesPricing(t *testing.T) {
	tests := []struct {
		name       string
		models     []string
		cacheAge   time.Duration
		attemptAge time.Duration // 0 = no earlier attempt
		wantFetchs int
	}{
		{"newer minor in known family", []string{"claude-alpha-8"}, time.Hour, 0, 1},
		{"newer minor priced at the family's own model", []string{"claude-alpha-9-5"}, time.Hour, 0, 1},
		{"unknown claude family", []string{"claude-zeta-1"}, time.Hour, 0, 1},
		{"two unknown models fetch once", []string{"claude-alpha-8", "claude-zeta-1"}, time.Hour, 0, 1},
		{"fast mode the table lacks", []string{"claude-beta-1:fast"}, time.Hour, 0, 1},
		{"exact match", []string{"claude-alpha-7"}, time.Hour, 0, 0},
		{"known fast tier", []string{"claude-alpha-9:fast"}, time.Hour, 0, 0},
		{"dated ID of known model", []string{"claude-alpha-9-20260101"}, time.Hour, 0, 0},
		{"vertex ID of known model", []string{"claude-alpha-9@20260101"}, time.Hour, 0, 0},
		{"non-Anthropic model", []string{"gpt-5"}, time.Hour, 0, 0},
		{"cache fetched minutes ago", []string{"claude-alpha-8"}, 5 * time.Minute, 0, 0},
		{"attempt minutes ago", []string{"claude-alpha-8"}, time.Hour, 5 * time.Minute, 0},
		{"attempt long ago", []string{"claude-alpha-8"}, time.Hour, time.Hour, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyTestPricing(t, testPricingJSON)
			fetches := countFetches(t)
			cached := filepath.Join(t.TempDir(), "pricing.json")
			writeAged(t, cached, tt.cacheAge)
			if tt.attemptAge > 0 {
				writeAged(t, cached+".attempt", tt.attemptAge)
			}
			origFile, origStarted := pricingCacheFile, forcedRefreshStarted
			t.Cleanup(func() { pricingCacheFile, forcedRefreshStarted = origFile, origStarted })
			pricingCacheFile, forcedRefreshStarted = cached, false

			for _, m := range tt.models {
				resolvePricing(m, time.Time{})
			}
			if *fetches != tt.wantFetchs {
				t.Errorf("fetches = %d, want %d", *fetches, tt.wantFetchs)
			}
		})
	}
}

func TestFetchAndCachePricing(t *testing.T) {
	client := &http.Client{Timeout: time.Second}
	cached := filepath.Join(t.TempDir(), "sub", "pricing.json")

	servePricing(t, http.StatusOK, testPricingJSON)
	if !fetchAndCachePricing(cached, client) {
		t.Fatal("valid pricing should be cached")
	}
	if data, err := os.ReadFile(cached); err != nil || string(data) != testPricingJSON {
		t.Fatalf("cache = %q, %v", data, err)
	}

	for _, bad := range []struct {
		status int
		body   string
	}{{http.StatusInternalServerError, testPricingJSON}, {http.StatusOK, "<html>"}} {
		servePricing(t, bad.status, bad.body)
		if fetchAndCachePricing(cached, client) {
			t.Errorf("status %d %q should not be cached", bad.status, bad.body)
		}
		if data, _ := os.ReadFile(cached); string(data) != testPricingJSON {
			t.Errorf("a failed fetch clobbered the cache: %q", data)
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(cached)); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}
