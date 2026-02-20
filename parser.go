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
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CacheWrite5m int     `json:"-"`
	CacheWrite1h int     `json:"-"`
	Cost         float64 `json:"cost"`
	Requests     int     `json:"requests"`
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
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if !bytes.Contains(line, []byte(`"type":"assistant"`)) {
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
			} else if len(rec.Timestamp) >= 10 {
				dateStr = rec.Timestamp[:10]
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
