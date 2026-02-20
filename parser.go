package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Bucket struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite5m int
	CacheWrite1h int
	Cost         float64
	Requests     int
}

func (b *Bucket) TotalCacheWrite() int { return b.CacheWrite5m + b.CacheWrite1h }

type ParseResult struct {
	ModelUsage   map[string]*Bucket
	DailyUsage   map[string]map[string]*Bucket
	ProjectUsage map[string]map[string]*Bucket
	TotalFiles   int
	TotalRecords int
	ParseErrors  int
	Duration     time.Duration
}

type jsonRecord struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Model string `json:"model"`
		Usage *Usage `json:"usage"`
	} `json:"message"`
}

type dedupRecord struct {
	Model   string
	Project string
	Date    string
	Usage   Usage
}

func parseFile(path string, cutoff time.Time, hasCutoff bool, projectSlug string, deduped map[string]*dedupRecord) (rawCount, parseErrs int, fileErr error) {
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

		if !bytes.Contains(line, []byte(`"type":"assistant"`)) && !bytes.Contains(line, []byte(`"type": "assistant"`)) {
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

		dateStr := "unknown"
		if rec.Timestamp != "" {
			parsed, err := time.Parse(time.RFC3339, rec.Timestamp)
			if err == nil {
				if hasCutoff && parsed.Before(cutoff) {
					continue
				}
				dateStr = parsed.Local().Format("2006-01-02")
			} else {
				parseErrs++
			}
		} else if hasCutoff {
			continue
		}

		rawCount++
		usage := *rec.Message.Usage

		requestID := rec.RequestID
		if requestID == "" {
			requestID = fmt.Sprintf("_noid_%s_%d", filepath.Base(path), rawCount)
		}

		deduped[requestID] = &dedupRecord{
			Model:   rec.Message.Model,
			Project: projectSlug,
			Date:    dateStr,
			Usage:   usage,
		}
	}
	if err := scanner.Err(); err != nil {
		return rawCount, parseErrs, err
	}
	return rawCount, parseErrs, nil
}

func parseLogs(baseDir string, days int, projectFilter string) (*ParseResult, error) {
	var cutoff time.Time
	if days > 0 {
		now := time.Now()
		cutoff = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days-1))
	}

	projectsDir := filepath.Join(baseDir, "projects")
	if info, err := os.Stat(projectsDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no projects directory found at %s", projectsDir)
	}

	var totalFiles, parseErrors int
	deduped := make(map[string]*dedupRecord)
	lowerFilter := strings.ToLower(projectFilter)
	hasCutoff := days > 0

	err := filepath.WalkDir(projectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
			return nil
		}

		if d.IsDir() {
			if path == projectsDir {
				return nil
			}
			if projectFilter != "" {
				rel, _ := filepath.Rel(projectsDir, path)
				slug := strings.SplitN(rel, string(filepath.Separator), 2)[0]
				if !strings.Contains(strings.ToLower(slug), lowerFilter) {
					return fs.SkipDir
				}
			}
			return nil
		}

		if !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		if hasCutoff {
			if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) {
				return nil
			}
		}

		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		projectSlug := parts[0]

		totalFiles++
		_, pErr, fErr := parseFile(path, cutoff, hasCutoff, projectSlug, deduped)
		if fErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, fErr)
			return nil
		}
		parseErrors += pErr
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := &ParseResult{
		ModelUsage:   make(map[string]*Bucket),
		DailyUsage:   make(map[string]map[string]*Bucket),
		ProjectUsage: make(map[string]map[string]*Bucket),
		TotalFiles:   totalFiles,
		TotalRecords: len(deduped),
		ParseErrors:  parseErrors,
	}

	for _, r := range deduped {
		cost := calcCost(r.Model, r.Usage)
		cache5m, cache1h := r.Usage.CacheWriteTokens()

		buckets := []*Bucket{
			getOrCreateBucket(result.ModelUsage, r.Model),
			getOrCreateNestedBucket(result.DailyUsage, r.Date, r.Model),
			getOrCreateNestedBucket(result.ProjectUsage, r.Project, r.Model),
		}

		for _, b := range buckets {
			b.InputTokens += r.Usage.InputTokens
			b.OutputTokens += r.Usage.OutputTokens
			b.CacheRead += r.Usage.CacheReadInputTokens
			b.CacheWrite5m += cache5m
			b.CacheWrite1h += cache1h
			b.Cost += cost
			b.Requests++
		}
	}

	return result, nil
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

type UsageTotals struct {
	Cost     float64
	Input    int
	Output   int
	CacheR   int
	CacheW   int
	CacheW5m int
	CacheW1h int
	Requests int
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
	}
	t.CacheW = t.CacheW5m + t.CacheW1h
	return t
}
