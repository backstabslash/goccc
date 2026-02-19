package main

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	TotalDeduped int
	ParseErrors  int
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
	Model        string
	Project      string
	Date         string
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheWrite5m int
	CacheWrite1h int
	Usage        Usage
}

func parseLogs(baseDir string, days int, projectFilter string) (*ParseResult, error) {
	var cutoff time.Time
	if days > 0 {
		cutoff = time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -(days - 1))
	}

	projectsDir := filepath.Join(baseDir, "projects")
	if info, err := os.Stat(projectsDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no projects directory found at %s", projectsDir)
	}

	var totalFiles, rawRecords, parseErrors int
	deduped := make(map[string]*dedupRecord)

	err := filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}

		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		projectSlug := parts[0]

		if projectFilter != "" && !strings.Contains(
			strings.ToLower(projectSlug), strings.ToLower(projectFilter),
		) {
			return nil
		}

		totalFiles++
		f, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, err)
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var rec jsonRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				parseErrors++
				continue
			}

			if rec.Type != "assistant" {
				continue
			}
			if rec.Message.Usage == nil || rec.Message.Model == "" {
				continue
			}

			if days > 0 {
				if rec.Timestamp == "" {
					continue
				}
				ts, err := time.Parse(time.RFC3339, rec.Timestamp)
				if err == nil && ts.Before(cutoff) {
					continue
				}
			}

			rawRecords++
			usage := *rec.Message.Usage

			requestID := rec.RequestID
			if requestID == "" {
				requestID = fmt.Sprintf("_noid_%d", rawRecords)
			}

			dateStr := "unknown"
			if len(rec.Timestamp) >= 10 {
				dateStr = rec.Timestamp[:10]
			}

			cache5m, cache1h := usage.CacheWriteTokens()

			deduped[requestID] = &dedupRecord{
				Model:        rec.Message.Model,
				Project:      projectSlug,
				Date:         dateStr,
				InputTokens:  usage.InputTokens,
				OutputTokens: usage.OutputTokens,
				CacheRead:    usage.CacheReadInputTokens,
				CacheWrite5m: cache5m,
				CacheWrite1h: cache1h,
				Usage:        usage,
			}
		}
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
		TotalDeduped: rawRecords - len(deduped),
		ParseErrors:  parseErrors,
	}

	for _, r := range deduped {
		cost := calcCost(r.Model, r.Usage)

		buckets := []*Bucket{
			getOrCreateBucket(result.ModelUsage, r.Model),
			getOrCreateNestedBucket(result.DailyUsage, r.Date, r.Model),
			getOrCreateNestedBucket(result.ProjectUsage, r.Project, r.Model),
		}

		for _, b := range buckets {
			b.InputTokens += r.InputTokens
			b.OutputTokens += r.OutputTokens
			b.CacheRead += r.CacheRead
			b.CacheWrite5m += r.CacheWrite5m
			b.CacheWrite1h += r.CacheWrite1h
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
