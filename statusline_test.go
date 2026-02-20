package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestReadStatuslineInput_Valid(t *testing.T) {
	json := `{
		"model": {"id": "claude-opus-4-6", "display_name": "Opus"},
		"cost": {"total_cost_usd": 1.23},
		"context_window": {"used_percentage": 45.2},
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
	color.NoColor = true
	defer func() { color.NoColor = false }()

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
			wantSub: []string{"💸 $0.5000 session", "💰 $2.00 today", "💭 45% ctx", "🤖 Opus 4.6"},
		},
		{
			name:     "today same as session omits today",
			sCost:    1.50,
			tCost:    1.50,
			modelID:  "claude-sonnet-4-6",
			ctxPct:   80.0,
			wantSub:  []string{"💸 $1.50 session", "💭 80% ctx", "🤖 Sonnet 4.6"},
			dontWant: []string{"today"},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &StatuslineInput{}
			input.Model.ID = tt.modelID
			input.ContextWindow.UsedPercentage = tt.ctxPct

			result := formatStatusline(tt.sCost, tt.tCost, input)
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
	// Opus: 1.2335 + Haiku: 0.057625 = 1.291125
	if math.Abs(cost-1.291125) > 0.000001 {
		t.Errorf("sessionCost = %.6f, want 1.291125", cost)
	}
}
