package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadStatuslineInput_Valid(t *testing.T) {
	json := `{
		"model": {"id": "claude-opus-4-6", "display_name": "Opus"},
		"cost": {"total_cost_usd": 1.23},
		"context_window": {"used_percentage": 45.2},
		"rate_limits": {"five_hour": {"used_percentage": 42, "resets_at": 1774468800}},
		"transcript_path": "/home/user/.claude/projects/my-project/abc123.jsonl",
		"session_id": "abc123"
	}`
	input, err := readStatuslineInput(strings.NewReader(json))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.Model.ID != "claude-opus-4-6" {
		t.Errorf("Model.ID = %q, want %q", input.Model.ID, "claude-opus-4-6")
	}
	if input.Cost.TotalCostUSD != 1.23 {
		t.Errorf("Cost.TotalCostUSD = %f, want 1.23", input.Cost.TotalCostUSD)
	}
	if input.ContextWindow.UsedPercentage != 45.2 {
		t.Errorf("ContextWindow.UsedPercentage = %f, want 45.2", input.ContextWindow.UsedPercentage)
	}
	if input.TranscriptPath != "/home/user/.claude/projects/my-project/abc123.jsonl" {
		t.Errorf("TranscriptPath = %q", input.TranscriptPath)
	}
	if input.RateLimits.FiveHour == nil {
		t.Fatal("expected non-nil FiveHour")
	}
	if input.RateLimits.FiveHour.UsedPercentage != 42 {
		t.Errorf("FiveHour.UsedPercentage = %f, want 42", input.RateLimits.FiveHour.UsedPercentage)
	}
	if input.RateLimits.FiveHour.ResetsAt != 1774468800 {
		t.Errorf("FiveHour.ResetsAt = %d, want 1774468800", input.RateLimits.FiveHour.ResetsAt)
	}
}

func TestReadStatuslineInput_InvalidJSON(t *testing.T) {
	_, err := readStatuslineInput(strings.NewReader("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadStatuslineInput_EmptyStdin(t *testing.T) {
	_, err := readStatuslineInput(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty stdin")
	}
}

func TestReadStatuslineInput_MissingFields(t *testing.T) {
	input, err := readStatuslineInput(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.TranscriptPath != "" {
		t.Errorf("expected empty TranscriptPath, got %q", input.TranscriptPath)
	}
	if input.RateLimits.FiveHour != nil {
		t.Errorf("expected nil FiveHour, got %+v", input.RateLimits.FiveHour)
	}
}

func TestFormatFiveHourUsage(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	tests := []struct {
		name     string
		pct      float64
		resetsAt int64
		now      time.Time
		want     string
	}{
		{
			name:     "mid window low usage",
			pct:      6,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 11, 30, 0, 0, time.UTC),
			want:     "🔋 94% (1.5/5h)",
		},
		{
			name:     "full hour no decimal",
			pct:      20,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 12, 0, 0, 0, time.UTC),
			want:     "🔋 80% (2/5h)",
		},
		{
			name:     "just started full battery",
			pct:      0,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC),
			want:     "🔋 100% (0/5h)",
		},
		{
			name:     "heavy usage low remaining",
			pct:      85,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 14, 42, 0, 0, time.UTC),
			want:     "🪫 15% (4.7/5h)",
		},
		{
			name:     "fully depleted clamps to 0%",
			pct:      100,
			resetsAt: time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 11, 0, 0, 0, time.UTC),
			want:     "🪫 0% (5/5h)",
		},
		{
			name:     "at 25% remaining switches to low battery",
			pct:      75,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 13, 0, 0, 0, time.UTC),
			want:     "🪫 25% (3/5h)",
		},
		{
			name:     "at 26% remaining stays full battery",
			pct:      74,
			resetsAt: time.Date(2026, 3, 25, 15, 0, 0, 0, time.UTC).Unix(),
			now:      time.Date(2026, 3, 25, 13, 0, 0, 0, time.UTC),
			want:     "🔋 26% (3/5h)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFiveHourUsage(tt.pct, tt.resetsAt, tt.now)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSession_MainOnly(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	content := makeRecord("req_1", "claude-opus-4-6", "2026-02-19T10:00:00Z", 1000, 500, 5000, 200, 100) + "\n" +
		makeRecord("req_2", "claude-opus-4-6", "2026-02-19T10:05:00Z", 2000, 800, 3000, 0, 0) + "\n"
	if err := os.WriteFile(transcript, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deduped, err := parseSession(transcript)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}
	if len(deduped) != 2 {
		t.Errorf("got %d records, want 2", len(deduped))
	}

	cost := sessionCost(deduped)
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}

