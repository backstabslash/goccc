package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestResolveStatuslineConfig_Nil(t *testing.T) {
	segments, sep, opts := resolveStatuslineConfig(nil)
	if len(segments) != len(defaultSegments) {
		t.Errorf("got %d segments, want %d", len(segments), len(defaultSegments))
	}
	if sep != defaultSeparator {
		t.Errorf("sep = %q, want %q", sep, defaultSeparator)
	}
	if opts != nil {
		t.Error("expected nil options")
	}
}

func TestResolveStatuslineConfig_Custom(t *testing.T) {
	cfg := &StatuslineConfig{
		Segments:  []string{"ctx", "model"},
		Separator: " | ",
		SegmentOptions: map[string]SegmentOptions{
			"ctx": {Emoji: "🧠"},
		},
	}
	segments, sep, opts := resolveStatuslineConfig(cfg)
	if len(segments) != 2 || segments[0] != "ctx" || segments[1] != "model" {
		t.Errorf("segments = %v", segments)
	}
	if sep != " | " {
		t.Errorf("sep = %q", sep)
	}
	if opts["ctx"].Emoji != "🧠" {
		t.Errorf("ctx emoji = %q", opts["ctx"].Emoji)
	}
}

func TestResolveStatuslineConfig_PartialOverride(t *testing.T) {
	cfg := &StatuslineConfig{
		Segments: []string{"model"},
	}
	_, sep, _ := resolveStatuslineConfig(cfg)
	if sep != defaultSeparator {
		t.Errorf("expected default separator when not specified, got %q", sep)
	}
}

func TestSegmentEmoji_Default(t *testing.T) {
	emoji := segmentEmoji("ctx", "💭", nil)
	if emoji != "💭" {
		t.Errorf("got %q, want 💭", emoji)
	}
}

func TestSegmentEmoji_Override(t *testing.T) {
	opts := map[string]SegmentOptions{"ctx": {Emoji: "🧠"}}
	emoji := segmentEmoji("ctx", "💭", opts)
	if emoji != "🧠" {
		t.Errorf("got %q, want 🧠", emoji)
	}
}

func TestSegmentLabel_Default(t *testing.T) {
	label := segmentLabel("ctx", "ctx", nil)
	if label != "ctx" {
		t.Errorf("got %q, want ctx", label)
	}
}

func TestSegmentLabel_Override(t *testing.T) {
	opts := map[string]SegmentOptions{"ctx": {Label: "context"}}
	label := segmentLabel("ctx", "ctx", opts)
	if label != "context" {
		t.Errorf("got %q, want context", label)
	}
}

func makeTestInput() *StatuslineInput {
	input := &StatuslineInput{}
	input.Model.ID = "claude-opus-4-6"
	input.ContextWindow.UsedPercentage = 33
	input.ContextWindow.TotalInputTokens = 438
	input.ContextWindow.TotalOutputTokens = 6518
	input.Cost.TotalDurationMs = 1078708
	input.Cost.TotalLinesAdded = 25
	input.Cost.TotalLinesRemoved = 3
	input.Cwd = "/home/user/projects/goccc"
	input.Version = "2.1.83"
	return input
}

func TestRenderSessionCost(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	ctx := &StatuslineContext{SessionCost: 1.50, Input: makeTestInput()}
	got := renderSessionCost(ctx)
	if got != "💸 $1.50 session" {
		t.Errorf("got %q", got)
	}
}

