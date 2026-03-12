package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCurrencyConfig(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantCurr  string
		wantRate  float64
		wantWarn  float64
		wantAlert float64
	}{
		{"valid config", `{"currency":"ZAR","cached_rate":18.5,"rate_updated":"2026-03-05T12:00:00Z"}`, "ZAR", 18.5, 0, 0},
		{"empty currency", `{"currency":""}`, "", 0, 0, 0},
		{"invalid json", `{not json}`, "", 0, 0, 0},
		{"currency only", `{"currency":"EUR"}`, "EUR", 0, 0, 0},
		{"with thresholds", `{"warn_threshold":30,"alert_threshold":75}`, "", 0, 30, 75},
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
			if cfg.WarnThreshold != tt.wantWarn {
				t.Errorf("WarnThreshold = %f, want %f", cfg.WarnThreshold, tt.wantWarn)
			}
			if cfg.AlertThreshold != tt.wantAlert {
				t.Errorf("AlertThreshold = %f, want %f", cfg.AlertThreshold, tt.wantAlert)
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
		code       string
		wantSymbol string
		wantSuffix bool
	}{
		{"USD", "$", false},
		{"EUR", "€", false},
		{"GBP", "£", false},
		{"ZAR", "R", false},
		{"BRL", "R$", false},
		{"RON", "lei", true},
		{"SEK", "kr", true},
		{"UNKNOWN", "UNKNOWN", true},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			sym, suffix := symbolForCurrency(tt.code)
			if sym != tt.wantSymbol {
				t.Errorf("symbolForCurrency(%q) symbol = %q, want %q", tt.code, sym, tt.wantSymbol)
			}
			if suffix != tt.wantSuffix {
				t.Errorf("symbolForCurrency(%q) suffix = %v, want %v", tt.code, suffix, tt.wantSuffix)
			}
		})
	}
}

func TestFmtCostWithCurrency(t *testing.T) {
	origCode := activeCurrency.Code
	origSym := activeCurrency.Symbol
	origRate := activeCurrency.Rate
	origSuffix := activeCurrency.Suffix
	defer func() {
		activeCurrency.Code = origCode
		activeCurrency.Symbol = origSym
		activeCurrency.Rate = origRate
		activeCurrency.Suffix = origSuffix
	}()

	tests := []struct {
		name   string
		symbol string
		suffix bool
		rate   float64
		cost   float64
		want   string
	}{
		{"USD default", "", false, 0, 1.50, "$1.50"},
		{"USD small", "", false, 0, 0.005, "$0.01"},
		{"ZAR large", "R", false, 18.5, 1.00, "R18.50"},
		{"ZAR small", "R", false, 18.5, 0.01, "R0.18"},
		{"EUR large", "€", false, 0.92, 10.0, "€9.20"},
		{"RON large", "lei", true, 4.9, 10.0, "49.00 lei"},
		{"RON small", "lei", true, 4.9, 0.01, "0.05 lei"},
		{"SEK suffix", "kr", true, 10.5, 2.0, "21.00 kr"},
		{"unknown suffix", "XYZ", true, 1.5, 1.0, "1.50 XYZ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeCurrency.Symbol = tt.symbol
			activeCurrency.Rate = tt.rate
			activeCurrency.Suffix = tt.suffix
			got := fmtCost(tt.cost)
			if got != tt.want {
				t.Errorf("fmtCost(%f) with rate=%f symbol=%q suffix=%v = %q, want %q",
					tt.cost, tt.rate, tt.symbol, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestSaveCurrencyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := CurrencyConfig{
		Currency:       "ZAR",
		CachedRate:     18.5,
		RateUpdated:    "2026-03-05T12:00:00Z",
		WarnThreshold:  30,
		AlertThreshold: 75,
	}
	saveCurrencyConfig(path, cfg)

	loaded := loadCurrencyConfig(path)
	if loaded.Currency != "ZAR" {
		t.Errorf("Currency = %q, want ZAR", loaded.Currency)
	}
	if loaded.CachedRate != 18.5 {
		t.Errorf("CachedRate = %f, want 18.5", loaded.CachedRate)
	}
	if loaded.WarnThreshold != 30 {
		t.Errorf("WarnThreshold = %f, want 30", loaded.WarnThreshold)
	}
	if loaded.AlertThreshold != 75 {
		t.Errorf("AlertThreshold = %f, want 75", loaded.AlertThreshold)
	}
}

func TestResolveCurrencyRateUSD(t *testing.T) {
	cfg := &CurrencyConfig{Currency: "USD"}
	sym, _, rate := resolveCurrencyRate(cfg, "")
	if sym != "$" || rate != 0 {
		t.Errorf("USD should return $, 0; got %q, %f", sym, rate)
	}
}

func TestResolveCurrencyRateEmpty(t *testing.T) {
	cfg := &CurrencyConfig{Currency: ""}
	sym, _, rate := resolveCurrencyRate(cfg, "")
	if sym != "$" || rate != 0 {
		t.Errorf("empty currency should return $, 0; got %q, %f", sym, rate)
	}
}

func TestInitConfigSwapsThresholds(t *testing.T) {
	origWarn := costThresholdYellow
	origAlert := costThresholdRed
	origCurrency := activeCurrency
	defer func() {
		costThresholdYellow = origWarn
		costThresholdRed = origAlert
		activeCurrency = origCurrency
	}()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".goccc.json")
	if err := os.WriteFile(cfgPath, []byte(`{"warn_threshold":75,"alert_threshold":30}`), 0644); err != nil {
		t.Fatal(err)
	}

	origConfigPath := configPath
	configPath = func() string { return cfgPath }
	defer func() { configPath = origConfigPath }()

	costThresholdYellow = 25.0
	costThresholdRed = 50.0
	if err := initConfig("", 0); err != nil {
		t.Fatal(err)
	}

	if costThresholdYellow != 30 {
		t.Errorf("costThresholdYellow = %f, want 30 (swapped)", costThresholdYellow)
	}
	if costThresholdRed != 75 {
		t.Errorf("costThresholdRed = %f, want 75 (swapped)", costThresholdRed)
	}
}

func TestResolveCurrencyRateCached(t *testing.T) {
	cfg := &CurrencyConfig{
		Currency:    "ZAR",
		CachedRate:  18.5,
		RateUpdated: "2099-01-01T00:00:00Z", // far future = not stale
	}
	sym, suffix, rate := resolveCurrencyRate(cfg, "")
	if sym != "R" {
		t.Errorf("symbol = %q, want R", sym)
	}
	if suffix != false {
		t.Errorf("suffix = %v, want false", suffix)
	}
	if rate != 18.5 {
		t.Errorf("rate = %f, want 18.5", rate)
	}
}