func TestParseSession_WithSubagents(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "abc123.jsonl")
	mainContent := makeRecord("req_main", "claude-opus-4-6", "2026-02-19T10:00:00Z", 1000, 500, 5000, 0, 0) + "\n"
	if err := os.WriteFile(transcript, []byte(mainContent), 0644); err != nil {
		t.Fatal(err)
	}

	subDir := filepath.Join(dir, "abc123", "subagents")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	subContent := makeRecord("req_sub", "claude-haiku-4-5-20251001", "2026-02-19T10:01:00Z", 500, 200, 1000, 0, 0) + "\n"
	if err := os.WriteFile(filepath.Join(subDir, "agent-a1b2c3d.jsonl"), []byte(subContent), 0644); err != nil {
		t.Fatal(err)
	}

	deduped, err := parseSession(transcript)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}
	if len(deduped) != 2 {
		t.Errorf("got %d records, want 2 (main + subagent)", len(deduped))
	}
}

func TestParseSession_MissingSubagentDir(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	content := makeRecord("req_1", "claude-opus-4-6", "2026-02-19T10:00:00Z", 1000, 500, 0, 0, 0) + "\n"
	if err := os.WriteFile(transcript, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	deduped, err := parseSession(transcript)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}
	if len(deduped) != 1 {
		t.Errorf("got %d records, want 1", len(deduped))
	}
}

func TestParseSession_MissingTranscript(t *testing.T) {
	_, err := parseSession("/nonexistent/path/session.jsonl")
	if err == nil {
		t.Error("expected error for missing transcript")
	}
}

func TestSessionCost(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {
			Model: "claude-opus-4-6",
			Usage: Usage{InputTokens: 1000, OutputTokens: 500},
		},
		"req_2": {
			Model: "claude-opus-4-6",
			Usage: Usage{InputTokens: 2000, OutputTokens: 1000},
		},
	}

	// Opus 4.6: Input=$5/M, Output=$25/M
	// req_1: (1000/1M)*5 + (500/1M)*25 = 0.005 + 0.0125 = 0.0175
	// req_2: (2000/1M)*5 + (1000/1M)*25 = 0.01 + 0.025 = 0.035
	// Total: 0.0525
	cost := sessionCost(deduped)
	if math.Abs(cost-0.0525) > 0.000001 {
		t.Errorf("sessionCost = %.6f, want 0.052500", cost)
	}
}

