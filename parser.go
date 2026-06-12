package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Bucket struct {
	InputTokens     int
	OutputTokens    int
	CacheRead       int
	CacheWrite5m    int
	CacheWrite1h    int
	Cost            float64
	Requests        int
	WebSearches     int
	LongCtxRequests int
}

func (b *Bucket) TotalCacheWrite() int { return b.CacheWrite5m + b.CacheWrite1h }

type ParseResult struct {
	ModelUsage   map[string]*Bucket
	DailyUsage   map[string]map[string]*Bucket
	ProjectUsage map[string]map[string]*Bucket
	BranchUsage  map[string]map[string]map[string]*Bucket
	ProjectPaths map[string]string
	TotalFiles   int
	TotalRecords int
	ParseErrors  int
	Duration     time.Duration
}

type jsonRecord struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Timestamp string `json:"timestamp"`
	GitBranch string `json:"gitBranch"`
	Cwd       string `json:"cwd"`
	Message   struct {
		Model string `json:"model"`
		Usage *Usage `json:"usage"`
	} `json:"message"`
}

// parseLocation controls how UTC timestamps are bucketed into calendar days.
// Defaults to time.Local; set to time.UTC via the -utc flag.
var parseLocation = time.Local

type dedupRecord struct {
	Model     string
	Project   string
	Date      string
	Branch    string
	Usage     Usage
	Timestamp time.Time
}

func hasJSONType(line []byte, typ string) bool {
	return bytes.Contains(line, []byte(`"type":"`+typ+`"`)) ||
		bytes.Contains(line, []byte(`"type": "`+typ+`"`))
}

func fastSuffix(speed string) string {
	if speed == "fast" {
		return ":fast"
	}
	return ""
}

// localMidnightCutoff returns the midnight start of the days-long window
// (inclusive of today) in parseLocation, and whether a cutoff applies at all.
func localMidnightCutoff(days int) (cutoff time.Time, hasCutoff bool) {
	if days <= 0 {
		return time.Time{}, false
	}
	now := time.Now().In(parseLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, parseLocation).AddDate(0, 0, -(days - 1)), true
}

func captureCwd(paths map[string]string, slug, cwd string) {
	if paths == nil || cwd == "" {
		return
	}
	if _, ok := paths[slug]; !ok {
		paths[slug] = cwd
	}
}

func parseDateStr(timestamp string, cutoff time.Time, hasCutoff bool) (dateStr string, ts time.Time, skip bool, parseErr bool) {
	if timestamp == "" {
		if hasCutoff {
			return "", time.Time{}, true, false
		}
		return "unknown", time.Time{}, false, false
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "unknown", time.Time{}, false, true
	}
	if hasCutoff && parsed.Before(cutoff) {
		return "", time.Time{}, true, false
	}
	return parsed.In(parseLocation).Format("2006-01-02"), parsed, false, false
}

func parseFile(path string, cutoff time.Time, hasCutoff bool, projectSlug string, deduped map[string]*dedupRecord, projectPaths map[string]string) (rawCount, parseErrs int, fileErr error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 100*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if !hasJSONType(line, "assistant") {
			continue
		}

		var rec jsonRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			parseErrs++
			continue
		}
		if rec.Message.Usage == nil || rec.Message.Model == "" {
			continue
		}
		if rec.Message.Model == "<synthetic>" {
			continue
		}

		dateStr, ts, skip, pErr := parseDateStr(rec.Timestamp, cutoff, hasCutoff)
		if pErr {
			parseErrs++
		}
		if skip {
			continue
		}

		captureCwd(projectPaths, projectSlug, rec.Cwd)

		rawCount++
		usage := *rec.Message.Usage

		branch := rec.GitBranch
		if branch == "" {
			branch = "(no branch)"
		}

		model := rec.Message.Model + fastSuffix(usage.Speed)
		requestID := rec.RequestID
		if requestID == "" {
			requestID = fmt.Sprintf("_noid_%s_%d", filepath.Base(path), rawCount)
		}

		deduped[requestID] = &dedupRecord{
			Model:     model,
			Project:   projectSlug,
			Date:      dateStr,
			Branch:    branch,
			Usage:     usage,
			Timestamp: ts,
		}
	}
	if err := scanner.Err(); err != nil {
		return rawCount, parseErrs, err
	}
	return rawCount, parseErrs, nil
}

func newParseResult() *ParseResult {
	return &ParseResult{
		ModelUsage:   make(map[string]*Bucket),
		DailyUsage:   make(map[string]map[string]*Bucket),
		ProjectUsage: make(map[string]map[string]*Bucket),
		BranchUsage:  make(map[string]map[string]map[string]*Bucket),
		ProjectPaths: make(map[string]string),
	}
}

