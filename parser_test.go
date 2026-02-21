package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeRecord builds a JSONL line for an assistant message.
func makeRecord(requestID, model, timestamp string, input, output, cacheRead, cacheWrite5m, cacheWrite1h int) string {
	type cacheCreation struct {
		Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
	}
	type usage struct {
		InputTokens              int            `json:"input_tokens"`
		OutputTokens             int            `json:"output_tokens"`
		CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
		CacheCreation            *cacheCreation `json:"cache_creation,omitempty"`
	}
	type message struct {
		Model string `json:"model"`
		Role  string `json:"role"`
		Usage usage  `json:"usage"`
	}
	type record struct {
		Type      string  `json:"type"`
		RequestID string  `json:"requestId"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}

	var cc *cacheCreation
	if cacheWrite5m > 0 || cacheWrite1h > 0 {
		cc = &cacheCreation{Ephemeral5m: cacheWrite5m, Ephemeral1h: cacheWrite1h}
	}

	rec := record{
		Type:      "assistant",
		RequestID: requestID,
		Timestamp: timestamp,
		Message: message{
			Model: model,
			Role:  "assistant",
			Usage: usage{
				InputTokens:              input,
				OutputTokens:             output,
				CacheReadInputTokens:     cacheRead,
				CacheCreationInputTokens: cacheWrite5m + cacheWrite1h,
				CacheCreation:            cc,
			},
		},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeUserRecord(timestamp string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":"hello"},"timestamp":%q}`, timestamp)
}

func localMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

func ts(daysAgo int, hour int) string {
	t := localMidnight().AddDate(0, 0, -daysAgo).Add(time.Duration(hour) * time.Hour)
	return t.Format(time.RFC3339)
}

func dateStr(daysAgo int) string {
	return localMidnight().AddDate(0, 0, -daysAgo).Format("2006-01-02")
}

// setupProject creates a project dir with JSONL lines and returns the base dir.
func setupProject(t *testing.T, projectName string, lines []string) string {
	t.Helper()
	base := t.TempDir()
	return addProject(t, base, projectName, lines)
}