func TestRenderSessionCost_Zero(t *testing.T) {
	ctx := &StatuslineContext{SessionCost: 0, Input: makeTestInput()}
	got := renderSessionCost(ctx)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderSessionCost_CustomEmoji(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	ctx := &StatuslineContext{
		SessionCost: 2.00,
		Input:       makeTestInput(),
		Options:     map[string]SegmentOptions{"session_cost": {Emoji: "🤑", Label: "sess"}},
	}
	got := renderSessionCost(ctx)
	if got != "🤑 $2.00 sess" {
		t.Errorf("got %q", got)
	}
}

func TestRenderTodayCost_Zero(t *testing.T) {
	ctx := &StatuslineContext{TodayCost: 0, Input: makeTestInput()}
	if got := renderTodayCost(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderCtx(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderCtx(ctx)
	if got != "💭 33% ctx" {
		t.Errorf("got %q", got)
	}
}

func TestRenderModel(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderModel(ctx)
	if got != "🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestRenderMCP_None(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	if got := renderMCP(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderMCP_Multiple(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput(), MCPNames: []string{"github", "jira"}}
	got := renderMCP(ctx)
	if !strings.Contains(got, "🔌 2 MCPs (github, jira)") {
		t.Errorf("got %q", got)
	}
}

func TestRender5h_Nil(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	if got := render5h(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRender5h_Present(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	input.RateLimits.FiveHour = &rateLimitWindow{
		UsedPercentage: 6,
		ResetsAt:       time.Now().Add(3*time.Hour + 30*time.Minute).Unix(),
	}
	ctx := &StatuslineContext{Input: input}
	got := render5h(ctx)
	if !strings.Contains(got, "94%") {
		t.Errorf("got %q, expected 94%%", got)
	}
}

func TestRender7d_Nil(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	if got := render7d(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRender7d_Present(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	input.RateLimits.SevenDay = &rateLimitWindow{
		UsedPercentage: 13,
		ResetsAt:       time.Now().Add(100 * time.Hour).Unix(),
	}
	ctx := &StatuslineContext{Input: input}
	got := render7d(ctx)
	if !strings.Contains(got, "87%") {
		t.Errorf("got %q, expected 87%%", got)
	}
	if !strings.Contains(got, "/7d") {
		t.Errorf("got %q, expected /7d label", got)
	}
}

func TestRenderTokens(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderTokens(ctx)
	if !strings.Contains(got, "📊") || !strings.Contains(got, "in") || !strings.Contains(got, "out") {
		t.Errorf("got %q", got)
	}
}

func TestRenderLines(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderLines(ctx)
	if got != "📝 +25 -3" {
		t.Errorf("got %q", got)
	}
}

func TestRenderLines_BothZero(t *testing.T) {
	input := makeTestInput()
	input.Cost.TotalLinesAdded = 0
	input.Cost.TotalLinesRemoved = 0
	ctx := &StatuslineContext{Input: input}
	if got := renderLines(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderDuration(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderDuration(ctx)
	if !strings.Contains(got, "⏱️") || !strings.Contains(got, "17m") {
		t.Errorf("got %q", got)
	}
}

func TestRenderDuration_Zero(t *testing.T) {
	input := makeTestInput()
	input.Cost.TotalDurationMs = 0
	ctx := &StatuslineContext{Input: input}
	if got := renderDuration(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderCwd(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderCwd(ctx)
	if got != "📁 goccc" {
		t.Errorf("got %q", got)
	}
}

func TestRenderCwd_FallbackToWorkspace(t *testing.T) {
	input := makeTestInput()
	input.Cwd = ""
	input.Workspace.CurrentDir = "/opt/workspace/myproject"
	ctx := &StatuslineContext{Input: input}
	got := renderCwd(ctx)
	if got != "📁 myproject" {
		t.Errorf("got %q", got)
	}
}

func TestRenderCwd_Empty(t *testing.T) {
	input := makeTestInput()
	input.Cwd = ""
	ctx := &StatuslineContext{Input: input}
	if got := renderCwd(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestRenderVersion(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput()}
	got := renderVersion(ctx)
	if got != "🏷️ 2.1.83" {
		t.Errorf("got %q", got)
	}
}

func TestRenderVersion_Empty(t *testing.T) {
	input := makeTestInput()
	input.Version = ""
	ctx := &StatuslineContext{Input: input}
	if got := renderVersion(ctx); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestFormatStatuslineWithConfig_CustomSegments(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"ctx", "model"},
	}
	got := formatStatuslineWithConfig(0.50, 2.00, input, nil, "", cfg)
	if got != "💭 33% ctx · 🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestFormatStatuslineWithConfig_CustomSeparator(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments:  []string{"ctx", "model"},
		Separator: " | ",
	}
	got := formatStatuslineWithConfig(0, 0, input, nil, "", cfg)
	if got != "💭 33% ctx | 🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestFormatStatuslineWithConfig_LineBreak(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"session_cost", "|", "ctx", "model"},
	}
	got := formatStatuslineWithConfig(1.00, 0, input, nil, "", cfg)
	if !strings.Contains(got, "\n") {
		t.Errorf("expected newline in %q", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2", len(lines))
	}
	if lines[0] != "💸 $1.00 session" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "💭 33% ctx · 🤖 Opus 4.6" {
		t.Errorf("line 1 = %q", lines[1])
	}
}

func TestFormatStatuslineWithConfig_UnknownSegmentIgnored(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"nonexistent", "model"},
	}
	got := formatStatuslineWithConfig(0, 0, input, nil, "", cfg)
	if got != "🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestFormatStatuslineWithConfig_AutoHideSkipsEmptySegments(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"session_cost", "5h", "mcp", "model"},
	}
	got := formatStatuslineWithConfig(0, 0, input, nil, "", cfg)
	if got != "🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestAssembleStatusline_ExplicitBreak(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	ctx := &StatuslineContext{
		SessionCost: 1.00,
		TodayCost:   5.00,
		Input:       input,
	}
	segments := []string{"session_cost", "|", "today_cost", "ctx"}
	result := assembleStatusline(segments, " · ", ctx)
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), result)
	}
	if lines[0] != "💸 $1.00 session" {
		t.Errorf("line 0 = %q", lines[0])
	}
	if lines[1] != "💰 $5.00 today · 💭 33% ctx" {
		t.Errorf("line 1 = %q", lines[1])
	}
}

func TestAssembleStatusline_ConsecutiveBreaks(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	ctx := &StatuslineContext{SessionCost: 1.00, Input: input}
	segments := []string{"session_cost", "|", "|", "model"}
	result := assembleStatusline(segments, " · ", ctx)
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("consecutive | should not produce empty lines, got %d: %q", len(lines), result)
	}
}

func TestRenderBranch(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"branch", "model"},
	}

	got := formatStatuslineWithConfig(0, 0, input, nil, "feature/auth", cfg)
	if got != "🌿 feature/auth · 🤖 Opus 4.6" {
		t.Errorf("got %q", got)
	}
}