func TestFormatStatusline(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	tests := []struct {
		name     string
		sCost    float64
		tCost    float64
		modelID  string
		ctxPct   float64
		wantSub  []string
		dontWant []string
	}{
		{
			name:    "basic output",
			sCost:   0.50,
			tCost:   2.00,
			modelID: "claude-opus-4-6",
			ctxPct:  45.0,
			wantSub: []string{"💸 $0.50 session", "💰 $2.00 today", "💭 45% ctx", "🤖 Opus 4.6"},
		},
		{
			name:    "today same as session still shows today",
			sCost:   1.50,
			tCost:   1.50,
			modelID: "claude-sonnet-4-6",
			ctxPct:  80.0,
			wantSub: []string{"💸 $1.50 session", "💰 $1.50 today", "💭 80% ctx", "🤖 Sonnet 4.6"},
		},
		{
			name:    "resumed session shows today when lower",
			sCost:   3.00,
			tCost:   1.00,
			modelID: "claude-opus-4-6",
			ctxPct:  10.0,
			wantSub: []string{"💸 $3.00 session", "💰 $1.00 today", "💭 10% ctx"},
		},
		{
			name:     "today zero omits today",
			sCost:    3.00,
			tCost:    0,
			modelID:  "claude-opus-4-6",
			ctxPct:   10.0,
			wantSub:  []string{"💸 $3.00 session"},
			dontWant: []string{"today"},
		},
		{
			name:     "session zero omits session",
			sCost:    0,
			tCost:    0,
			modelID:  "claude-sonnet-4-6",
			ctxPct:   5.0,
			dontWant: []string{"session"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &StatuslineInput{}
			input.Model.ID = tt.modelID
			input.ContextWindow.UsedPercentage = tt.ctxPct

			result := formatStatusline(tt.sCost, tt.tCost, input, nil, "")
			for _, sub := range tt.wantSub {
				if !strings.Contains(result, sub) {
					t.Errorf("output %q missing substring %q", result, sub)
				}
			}
			for _, sub := range tt.dontWant {
				if strings.Contains(result, sub) {
					t.Errorf("output %q should not contain %q", result, sub)
				}
			}
		})
	}
}

func TestFormatStatusline_With5h(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 34.0
	input.RateLimits.FiveHour = &rateLimitWindow{
		UsedPercentage: 6, ResetsAt: time.Now().Add(2 * time.Hour).Unix(),
	}

	result := formatStatusline(1.35, 1.98, input, nil, "")
	if !strings.Contains(result, "🔋 94%") {
		t.Errorf("output %q missing 5h battery segment", result)
	}
	// 5h segment should appear before model
	batteryIdx := strings.Index(result, "🔋")
	modelIdx := strings.Index(result, "🤖")
	if batteryIdx >= modelIdx {
		t.Errorf("5h segment should appear before model: %q", result)
	}
}

func TestFormatStatusline_No5h(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 45.0
	// RateLimits.FiveHour is nil (API billing)

	result := formatStatusline(0.50, 2.00, input, nil, "")
	if strings.Contains(result, "🔋") || strings.Contains(result, "🪫") {
		t.Errorf("output %q should not contain battery when FiveHour is nil", result)
	}
}

func TestFormatStatusline_WithMCPs(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 45.0

	result := formatStatusline(0.50, 2.00, input, []string{"github", "jira", "slack"}, "")
	if !strings.Contains(result, "🔌 3 MCPs (github, jira, slack)") {
		t.Errorf("output %q missing MCP section", result)
	}
	if !strings.Contains(result, "🤖 Opus 4.6") {
		t.Errorf("output %q missing model after MCPs", result)
	}
}

func TestFormatStatusline_SingleMCP(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 45.0

	result := formatStatusline(0.50, 2.00, input, []string{"context7"}, "")
	if !strings.Contains(result, "🔌 1 MCP (context7)") {
		t.Errorf("output %q missing singular MCP section", result)
	}
}

func TestFormatStatusline_NoMCPs(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 45.0

	result := formatStatusline(0.50, 2.00, input, nil, "")
	if strings.Contains(result, "🔌") {
		t.Errorf("output %q should not contain MCP section when no MCPs", result)
	}

	result2 := formatStatusline(0.50, 2.00, input, []string{}, "")
	if strings.Contains(result2, "🔌") {
		t.Errorf("output %q should not contain MCP section for empty slice", result2)
	}
}

func TestFormatStatusline_ManyMCPsTruncated(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 45.0

	mcps := []string{"asana", "context7", "firebase", "github", "jira"}
	result := formatStatusline(0.50, 2.00, input, mcps, "")
	if !strings.Contains(result, "🔌 5 MCPs (asana, context7, firebase, ...)") {
		t.Errorf("output %q missing truncated MCP section", result)
	}
}

