package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCurrencyConfig(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantCurr string
		wantRate float64
	}{
		{"valid config", `{"currency":"ZAR","cached_rate":18.5,"rate_updated":"2026-03-05T12:00:00Z"}`, "ZAR", 18.5},
		{"empty currency", `{"currency":""}`, "", 0},
		{"invalid json", `{not json}`, "", 0},
		{"currency only", `{"currency":"EUR"}`, "EUR", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}
			cfg := loadCurrencyConfig(path)
			if cfg.Currency != tt.wantCurr {
				t.Errorf("Currency = %q, want %q", cfg.Currency, tt.wantCurr)
			}
			if cfg.CachedRate != tt.wantRate {
				t.Errorf("CachedRate = %f, want %f", cfg.CachedRate, tt.wantRate)
			}
		})
	}
}

func TestLoadCurrencyConfigMissing(t *testing.T) {
	cfg := loadCurrencyConfig("/nonexistent/path/config.json")
	if cfg.Currency != "" {
		t.Errorf("expected empty currency for missing file, got %q", cfg.Currency)
	}
}

func TestLoadCurrencyConfigEmptyPath(t *testing.T) {
	cfg := loadCurrencyConfig("")
	if cfg.Currency != "" {
		t.Errorf("expected empty currency for empty path, got %q", cfg.Currency)
	}
}

func TestSymbolForCurrency(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"USD", "$"},
		{"EUR", "€"},
		{"GBP", "£"},
		{"ZAR", "R"},
		{"BRL", "R$"},
		{"UNKNOWN", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := symbolForCurrency(tt.code)
			if got != tt.want {
				t.Errorf("symbolForCurrency(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestFmtCostWithCurrency(t *testing.T) {
	// Save and restore activeCurrency
	origCode := activeCurrency.Code
	origSym := activeCurrency.Symbol
	origRate := activeCurrency.Rate
	defer func() {
		activeCurrency.Code = origCode
		activeCurrency.Symbol = origSym
		activeCurrency.Rate = origRate
	}()

	tests := []struct {
		name   string
		symbol string
		rate   float64
		cost   float64
		want   string
	}{
		{"USD default", "", 0, 1.50, "$1.50"},
		{"USD small", "", 0, 0.005, "$0.0050"},
		{"ZAR large", "R", 18.5, 1.00, "R18.50"},
		{"ZAR small", "R", 18.5, 0.01, "R0.1850"},
		{"EUR large", "€", 0.92, 10.0, "€9.20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeCurrency.Symbol = tt.symbol
			activeCurrency.Rate = tt.rate
			got := fmtCost(tt.cost)
			if got != tt.want {
				t.Errorf("fmtCost(%f) with rate=%f symbol=%q = %q, want %q",
					tt.cost, tt.rate, tt.symbol, got, tt.want)
			}
		})
	}
}

func TestSaveCurrencyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := CurrencyConfig{
		Currency:    "ZAR",
		CachedRate:  18.5,
		RateUpdated: "2026-03-05T12:00:00Z",
	}
	saveCurrencyConfig(path, cfg)

	loaded := loadCurrencyConfig(path)
	if loaded.Currency != "ZAR" {
		t.Errorf("Currency = %q, want ZAR", loaded.Currency)
	}
	if loaded.CachedRate != 18.5 {
		t.Errorf("CachedRate = %f, want 18.5", loaded.CachedRate)
	}
}

func TestResolveCurrencyRateUSD(t *testing.T) {
	cfg := &CurrencyConfig{Currency: "USD"}
	sym, rate := resolveCurrencyRate(cfg, "")
	if sym != "$" || rate != 0 {
		t.Errorf("USD should return $, 0; got %q, %f", sym, rate)
	}
}

func TestResolveCurrencyRateEmpty(t *testing.T) {
	cfg := &CurrencyConfig{Currency: ""}
	sym, rate := resolveCurrencyRate(cfg, "")
	if sym != "$" || rate != 0 {
		t.Errorf("empty currency should return $, 0; got %q, %f", sym, rate)
	}
}

func TestResolveCurrencyRateCached(t *testing.T) {
	cfg := &CurrencyConfig{
		Currency:    "ZAR",
		CachedRate:  18.5,
		RateUpdated: "2099-01-01T00:00:00Z", // far future = not stale
	}
	sym, rate := resolveCurrencyRate(cfg, "")
	if sym != "R" {
		t.Errorf("symbol = %q, want R", sym)
	}
	if rate != 18.5 {
		t.Errorf("rate = %f, want 18.5", rate)
	}
}