func parseLogs(baseDir string, days int, projectFilter string) (*ParseResult, error) {
	cutoff, hasCutoff := localMidnightCutoff(days)

	projectsDir := filepath.Join(baseDir, "projects")
	if info, err := os.Stat(projectsDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no projects directory found at %s", projectsDir)
	}

	matchedSlug := resolveProjectSlug(projectsDir, projectFilter)
	if projectFilter != "" && matchedSlug == "" {
		return newParseResult(), nil
	}

	deduped := make(map[string]*dedupRecord)
	projectPaths := make(map[string]string)
	var parseErrors int

	w := &dirWalker{
		projectsDir: projectsDir,
		matchedSlug: matchedSlug,
		hasCutoff:   hasCutoff,
		cutoff:      cutoff,
		onFile: func(path, projectSlug, _ string) {
			_, pErr, fErr := parseFile(path, cutoff, hasCutoff, projectSlug, deduped, projectPaths)
			if fErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, fErr)
				return
			}
			parseErrors += pErr
		},
	}

	if err := filepath.WalkDir(projectsDir, w.walk); err != nil {
		return nil, err
	}

	result := newParseResult()
	result.ProjectPaths = projectPaths
	result.TotalFiles = w.totalFiles
	result.TotalRecords = len(deduped)
	result.ParseErrors = parseErrors

	for _, r := range deduped {
		cr := calcCostResult(r.Model, r.Usage)
		cache5m, cache1h := r.Usage.CacheWriteTokens()

		buckets := []*Bucket{
			getOrCreateBucket(result.ModelUsage, r.Model),
			getOrCreateNestedBucket(result.DailyUsage, r.Date, r.Model),
			getOrCreateNestedBucket(result.ProjectUsage, r.Project, r.Model),
			getOrCreate3LevelBucket(result.BranchUsage, r.Project, r.Branch, r.Model),
		}

		longCtxInc := 0
		if cr.LongCtx {
			longCtxInc = 1
		}

		for _, b := range buckets {
			b.InputTokens += r.Usage.InputTokens
			b.OutputTokens += r.Usage.OutputTokens
			b.CacheRead += r.Usage.CacheReadInputTokens
			b.CacheWrite5m += cache5m
			b.CacheWrite1h += cache1h
			b.Cost += cr.Cost
			b.Requests++
			b.WebSearches += cr.WebSearches
			b.LongCtxRequests += longCtxInc
		}
	}

	return result, nil
}

func resolveProjectSlug(projectsDir, filter string) string {
	if filter == "" {
		return ""
	}
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	lowerFilter := strings.ToLower(filter)
	var best string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		lower := strings.ToLower(slug)
		if lower == lowerFilter {
			return slug
		}
		if strings.Contains(lower, lowerFilter) {
			if best == "" || len(slug) < len(best) {
				best = slug
			}
		}
	}
	return best
}

func getOrCreateBucket(m map[string]*Bucket, key string) *Bucket {
	if b, ok := m[key]; ok {
		return b
	}
	b := &Bucket{}
	m[key] = b
	return b
}

func getOrCreateNestedBucket(m map[string]map[string]*Bucket, outerKey, innerKey string) *Bucket {
	inner, ok := m[outerKey]
	if !ok {
		inner = make(map[string]*Bucket)
		m[outerKey] = inner
	}
	return getOrCreateBucket(inner, innerKey)
}

func getOrCreate3LevelBucket(m map[string]map[string]map[string]*Bucket, k1, k2, k3 string) *Bucket {
	level2, ok := m[k1]
	if !ok {
		level2 = make(map[string]map[string]*Bucket)
		m[k1] = level2
	}
	return getOrCreateNestedBucket(level2, k2, k3)
}

type UsageTotals struct {
	Cost            float64
	Input           int
	Output          int
	CacheR          int
	CacheW          int
	CacheW5m        int
	CacheW1h        int
	Requests        int
	WebSearches     int
	LongCtxRequests int
}

func (r *ParseResult) DateRange() (from, to string) {
	for d := range r.DailyUsage {
		if d == "unknown" {
			continue
		}
		if from == "" || d < from {
			from = d
		}
		if to == "" || d > to {
			to = d
		}
	}
	return
}

func (r *ParseResult) Totals() UsageTotals {
	var t UsageTotals
	for _, b := range r.ModelUsage {
		t.Cost += b.Cost
		t.Input += b.InputTokens
		t.Output += b.OutputTokens
		t.CacheR += b.CacheRead
		t.CacheW5m += b.CacheWrite5m
		t.CacheW1h += b.CacheWrite1h
		t.Requests += b.Requests
		t.WebSearches += b.WebSearches
		t.LongCtxRequests += b.LongCtxRequests
	}
	t.CacheW = t.CacheW5m + t.CacheW1h
	return t
}