func TestParseSession_Fixture(t *testing.T) {
	transcriptPath := filepath.Join("testdata", "projects", "C--Users-alice-git-webapp", "abc123.jsonl")
	deduped, err := parseSession(transcriptPath)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}

	if len(deduped) != 7 {
		t.Errorf("got %d deduped records, want 7", len(deduped))
	}

	cost := sessionCost(deduped)
	// Opus: 1.2485 + Haiku: 0.057625 = 1.306125
	if math.Abs(cost-1.306125) > 0.000001 {
		t.Errorf("sessionCost = %.6f, want 1.306125", cost)
	}
}

func TestSessionBranch(t *testing.T) {
	now := time.Now()
	deduped := map[string]*dedupRecord{
		"req_1": {Branch: "main", Timestamp: now.Add(-2 * time.Minute)},
		"req_2": {Branch: "feature/auth", Timestamp: now.Add(-1 * time.Minute)},
		"req_3": {Branch: "(no branch)", Timestamp: now},
	}
	got := sessionBranch(deduped)
	if got != "feature/auth" {
		t.Errorf("sessionBranch = %q, want %q", got, "feature/auth")
	}
}

func TestSessionBranch_Empty(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {Branch: "(no branch)", Timestamp: time.Now()},
	}
	got := sessionBranch(deduped)
	if got != "" {
		t.Errorf("sessionBranch = %q, want empty", got)
	}
}

func TestFormatSessionEnd(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	tests := []struct {
		name     string
		sCost    float64
		tCost    float64
		reqs     int
		dur      time.Duration
		models   []string
		wantSub  []string
		dontWant []string
	}{
		{
			name:    "full output",
			sCost:   1.87,
			tCost:   12.34,
			reqs:    14,
			dur:     23 * time.Minute,
			models:  []string{"Opus 4.6", "Haiku 4.5"},
			wantSub: []string{"💸 $1.87 session (14 reqs, 23m)", "💰 $12.34 today", "🤖 Opus 4.6, Haiku 4.5"},
		},
		{
			name:     "today omitted when less than session",
			sCost:    5.00,
			tCost:    3.00,
			reqs:     10,
			dur:      10 * time.Minute,
			models:   []string{"Opus 4.6"},
			wantSub:  []string{"💸 $5.00 session (10 reqs, 10m)", "🤖 Opus 4.6"},
			dontWant: []string{"today"},
		},
		{
			name:     "today omitted when trivially higher (float noise)",
			sCost:    5.00,
			tCost:    5.0009,
			reqs:     10,
			dur:      10 * time.Minute,
			models:   []string{"Opus 4.6"},
			wantSub:  []string{"💸 $5.00 session (10 reqs, 10m)"},
			dontWant: []string{"today"},
		},
		{
			name:     "today omitted when zero",
			sCost:    1.00,
			tCost:    0,
			reqs:     5,
			dur:      5 * time.Minute,
			models:   []string{"Sonnet 4.6"},
			wantSub:  []string{"💸 $1.00 session (5 reqs, 5m)"},
			dontWant: []string{"today"},
		},
		{
			name:    "single model no comma",
			sCost:   0.50,
			tCost:   2.00,
			reqs:    3,
			dur:     2 * time.Minute,
			models:  []string{"Haiku 4.5"},
			wantSub: []string{"🤖 Haiku 4.5"},
		},
		{
			name:     "no models omits model segment",
			sCost:    0.10,
			tCost:    0,
			reqs:     1,
			dur:      1 * time.Minute,
			models:   nil,
			wantSub:  []string{"💸 $0.10 session (1 req, 1m)"},
			dontWant: []string{"🤖"},
		},
		{
			name:    "zero duration shows <1m",
			sCost:   0.50,
			tCost:   0,
			reqs:    2,
			dur:     30 * time.Second,
			models:  []string{"Opus 4.6"},
			wantSub: []string{"💸 $0.50 session (2 reqs, <1m)"},
		},
		{
			name:    "long duration in hours",
			sCost:   10.00,
			tCost:   15.00,
			reqs:    100,
			dur:     2*time.Hour + 15*time.Minute,
			models:  []string{"Opus 4.6"},
			wantSub: []string{"💸 $10.00 session (100 reqs, 2h15m)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSessionEnd(tt.sCost, tt.tCost, tt.reqs, tt.dur, tt.models)
			for _, sub := range tt.wantSub {
				if !strings.Contains(result, sub) {
					t.Errorf("output %q missing substring %q", result, sub)
				}
			}
			for _, sub := range tt.dontWant {
				if strings.Contains(result, sub) {
					t.Errorf("output %q should not contain %q", result, sub)
				}
			}
		})
	}
}

