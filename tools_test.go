package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeToolUseRecord(toolUseID, toolName, timestamp string) string {
	type contentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type message struct {
		Content []contentBlock `json:"content"`
	}
	type record struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}
	rec := record{
		Type: "assistant", Timestamp: timestamp,
		Message: message{Content: []contentBlock{
			{Type: "tool_use", ID: toolUseID, Name: toolName},
		}},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeSkillUseRecord(toolUseID, skillName, timestamp string) string {
	type inputObj struct {
		Skill string `json:"skill"`
	}
	type contentBlock struct {
		Type  string   `json:"type"`
		ID    string   `json:"id"`
		Name  string   `json:"name"`
		Input inputObj `json:"input"`
	}
	type message struct {
		Content []contentBlock `json:"content"`
	}
	type record struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}
	rec := record{
		Type: "assistant", Timestamp: timestamp,
		Message: message{Content: []contentBlock{
			{Type: "tool_use", ID: toolUseID, Name: "Skill", Input: inputObj{Skill: skillName}},
		}},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeMultiToolRecord(timestamp string, tools ...struct{ ID, Name string }) string {
	type contentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type message struct {
		Content []contentBlock `json:"content"`
	}
	type record struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}
	var blocks []contentBlock
	for _, t := range tools {
		blocks = append(blocks, contentBlock{Type: "tool_use", ID: t.ID, Name: t.Name})
	}
	rec := record{Type: "assistant", Timestamp: timestamp, Message: message{Content: blocks}}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeSkillListingRecord(timestamp string, skills []string) string {
	var lines []string
	for _, s := range skills {
		lines = append(lines, fmt.Sprintf("- %s: description of %s", s, s))
	}
	type attachment struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	type record struct {
		Type       string     `json:"type"`
		Timestamp  string     `json:"timestamp"`
		Attachment attachment `json:"attachment"`
	}
	rec := record{
		Type: "attachment", Timestamp: timestamp,
		Attachment: attachment{Type: "skill_listing", Content: strings.Join(lines, "\n")},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeAgentUseRecord(toolUseID, agentType, timestamp string) string {
	type inputObj struct {
		SubagentType string `json:"subagent_type,omitempty"`
		Prompt       string `json:"prompt"`
		Description  string `json:"description"`
	}
	type contentBlock struct {
		Type  string   `json:"type"`
		ID    string   `json:"id"`
		Name  string   `json:"name"`
		Input inputObj `json:"input"`
	}
	type message struct {
		Content []contentBlock `json:"content"`
	}
	type record struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}
	rec := record{
		Type: "assistant", Timestamp: timestamp,
		Message: message{Content: []contentBlock{
			{Type: "tool_use", ID: toolUseID, Name: "Agent", Input: inputObj{
				SubagentType: agentType, Prompt: "do something", Description: "test",
			}},
		}},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func makeToolResultRecord(toolUseID, timestamp string, isError bool) string {
	type resultBlock struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error"`
	}
	type message struct {
		Role    string        `json:"role"`
		Content []resultBlock `json:"content"`
	}
	type record struct {
		Type      string  `json:"type"`
		Timestamp string  `json:"timestamp"`
		Message   message `json:"message"`
	}
	content := "success"
	if isError {
		content = "command failed"
	}
	rec := record{
		Type: "user", Timestamp: timestamp,
		Message: message{Role: "user", Content: []resultBlock{
			{Type: "tool_result", ToolUseID: toolUseID, Content: content, IsError: isError},
		}},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

// --- parseToolFile tests ---

func TestParseToolFile_BasicToolUse(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_002", "Bash", ts(0, 11)),
		makeToolUseRecord("toolu_003", "Read", ts(0, 12)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	parseErrs, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}
	if parseErrs != 0 {
		t.Errorf("expected 0 parse errors, got %d", parseErrs)
	}
	if col.toolCounts["Read"] != 2 {
		t.Errorf("expected Read=2, got %d", col.toolCounts["Read"])
	}
	if col.toolCounts["Bash"] != 1 {
		t.Errorf("expected Bash=1, got %d", col.toolCounts["Bash"])
	}
	if !col.toolProjects["Read"]["test-project"] {
		t.Error("expected Read to track test-project")
	}
}

func TestParseToolFile_DeduplicatesByToolUseID(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)), // duplicate streaming entry
		makeToolUseRecord("toolu_002", "Edit", ts(0, 11)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolCounts["Read"] != 1 {
		t.Errorf("expected Read=1 after dedup, got %d", col.toolCounts["Read"])
	}
	if col.toolCounts["Edit"] != 1 {
		t.Errorf("expected Edit=1, got %d", col.toolCounts["Edit"])
	}
}

func TestParseToolFile_SkillExtraction(t *testing.T) {
	lines := []string{
		makeSkillUseRecord("toolu_001", "superpowers:brainstorming", ts(0, 10)),
		makeSkillUseRecord("toolu_002", "review-diff", ts(0, 11)),
		makeSkillUseRecord("toolu_003", "superpowers:brainstorming", ts(0, 12)),
		makeToolUseRecord("toolu_004", "Read", ts(0, 13)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolCounts["Skill"] != 3 {
		t.Errorf("expected Skill=3, got %d", col.toolCounts["Skill"])
	}
	if col.skillCounts["superpowers:brainstorming"] != 2 {
		t.Errorf("expected brainstorming=2, got %d", col.skillCounts["superpowers:brainstorming"])
	}
	if col.skillCounts["review-diff"] != 1 {
		t.Errorf("expected review-diff=1, got %d", col.skillCounts["review-diff"])
	}
}

func TestParseToolFile_MultipleToolsPerEntry(t *testing.T) {
	lines := []string{
		makeMultiToolRecord(ts(0, 10),
			struct{ ID, Name string }{"toolu_001", "Read"},
			struct{ ID, Name string }{"toolu_002", "Grep"},
		),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolCounts["Read"] != 1 {
		t.Errorf("expected Read=1, got %d", col.toolCounts["Read"])
	}
	if col.toolCounts["Grep"] != 1 {
		t.Errorf("expected Grep=1, got %d", col.toolCounts["Grep"])
	}
}

func TestParseToolFile_CutoffFilter(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Read", ts(5, 10)), // 5 days ago
		makeToolUseRecord("toolu_002", "Read", ts(0, 10)), // today
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	cutoff := localMidnight().AddDate(0, 0, -2) // last 3 days
	col := newToolCollector()
	_, err := parseToolFile(path, cutoff, true, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolCounts["Read"] != 1 {
		t.Errorf("expected Read=1 after cutoff, got %d", col.toolCounts["Read"])
	}
}

func TestParseToolFile_SkipsNonToolLines(t *testing.T) {
	lines := []string{
		makeUserRecord(ts(0, 10)),
		makeRecord("req_001", "claude-opus-4-6", ts(0, 10), 100, 50, 0, 0, 0),
		makeToolUseRecord("toolu_001", "Read", ts(0, 11)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if len(col.toolCounts) != 1 {
		t.Errorf("expected 1 tool, got %d: %v", len(col.toolCounts), col.toolCounts)
	}
}

func TestParseToolFile_SessionTracking(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeSkillUseRecord("toolu_002", "brainstorming", ts(0, 11)),
		makeAgentUseRecord("toolu_003", "Explore", ts(0, 12)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "sess-001", col)
	if err != nil {
		t.Fatal(err)
	}

	if !col.toolSessions["sess-001"] {
		t.Error("expected tool session sess-001 to be tracked")
	}
	if !col.skillSessions["sess-001"] {
		t.Error("expected skill session sess-001 to be tracked")
	}
	if !col.agentSessions["sess-001"] {
		t.Error("expected agent session sess-001 to be tracked")
	}
}

// --- parseSkillListing tests ---

func TestParseSkillListing(t *testing.T) {
	content := "- brainstorming: You MUST use this before any creative work\n- review-diff: Use when the user asks to review a diff\n- update-config: Use this skill to configure"
	got := parseSkillListing(content)
	want := []string{"brainstorming", "review-diff", "update-config"}
	if len(got) != len(want) {
		t.Fatalf("expected %d skills, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("skill[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSkillListing_NamespacedSkills(t *testing.T) {
	content := "- superpowers:brainstorming: description\n- scooby:investigate: description\n- slack-notifier:slack-notify: description"
	got := parseSkillListing(content)
	want := []string{"superpowers:brainstorming", "scooby:investigate", "slack-notifier:slack-notify"}
	if len(got) != len(want) {
		t.Fatalf("expected %d skills, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("skill[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSkillListing_Empty(t *testing.T) {
	got := parseSkillListing("")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestParseToolFile_SkillListing(t *testing.T) {
	lines := []string{
		makeSkillListingRecord(ts(0, 10), []string{"brainstorming", "review-diff", "update-config"}),
		makeToolUseRecord("toolu_001", "Read", ts(0, 11)),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if len(col.latestSkills) != 3 {
		t.Fatalf("expected 3 available skills, got %d: %v", len(col.latestSkills), col.latestSkills)
	}
	if col.latestSkills[0] != "brainstorming" {
		t.Errorf("expected first skill = brainstorming, got %q", col.latestSkills[0])
	}
}

func TestParseToolFile_SkillListingKeepsLatest(t *testing.T) {
	lines := []string{
		makeSkillListingRecord(ts(1, 10), []string{"old-skill"}),
		makeSkillListingRecord(ts(0, 10), []string{"new-skill-a", "new-skill-b"}),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if len(col.latestSkills) != 2 {
		t.Fatalf("expected 2 skills from latest listing, got %d: %v", len(col.latestSkills), col.latestSkills)
	}
}

// --- parseTools integration tests ---

func TestParseTools_MultiProject(t *testing.T) {
	base := setupProject(t, "project-alpha", []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_002", "Bash", ts(0, 11)),
		makeSkillUseRecord("toolu_003", "brainstorming", ts(0, 12)),
	})
	addProject(t, base, "project-beta", []string{
		makeToolUseRecord("toolu_004", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_005", "Edit", ts(0, 11)),
	})

	result, err := parseTools(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolCounts["Read"] != 2 {
		t.Errorf("expected Read=2, got %d", result.ToolCounts["Read"])
	}
	if result.ToolCounts["Bash"] != 1 {
		t.Errorf("expected Bash=1, got %d", result.ToolCounts["Bash"])
	}
	if result.ToolCounts["Edit"] != 1 {
		t.Errorf("expected Edit=1, got %d", result.ToolCounts["Edit"])
	}
	if len(result.ToolProjects["Read"]) != 2 {
		t.Errorf("expected Read in 2 projects, got %d", len(result.ToolProjects["Read"]))
	}
	if result.SkillCounts["brainstorming"] != 1 {
		t.Errorf("expected brainstorming=1, got %d", result.SkillCounts["brainstorming"])
	}
	if result.TotalFiles != 2 {
		t.Errorf("expected 2 files, got %d", result.TotalFiles)
	}
	if len(result.ToolSessions) != 1 {
		t.Errorf("expected 1 tool session, got %d", len(result.ToolSessions))
	}
	if len(result.SkillSessions) != 1 {
		t.Errorf("expected 1 skill session, got %d", len(result.SkillSessions))
	}
}

func TestParseTools_ProjectFilter(t *testing.T) {
	base := setupProject(t, "project-alpha", []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
	})
	addProject(t, base, "project-beta", []string{
		makeToolUseRecord("toolu_002", "Edit", ts(0, 10)),
	})

	result, err := parseTools(base, 0, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolCounts["Read"] != 1 {
		t.Errorf("expected Read=1, got %d", result.ToolCounts["Read"])
	}
	if result.ToolCounts["Edit"] != 0 {
		t.Errorf("expected Edit=0 (filtered out), got %d", result.ToolCounts["Edit"])
	}
}

func TestParseTools_DaysCutoff(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeToolUseRecord("toolu_001", "Read", ts(10, 10)), // 10 days ago
		makeToolUseRecord("toolu_002", "Read", ts(0, 10)),  // today
	})

	result, err := parseTools(base, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.ToolCounts["Read"] != 1 {
		t.Errorf("expected Read=1 after cutoff, got %d", result.ToolCounts["Read"])
	}
}

func TestParseTools_NoProjectMatch(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
	})

	result, err := parseTools(base, 0, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCounts) != 0 {
		t.Errorf("expected empty results, got %v", result.ToolCounts)
	}
}

func TestParseTools_WithSkillListing(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeSkillListingRecord(ts(0, 10), []string{"brainstorming", "review-diff", "update-config"}),
		makeSkillUseRecord("toolu_001", "brainstorming", ts(0, 11)),
	})

	result, err := parseTools(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AvailableSkills) != 3 {
		t.Errorf("expected 3 available skills, got %d", len(result.AvailableSkills))
	}
	if result.SkillCounts["brainstorming"] != 1 {
		t.Errorf("expected brainstorming=1, got %d", result.SkillCounts["brainstorming"])
	}
}

// --- Tool error tests ---

func TestParseToolFile_ToolErrors(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Bash", ts(0, 10)),
		makeToolResultRecord("toolu_001", ts(0, 10), true),
		makeToolUseRecord("toolu_002", "Bash", ts(0, 11)),
		makeToolResultRecord("toolu_002", ts(0, 11), false),
		makeToolUseRecord("toolu_003", "Edit", ts(0, 12)),
		makeToolResultRecord("toolu_003", ts(0, 12), true),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolErrors["Bash"] != 1 {
		t.Errorf("expected Bash errors=1, got %d", col.toolErrors["Bash"])
	}
	if col.toolErrors["Edit"] != 1 {
		t.Errorf("expected Edit errors=1, got %d", col.toolErrors["Edit"])
	}
}

func TestParseToolFile_NoErrorsTrackedAsZero(t *testing.T) {
	lines := []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeToolResultRecord("toolu_001", ts(0, 10), false),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.toolErrors["Read"] != 0 {
		t.Errorf("expected Read errors=0, got %d", col.toolErrors["Read"])
	}
}

// --- Agent breakdown tests ---

func TestParseToolFile_AgentTypes(t *testing.T) {
	lines := []string{
		makeAgentUseRecord("toolu_001", "Explore", ts(0, 10)),
		makeAgentUseRecord("toolu_002", "Explore", ts(0, 11)),
		makeAgentUseRecord("toolu_003", "code-reviewer", ts(0, 12)),
		makeAgentUseRecord("toolu_004", "", ts(0, 13)), // no type = general-purpose
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	if col.agentCounts["Explore"] != 2 {
		t.Errorf("expected Explore=2, got %d", col.agentCounts["Explore"])
	}
	if col.agentCounts["code-reviewer"] != 1 {
		t.Errorf("expected code-reviewer=1, got %d", col.agentCounts["code-reviewer"])
	}
	if col.agentCounts["general-purpose"] != 1 {
		t.Errorf("expected general-purpose=1, got %d", col.agentCounts["general-purpose"])
	}
	if col.toolCounts["Agent"] != 4 {
		t.Errorf("expected Agent=4, got %d", col.toolCounts["Agent"])
	}
}

func TestParseToolFile_AgentDuration(t *testing.T) {
	agentStart := ts(0, 10)
	// tool_result comes 5 minutes later
	resultTime := localMidnight().Add(10*time.Hour + 5*time.Minute).Format(time.RFC3339)

	lines := []string{
		makeAgentUseRecord("toolu_001", "Explore", agentStart),
		makeToolResultRecord("toolu_001", resultTime, false),
	}
	base := setupProject(t, "test-project", lines)
	path := filepath.Join(base, "projects", "test-project", "session.jsonl")

	col := newToolCollector()
	_, err := parseToolFile(path, time.Time{}, false, "test-project", "session", col)
	if err != nil {
		t.Fatal(err)
	}

	dur := col.agentTotalTime["Explore"]
	if dur < 4*time.Minute || dur > 6*time.Minute {
		t.Errorf("expected ~5m duration, got %v", dur)
	}
}

func TestParseTools_AgentsAndErrors(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeAgentUseRecord("toolu_001", "Explore", ts(0, 10)),
		makeToolResultRecord("toolu_001", ts(0, 10), false),
		makeToolUseRecord("toolu_002", "Bash", ts(0, 11)),
		makeToolResultRecord("toolu_002", ts(0, 11), true),
	})

	result, err := parseTools(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentCounts["Explore"] != 1 {
		t.Errorf("expected Explore=1, got %d", result.AgentCounts["Explore"])
	}
	if result.ToolErrors["Bash"] != 1 {
		t.Errorf("expected Bash errors=1, got %d", result.ToolErrors["Bash"])
	}
}

func TestPrintToolsJSON_TopN(t *testing.T) {
	base := setupProject(t, "test-project", []string{
		makeToolUseRecord("toolu_001", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_002", "Read", ts(0, 10)),
		makeToolUseRecord("toolu_003", "Bash", ts(0, 11)),
		makeToolUseRecord("toolu_004", "Edit", ts(0, 12)),
		makeSkillUseRecord("toolu_005", "brainstorming", ts(0, 13)),
		makeSkillUseRecord("toolu_006", "review-diff", ts(0, 14)),
		makeSkillUseRecord("toolu_007", "update-config", ts(0, 15)),
		makeAgentUseRecord("toolu_008", "Explore", ts(0, 16)),
		makeAgentUseRecord("toolu_009", "general-purpose", ts(0, 17)),
		makeAgentUseRecord("toolu_010", "Plan", ts(0, 18)),
	})

	result, err := parseTools(base, 0, "")
	if err != nil {
		t.Fatal(err)
	}

	// Capture JSON output with topN=2
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printToolsJSON(result, 2)
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)

	var out struct {
		Tools  []json.RawMessage `json:"tools"`
		Agents []json.RawMessage `json:"agents"`
		Skills []json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if len(out.Tools) != 2 {
		t.Errorf("expected 2 tools with topN=2, got %d", len(out.Tools))
	}
	if len(out.Agents) != 2 {
		t.Errorf("expected 2 agents with topN=2, got %d", len(out.Agents))
	}
	if len(out.Skills) != 2 {
		t.Errorf("expected 2 skills with topN=2, got %d", len(out.Skills))
	}
}
