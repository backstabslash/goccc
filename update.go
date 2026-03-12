package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateCheckInterval = 24 * time.Hour
	updateCheckTimeout  = 2 * time.Second
	releasesURL         = "https://api.github.com/repos/backstabslash/goccc/releases/latest"
	pricingURL          = "https://raw.githubusercontent.com/backstabslash/goccc/main/pricing.json"
)

type updateResult struct {
	Latest string
	Stale  bool
}

func checkForUpdate(current string) <-chan *updateResult {
	ch := make(chan *updateResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- nil
			}
		}()
		ch <- doUpdateCheck(current)
	}()
	return ch
}

func refreshPricingCache() {
	cached := pricingCachePath()
	if cached == "" {
		return
	}
	if info, err := os.Stat(cached); err == nil && time.Since(info.ModTime()) < updateCheckInterval {
		return
	}
	client := &http.Client{Timeout: updateCheckTimeout}
	fetchAndCachePricing(cached, client)
}

func doUpdateCheck(current string) *updateResult {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	cacheFile := filepath.Join(cacheDir, "goccc", "latest-version")

	if data, err := os.ReadFile(cacheFile); err == nil {
		parts := strings.SplitN(string(data), "\n", 2)
		if len(parts) == 2 {
			if ts, err := time.Parse(time.RFC3339, parts[0]); err == nil {
				if time.Since(ts) < updateCheckInterval {
					refreshPricingCache()
					latest := strings.TrimSpace(parts[1])
					if latest != "" && isNewer(latest, current) {
						return &updateResult{Latest: latest, Stale: true}
					}
					return nil
				}
			}
		}
	}

	client := &http.Client{Timeout: updateCheckTimeout}
	resp, err := client.Get(releasesURL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil || release.TagName == "" {
		return nil
	}

	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	_ = os.WriteFile(cacheFile, []byte(time.Now().Format(time.RFC3339)+"\n"+release.TagName+"\n"), 0o644)

	refreshPricingCache()

	if isNewer(release.TagName, current) {
		return &updateResult{Latest: release.TagName, Stale: true}
	}
	return nil
}

func fetchAndCachePricing(cacheFile string, client *http.Client) bool {
	resp, err := client.Get(pricingURL)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	const maxPricingSize = 10 << 20 // 10 MB
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPricingSize))
	if err != nil {
		return false
	}

	if _, err := loadPricingFrom(data); err != nil {
		return false
	}

	_ = os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	_ = os.WriteFile(cacheFile, data, 0o644)
	return true
}

func normalizeVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.Index(v, "+"); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.Index(v, "-"); idx >= 0 {
		v = v[:idx]
	}
	return v
}

func parseSemver(v string) (major, minor, patch int, ok bool) {
	parts := strings.SplitN(normalizeVersion(v), ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	patch, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, false
	}
	return major, minor, patch, true
}

func isNewer(latest, current string) bool {
	lMaj, lMin, lPat, lok := parseSemver(latest)
	cMaj, cMin, cPat, cok := parseSemver(current)
	if !lok || !cok {
		return normalizeVersion(latest) != normalizeVersion(current)
	}
	if lMaj != cMaj {
		return lMaj > cMaj
	}
	if lMin != cMin {
		return lMin > cMin
	}
	return lPat > cPat
}

func printUpdateNotice(res *updateResult) {
	if res == nil || !res.Stale || version == "dev" {
		return
	}
	fmt.Fprintf(os.Stderr, "\n  %s %s → %s\n",
		dim.wrap("Update available:"),
		yellowString(version),
		cyan.wrap(res.Latest),
	)
	fmt.Fprintf(os.Stderr, "  %s\n",
		dim.wrap("brew upgrade goccc · go install github.com/backstabslash/goccc@latest"),
	)
}