// addProject adds a project's JSONL lines to an existing base dir.
func addProject(t *testing.T, base, projectName string, lines []string) string {
	t.Helper()
	projDir := filepath.Join(base, "projects", projectName)
	if err := os.MkdirAll(projDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(filepath.Join(projDir, "session.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return base
}

// --- Deduplication ---

func TestDeduplicate_StreamingUpdates(t *testing.T) {
	// Same requestID appears twice (streaming) — last value wins
	lines := []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 10, 500, 200, 0),
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 500, 200, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 deduped record, got %d", data.TotalRecords)
	}
	opus := data.ModelUsage["claude-opus-4-6"]
	if opus == nil {
		t.Fatal("expected opus bucket")
	}
	if opus.OutputTokens != 50 {
		t.Errorf("expected output_tokens=50 (last value wins), got %d", opus.OutputTokens)
	}
}

func TestDeduplicate_DifferentRequests(t *testing.T) {
	lines := []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeRecord("req_002", "claude-opus-4-6", ts(0, 11), 200, 100, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 2 {
		t.Errorf("expected 2 records, got %d", data.TotalRecords)
	}
	opus := data.ModelUsage["claude-opus-4-6"]
	if opus.InputTokens != 300 {
		t.Errorf("expected aggregated input=300, got %d", opus.InputTokens)
	}
	if opus.OutputTokens != 150 {
		t.Errorf("expected aggregated output=150, got %d", opus.OutputTokens)
	}
}

func TestDeduplicate_SameRequestAcrossProjects(t *testing.T) {
	// Same requestID in two projects — last write wins (map semantics)
	base := setupProject(t, "project-a", []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	addProject(t, base, "project-b", []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	// Same requestID should deduplicate to 1 record
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 deduped record across projects, got %d", data.TotalRecords)
	}
}

// --- Model Aggregation ---

func TestModelAggregation_MultipleModels(t *testing.T) {
	lines := []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 500, 200, 0),
		makeRecord("req_002", "claude-sonnet-4-6", ts(0, 11), 80, 30, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(data.ModelUsage) != 2 {
		t.Errorf("expected 2 models, got %d", len(data.ModelUsage))
	}
	opus := data.ModelUsage["claude-opus-4-6"]
	if opus == nil || opus.Requests != 1 {
		t.Error("expected 1 opus request")
	}
	sonnet := data.ModelUsage["claude-sonnet-4-6"]
	if sonnet == nil || sonnet.Requests != 1 {
		t.Error("expected 1 sonnet request")
	}
}

// --- Project Filter ---

func TestProjectFilter_MatchesSubstring(t *testing.T) {
	base := setupProject(t, "my-cool-project", []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	addProject(t, base, "other-project", []string{
		makeRecord("req_002", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})

	data, err := parseLogs(base, 0, "cool")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record matching 'cool', got %d", data.TotalRecords)
	}
}

func TestProjectFilter_CaseInsensitive(t *testing.T) {
	base := setupProject(t, "MyProject", []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	data, err := parseLogs(base, 0, "myproject")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected case-insensitive match, got %d records", data.TotalRecords)
	}
}

func TestProjectFilter_NoMatch(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	data, err := parseLogs(base, 0, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 0 {
		t.Errorf("expected 0 records with filter, got %d", data.TotalRecords)
	}
}

// --- Days Filter ---

func TestDaysFilter_ExcludesOldRecords(t *testing.T) {
	lines := []string{
		makeRecord("req_today", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeRecord("req_yesterday", "claude-opus-4-6", ts(1, 10), 100, 50, 0, 0, 0),
		makeRecord("req_old", "claude-opus-4-6", ts(5, 10), 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)

	data, err := parseLogs(base, 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 2 {
		t.Errorf("expected 2 records (today + yesterday), got %d", data.TotalRecords)
	}
}

func TestDaysFilter_OneDayMeansToday(t *testing.T) {
	lines := []string{
		makeRecord("req_today", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeRecord("req_yesterday", "claude-opus-4-6", ts(1, 10), 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)

	data, err := parseLogs(base, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record (today only), got %d", data.TotalRecords)
	}
}

func TestDaysFilter_BoundaryExactCutoff(t *testing.T) {
	// A record at 00:00:00 on the cutoff day should be included
	cutoffDay := localMidnight().AddDate(0, 0, -2)
	atMidnight := cutoffDay.Format(time.RFC3339)
	beforeMidnight := cutoffDay.Add(-1 * time.Second).Format(time.RFC3339)

	lines := []string{
		makeRecord("req_at_cutoff", "claude-opus-4-6", atMidnight, 100, 50, 0, 0, 0),
		makeRecord("req_before_cutoff", "claude-opus-4-6", beforeMidnight, 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)

	// days=3: today, yesterday, 2 days ago
	data, err := parseLogs(base, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record (at cutoff included, before cutoff excluded), got %d", data.TotalRecords)
	}
}

func TestDaysFilter_ZeroMeansAllTime(t *testing.T) {
	lines := []string{
		makeRecord("req_ancient", "claude-opus-4-6", "2020-01-01T00:00:00Z", 100, 50, 0, 0, 0),
		makeRecord("req_today", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)

	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 2 {
		t.Errorf("expected 2 records with days=0, got %d", data.TotalRecords)
	}
}

func TestDaysFilter_ExcludesEmptyTimestamp(t *testing.T) {
	lines := []string{
		makeRecord("req_today", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		// Record without timestamp
		`{"type":"assistant","requestId":"req_notime","message":{"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":100,"output_tokens":50}}}`,
	}
	base := setupProject(t, "test-project", lines)

	// With days filter, empty-timestamp records should be excluded
	data, err := parseLogs(base, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record (empty timestamp excluded with days filter), got %d", data.TotalRecords)
	}

	// Without days filter, empty-timestamp records should be included
	data, err = parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 2 {
		t.Errorf("expected 2 records (empty timestamp included without days filter), got %d", data.TotalRecords)
	}
}

func TestDaysFilter_CountsCalendarDays(t *testing.T) {
	// Build one record per day for the last 10 days
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, makeRecord(
			fmt.Sprintf("req_%d", i), "claude-opus-4-6", ts(i, 12), 100, 50, 0, 0, 0,
		))
	}
	base := setupProject(t, "test-project", lines)

	data, err := parseLogs(base, 7, "")
	if err != nil {
		t.Fatal(err)
	}
	// days=7 should include today + 6 previous days = 7 records (days 0-6)
	if data.TotalRecords != 7 {
		t.Errorf("expected 7 records for days=7, got %d", data.TotalRecords)
	}
	// Verify the daily breakdown has exactly 7 dates
	if len(data.DailyUsage) != 7 {
		t.Errorf("expected 7 dates in daily breakdown, got %d", len(data.DailyUsage))
	}
}

// --- Daily and Project Aggregation ---

func TestDailyAggregation(t *testing.T) {
	lines := []string{
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeRecord("req_2", "claude-opus-4-6", ts(0, 14), 200, 100, 0, 0, 0),
		makeRecord("req_3", "claude-opus-4-6", ts(1, 10), 300, 150, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	today := dateStr(0)
	yesterday := dateStr(1)

	todayBucket := data.DailyUsage[today]["claude-opus-4-6"]
	if todayBucket == nil {
		t.Fatalf("no bucket for today %s", today)
	}
	if todayBucket.Requests != 2 {
		t.Errorf("expected 2 requests today, got %d", todayBucket.Requests)
	}
	if todayBucket.InputTokens != 300 {
		t.Errorf("expected 300 input tokens today, got %d", todayBucket.InputTokens)
	}

	yesterdayBucket := data.DailyUsage[yesterday]["claude-opus-4-6"]
	if yesterdayBucket == nil {
		t.Fatalf("no bucket for yesterday %s", yesterday)
	}
	if yesterdayBucket.Requests != 1 {
		t.Errorf("expected 1 request yesterday, got %d", yesterdayBucket.Requests)
	}
}

func TestProjectAggregation(t *testing.T) {
	base := setupProject(t, "project-alpha", []string{
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeRecord("req_2", "claude-sonnet-4-6", ts(0, 11), 80, 30, 0, 0, 0),
	})
	addProject(t, base, "project-beta", []string{
		makeRecord("req_3", "claude-opus-4-6", ts(0, 10), 200, 100, 0, 0, 0),
	})

	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	alpha := data.ProjectUsage["project-alpha"]
	if alpha == nil {
		t.Fatal("expected project-alpha in results")
	}
	if len(alpha) != 2 {
		t.Errorf("expected 2 models in project-alpha, got %d", len(alpha))
	}

	beta := data.ProjectUsage["project-beta"]
	if beta == nil {
		t.Fatal("expected project-beta in results")
	}
	if beta["claude-opus-4-6"].InputTokens != 200 {
		t.Errorf("expected 200 input for project-beta opus, got %d", beta["claude-opus-4-6"].InputTokens)
	}
}

// --- Cache Token Handling ---

func TestCacheTokenAggregation(t *testing.T) {
	lines := []string{
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 500, 200, 100),
		makeRecord("req_2", "claude-opus-4-6", ts(0, 11), 100, 50, 300, 0, 150),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	opus := data.ModelUsage["claude-opus-4-6"]
	if opus.CacheRead != 800 {
		t.Errorf("expected cache_read=800, got %d", opus.CacheRead)
	}
	if opus.CacheWrite5m != 200 {
		t.Errorf("expected cache_write_5m=200, got %d", opus.CacheWrite5m)
	}
	if opus.CacheWrite1h != 250 {
		t.Errorf("expected cache_write_1h=250, got %d", opus.CacheWrite1h)
	}
	if opus.TotalCacheWrite() != 450 {
		t.Errorf("expected total_cache_write=450, got %d", opus.TotalCacheWrite())
	}
}

func TestCacheTokenFallback_FlatField(t *testing.T) {
	// When cache_creation is nil, cache_creation_input_tokens falls back to 5m
	line := `{"type":"assistant","requestId":"req_flat","timestamp":` +
		fmt.Sprintf("%q", ts(0, 10)) +
		`,"message":{"model":"claude-opus-4-6","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":500}}}`

	base := setupProject(t, "test-project", []string{line})
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	opus := data.ModelUsage["claude-opus-4-6"]
	if opus.CacheWrite5m != 500 {
		t.Errorf("expected flat cache_creation to become cache_write_5m=500, got %d", opus.CacheWrite5m)
	}
	if opus.CacheWrite1h != 0 {
		t.Errorf("expected cache_write_1h=0, got %d", opus.CacheWrite1h)
	}
}

// --- Edge Cases ---

func TestSkipsNonAssistantRecords(t *testing.T) {
	lines := []string{
		makeUserRecord(ts(0, 10)),
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record (user record skipped), got %d", data.TotalRecords)
	}
}

func TestSkipsMalformedJSON(t *testing.T) {
	lines := []string{
		`{"type":"assistant", this is not valid json}`,
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 valid record, got %d", data.TotalRecords)
	}
	if data.ParseErrors != 1 {
		t.Errorf("expected 1 parse error, got %d", data.ParseErrors)
	}
}

func TestEmptyProject(t *testing.T) {
	base := setupProject(t, "empty-project", []string{})
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 0 {
		t.Errorf("expected 0 records, got %d", data.TotalRecords)
	}
}

func TestSkipsSyntheticErrorEntries(t *testing.T) {
	lines := []string{
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		`{"type":"assistant","requestId":"req_err","timestamp":"` + ts(0, 11) + `","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Claude AI usage limit reached|1755295200"}],"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"isApiErrorMessage":true}`,
	}
	base := setupProject(t, "test-project", lines)
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if data.TotalRecords != 1 {
		t.Errorf("expected 1 record (synthetic skipped), got %d", data.TotalRecords)
	}
	if _, ok := data.ModelUsage["<synthetic>"]; ok {
		t.Error("synthetic model should not appear in results")
	}
}

func TestNoProjectsDir(t *testing.T) {
	base := t.TempDir()
	_, err := parseLogs(base, 0, "")
	if err == nil {
		t.Error("expected error when projects dir doesn't exist")
	}
}

// --- Branch Aggregation ---

func makeRecordWithBranch(requestID, model, timestamp string, input, output, cacheRead, cacheWrite5m, cacheWrite1h int, gitBranch string) string {
	type cacheCreation struct {
		Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
	}
	type usage struct {
		InputTokens              int            `json:"input_tokens"`
		OutputTokens             int            `json:"output_tokens"`
		CacheReadInputTokens     int            `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int            `json:"cache_creation_input_tokens"`
		CacheCreation            *cacheCreation `json:"cache_creation,omitempty"`
	}
	type message struct {
		Model string `json:"model"`
		Role  string `json:"role"`
		Usage usage  `json:"usage"`
	}
	type record struct {
		Type      string  `json:"type"`
		RequestID string  `json:"requestId"`
		Timestamp string  `json:"timestamp"`
		GitBranch string  `json:"gitBranch,omitempty"`
		Message   message `json:"message"`
	}

	var cc *cacheCreation
	if cacheWrite5m > 0 || cacheWrite1h > 0 {
		cc = &cacheCreation{Ephemeral5m: cacheWrite5m, Ephemeral1h: cacheWrite1h}
	}
	rec := record{
		Type: "assistant", RequestID: requestID, Timestamp: timestamp, GitBranch: gitBranch,
		Message: message{Model: model, Role: "assistant", Usage: usage{
			InputTokens: input, OutputTokens: output,
			CacheReadInputTokens:     cacheRead,
			CacheCreationInputTokens: cacheWrite5m + cacheWrite1h,
			CacheCreation:            cc,
		}},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func TestBranchAggregation(t *testing.T) {
	base := setupProject(t, "project-alpha", []string{
		makeRecordWithBranch("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0, "main"),
		makeRecordWithBranch("req_2", "claude-opus-4-6", ts(0, 11), 200, 100, 0, 0, 0, "feature/auth"),
		makeRecordWithBranch("req_3", "claude-sonnet-4-6", ts(0, 12), 80, 30, 0, 0, 0, "main"),
	})
	addProject(t, base, "project-beta", []string{
		makeRecordWithBranch("req_4", "claude-opus-4-6", ts(0, 13), 150, 75, 0, 0, 0, "main"),
	})

	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	alpha := data.BranchUsage["project-alpha"]
	if alpha == nil {
		t.Fatal("missing project-alpha in BranchUsage")
	}
	if len(alpha) != 2 {
		t.Errorf("expected 2 branches in project-alpha, got %d", len(alpha))
	}
	if alpha["main"]["claude-opus-4-6"].Requests != 1 {
		t.Error("wrong main opus count in project-alpha")
	}
	if alpha["main"]["claude-sonnet-4-6"].Requests != 1 {
		t.Error("wrong main sonnet count in project-alpha")
	}
	if alpha["feature/auth"]["claude-opus-4-6"].Requests != 1 {
		t.Error("wrong feature/auth count in project-alpha")
	}

	beta := data.BranchUsage["project-beta"]
	if beta == nil {
		t.Fatal("missing project-beta in BranchUsage")
	}
	if beta["main"]["claude-opus-4-6"].Requests != 1 {
		t.Error("wrong main count in project-beta")
	}
}

func TestBranchAggregation_NoBranch(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeRecord("req_1", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
	})
	data, err := parseLogs(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	proj := data.BranchUsage["test-project"]
	if proj == nil {
		t.Fatal("missing project in BranchUsage")
	}
	if _, ok := proj["(no branch)"]; !ok {
		t.Error("expected (no branch) bucket for records without gitBranch")
	}
	if proj["(no branch)"]["claude-opus-4-6"].Requests != 1 {
		t.Error("wrong (no branch) request count")
	}
}

func TestTotals(t *testing.T) {
	result := &ParseResult{
		ModelUsage: map[string]*Bucket{
			"claude-opus-4-6": {
				InputTokens: 1000, OutputTokens: 500,
				CacheRead: 200, CacheWrite5m: 100, CacheWrite1h: 50,
				Cost: 1.5, Requests: 2,
			},
			"claude-haiku-4-5-20251001": {
				InputTokens: 800, OutputTokens: 300,
				CacheRead: 100, CacheWrite5m: 60, CacheWrite1h: 40,
				Cost: 0.5, Requests: 3,
			},
		},
	}

	totals := result.Totals()
	assertInt(t, "totals.Input", totals.Input, 1800)
	assertInt(t, "totals.Output", totals.Output, 800)
	assertInt(t, "totals.CacheR", totals.CacheR, 300)
	assertInt(t, "totals.CacheW5m", totals.CacheW5m, 160)
	assertInt(t, "totals.CacheW1h", totals.CacheW1h, 90)
	assertInt(t, "totals.CacheW", totals.CacheW, 250)
	assertInt(t, "totals.Requests", totals.Requests, 5)
	assertCost(t, "totals.Cost", totals.Cost, 2.0)
}

func TestDateRange(t *testing.T) {
	tests := []struct {
		name             string
		dates            []string
		wantFrom, wantTo string
	}{
		{
			name:     "multiple dates",
			dates:    []string{"2026-02-18", "2026-02-20", "2026-02-19"},
			wantFrom: "2026-02-18", wantTo: "2026-02-20",
		},
		{
			name:     "single date",
			dates:    []string{"2026-02-19"},
			wantFrom: "2026-02-19", wantTo: "2026-02-19",
		},
		{
			name:     "unknown dates excluded",
			dates:    []string{"unknown", "2026-02-18", "2026-02-20"},
			wantFrom: "2026-02-18", wantTo: "2026-02-20",
		},
		{
			name:     "only unknown",
			dates:    []string{"unknown"},
			wantFrom: "", wantTo: "",
		},
		{
			name:     "empty",
			dates:    nil,
			wantFrom: "", wantTo: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &ParseResult{DailyUsage: make(map[string]map[string]*Bucket)}
			for _, d := range tt.dates {
				r.DailyUsage[d] = map[string]*Bucket{"model": {}}
			}
			from, to := r.DateRange()
			if from != tt.wantFrom {
				t.Errorf("from = %q, want %q", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("to = %q, want %q", to, tt.wantTo)
			}
		})
	}
}
