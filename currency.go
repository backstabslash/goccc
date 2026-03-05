package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// activeCurrency holds the resolved currency code, symbol, and exchange rate.
// When Rate is 0, costs are displayed in USD.
var activeCurrency struct {
	Code   string
	Symbol string
	Rate   float64
}

// CurrencyConfig represents the persistent config in ~/.goccc.json.
type CurrencyConfig struct {
	Currency    string  `json:"currency"`
	CachedRate  float64 `json:"cached_rate,omitempty"`
	RateUpdated string  `json:"rate_updated,omitempty"`
}

var currencySymbols = map[string]string{
	"USD": "$",
	"EUR": "€",
	"GBP": "£",
	"JPY": "¥",
	"CNY": "¥",
	"KRW": "₩",
	"INR": "₹",
	"RUB": "₽",
	"BRL": "R$",
	"ZAR": "R",
	"CAD": "CA$",
	"AUD": "A$",
	"CHF": "CHF",
	"SEK": "kr",
	"NOK": "kr",
	"DKK": "kr",
	"PLN": "zł",
	"TRY": "₺",
	"MXN": "MX$",
	"NZD": "NZ$",
}

func symbolForCurrency(code string) string {
	if s, ok := currencySymbols[code]; ok {
		return s
	}
	return code
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".goccc.json")
}

func loadCurrencyConfig(path string) CurrencyConfig {
	if path == "" {
		return CurrencyConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CurrencyConfig{}
	}
	var cfg CurrencyConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return CurrencyConfig{}
	}
	return cfg
}

func saveCurrencyConfig(path string, cfg CurrencyConfig) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "goccc: warning: failed to save currency config: %v\n", err)
	}
}

type erAPIResponse struct {
	Result string             `json:"result"`
	Rates  map[string]float64 `json:"rates"`
}

var httpClient = &http.Client{Timeout: 2 * time.Second}

func fetchExchangeRate(currency string) (float64, error) {
	resp, err := httpClient.Get("https://open.er-api.com/v6/latest/USD")
	if err != nil {
		return 0, fmt.Errorf("fetching exchange rate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("exchange rate API returned status %d", resp.StatusCode)
	}

	var apiResp erAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return 0, fmt.Errorf("decoding exchange rate response: %w", err)
	}

	rate, ok := apiResp.Rates[currency]
	if !ok {
		return 0, fmt.Errorf("currency %q not found in exchange rate data", currency)
	}
	return rate, nil
}

// initCurrency resolves currency from CLI flags or config file and sets activeCurrency.
func initCurrency(symbolFlag string, rateFlag float64) error {
	if (symbolFlag != "") != (rateFlag != 0) {
		return fmt.Errorf("-currency-symbol and -currency-rate must be used together")
	}
	if symbolFlag != "" && rateFlag != 0 {
		if math.IsNaN(rateFlag) || math.IsInf(rateFlag, 0) || rateFlag <= 0 {
			return fmt.Errorf("-currency-rate must be a positive number")
		}
		activeCurrency.Symbol = symbolFlag
		activeCurrency.Rate = rateFlag
		return nil
	}
	cfgPath := configPath()
	cfg := loadCurrencyConfig(cfgPath)
	if cfg.Currency != "" {
		sym, rate := resolveCurrencyRate(&cfg, cfgPath)
		activeCurrency.Code = cfg.Currency
		activeCurrency.Symbol = sym
		activeCurrency.Rate = rate
	}
	return nil
}

const rateStaleDuration = 24 * time.Hour

func resolveCurrencyRate(cfg *CurrencyConfig, cfgPath string) (symbol string, rate float64) {
	if cfg.Currency == "" || cfg.Currency == "USD" {
		return "$", 0
	}

	symbol = symbolForCurrency(cfg.Currency)

	// Check if cached rate is fresh enough
	if cfg.CachedRate > 0 && cfg.RateUpdated != "" {
		if updated, err := time.Parse(time.RFC3339, cfg.RateUpdated); err == nil {
			if time.Since(updated) < rateStaleDuration {
				return symbol, cfg.CachedRate
			}
		}
	}

	// Fetch fresh rate
	newRate, err := fetchExchangeRate(cfg.Currency)
	if err != nil {
		fmt.Fprintf(os.Stderr, "goccc: warning: %v\n", err)
		// Fall back to cached rate if available
		if cfg.CachedRate > 0 {
			return symbol, cfg.CachedRate
		}
		fmt.Fprintf(os.Stderr, "goccc: warning: no cached rate available, displaying in USD\n")
		return "$", 0
	}

	// Save updated rate
	cfg.CachedRate = newRate
	cfg.RateUpdated = time.Now().UTC().Format(time.RFC3339)
	saveCurrencyConfig(cfgPath, *cfg)

	return symbol, newRate
}