func TestRenderBranch_Empty(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"branch", "model"},
	}

	got := formatStatuslineWithConfig(0, 0, input, nil, "", cfg)
	if got != "🤖 Opus 4.6" {
		t.Errorf("expected branch to be hidden when empty, got %q", got)
	}
}

func TestRenderBranch_CustomEmoji(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	cfg := &StatuslineConfig{
		Segments: []string{"branch"},
		SegmentOptions: map[string]SegmentOptions{
			"branch": {Emoji: "🔀"},
		},
	}

	got := formatStatuslineWithConfig(0, 0, input, nil, "main", cfg)
	if got != "🔀 main" {
		t.Errorf("got %q", got)
	}
}

func TestFormatRateLimitUsage_SevenDay(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	now := time.Date(2026, 3, 25, 10, 0, 0, 0, time.UTC)
	resetsAt := time.Date(2026, 3, 30, 10, 0, 0, 0, time.UTC).Unix()
	got := formatRateLimitUsage(13, resetsAt, now, sevenDayWindow, sevenDayLowBattery)
	if !strings.Contains(got, "87%") {
		t.Errorf("got %q, expected 87%%", got)
	}
	if !strings.Contains(got, "/7d") {
		t.Errorf("got %q, expected /7d label", got)
	}
}

func TestStatuslineConfigFromJSON(t *testing.T) {
	raw := `{
		"currency": "EUR",
		"cached_rate": 0.87,
		"statusline": {
			"segments": ["ctx", "model"],
			"separator": " | ",
			"segment_options": {
				"ctx": {"emoji": "🧠", "label": "context"}
			}
		}
	}`
	var cfg CurrencyConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Statusline == nil {
		t.Fatal("expected non-nil Statusline config")
	}
	if len(cfg.Statusline.Segments) != 2 {
		t.Errorf("got %d segments", len(cfg.Statusline.Segments))
	}
	if cfg.Statusline.Separator != " | " {
		t.Errorf("separator = %q", cfg.Statusline.Separator)
	}
	if cfg.Statusline.SegmentOptions["ctx"].Emoji != "🧠" {
		t.Errorf("ctx emoji = %q", cfg.Statusline.SegmentOptions["ctx"].Emoji)
	}
}

func TestStatuslineConfigFromJSON_Missing(t *testing.T) {
	raw := `{"currency": "USD"}`
	var cfg CurrencyConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Statusline != nil {
		t.Errorf("expected nil Statusline for missing key, got %+v", cfg.Statusline)
	}
}