func TestSessionModels(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {Model: "claude-opus-4-6", Usage: Usage{InputTokens: 1000, OutputTokens: 500}},
		"req_2": {Model: "claude-opus-4-6", Usage: Usage{InputTokens: 2000, OutputTokens: 1000}},
		"req_3": {Model: "claude-haiku-4-5-20251001", Usage: Usage{InputTokens: 500, OutputTokens: 200}},
	}
	models := sessionModels(deduped)
	if len(models) != 2 {
		t.Fatalf("got %d models, want 2", len(models))
	}
	if models[0] != "Opus 4.6" {
		t.Errorf("models[0] = %q, want Opus 4.6 (highest cost)", models[0])
	}
	if models[1] != "Haiku 4.5" {
		t.Errorf("models[1] = %q, want Haiku 4.5", models[1])
	}
}

func TestSessionModelsEmpty(t *testing.T) {
	models := sessionModels(map[string]*dedupRecord{})
	if len(models) != 0 {
		t.Errorf("got %d models for empty deduped, want 0", len(models))
	}
}

func TestSessionDuration(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {Timestamp: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)},
		"req_2": {Timestamp: time.Date(2026, 3, 12, 10, 23, 0, 0, time.UTC)},
		"req_3": {Timestamp: time.Date(2026, 3, 12, 10, 10, 0, 0, time.UTC)},
	}
	dur := sessionDuration(deduped)
	if dur != 23*time.Minute {
		t.Errorf("duration = %v, want 23m", dur)
	}
}

func TestSessionDurationSingleRequest(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {Timestamp: time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)},
	}
	dur := sessionDuration(deduped)
	if dur != 0 {
		t.Errorf("duration = %v, want 0 for single request", dur)
	}
}

func TestSessionDurationZeroTimestamps(t *testing.T) {
	deduped := map[string]*dedupRecord{
		"req_1": {Timestamp: time.Time{}},
		"req_2": {Timestamp: time.Time{}},
	}
	dur := sessionDuration(deduped)
	if dur != 0 {
		t.Errorf("duration = %v, want 0 for zero timestamps", dur)
	}
}

func TestReadSessionEndInput_Valid(t *testing.T) {
	jsonStr := `{
		"session_id": "abc123",
		"transcript_path": "/home/user/.claude/projects/my-project/abc123.jsonl",
		"cwd": "/home/user/project",
		"hook_event_name": "SessionEnd",
		"reason": "prompt_input_exit"
	}`
	input, err := readSessionEndInput(strings.NewReader(jsonStr))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input.TranscriptPath != "/home/user/.claude/projects/my-project/abc123.jsonl" {
		t.Errorf("TranscriptPath = %q", input.TranscriptPath)
	}
	if input.SessionID != "abc123" {
		t.Errorf("SessionID = %q", input.SessionID)
	}
}

func TestReadSessionEndInput_InvalidJSON(t *testing.T) {
	_, err := readSessionEndInput(strings.NewReader("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadSessionEndInput_Empty(t *testing.T) {
	_, err := readSessionEndInput(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty stdin")
	}
}
