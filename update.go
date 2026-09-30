package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	updateCheckInterval    = 24 * time.Hour
	pricingRefreshInterval = 6 * time.Hour
	pricingRetryInterval   = 15 * time.Minute
	pricingFetchTimeout    = 10 * time.Second
	updateCheckTimeout     = 2 * time.Second
	releasesURL            = "https://api.github.com/repos/backstabslash/goccc/releases/latest"
)

var pricingURL = "https://raw.githubusercontent.com/backstabslash/goccc/main/pricing.json"

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

// refreshPricingCache starts a background fetch once the cache is older than maxAge.
// The attempt is stamped first, so a failing or hanging upstream is retried at most
// once per pricingRetryInterval, and every caller shares that throttle.
func refreshPricingCache(cached string, maxAge time.Duration) {
	if cached == "" || modifiedWithin(cached, maxAge) || modifiedWithin(cached+".attempt", pricingRetryInterval) {
		return
	}
	_ = os.MkdirAll(filepath.Dir(cached), 0o755)
	if os.WriteFile(cached+".attempt", []byte(time.Now().Format(time.RFC3339)), 0o644) != nil {
		return
	}
	_ = startPricingFetch()
}

func modifiedWithin(path string, d time.Duration) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < d
}

// startPricingFetch runs the fetch as its own process, so goccc exits without
// waiting on the network and the fetch still lands.
var startPricingFetch = func() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "-refresh-pricing")
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
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

	return writeFileAtomic(cacheFile, data, 0o644) == nil
}

// writeFileAtomic replaces path in one step, so a concurrent reader never sees a partial file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
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
