package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	ctxThresholdRed    = 70.0
	ctxThresholdYellow = 50.0

	fiveHourWindow          = 5 * time.Hour
	fiveHourThresholdRed    = 25.0
	fiveHourThresholdYellow = 50.0
	fiveHourLowBattery      = 25.0
)

type rateLimitWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	ResetsAt       int64   `json:"resets_at"`
}

type StatuslineInput struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Cost struct {
		TotalCostUSD      float64 `json:"total_cost_usd"`
		TotalDurationMs   int64   `json:"total_duration_ms"`
		TotalLinesAdded   int     `json:"total_lines_added"`
		TotalLinesRemoved int     `json:"total_lines_removed"`
	} `json:"cost"`
	ContextWindow struct {
		UsedPercentage    float64 `json:"used_percentage"`
		ContextWindowSize int     `json:"context_window_size"`
		TotalInputTokens  int     `json:"total_input_tokens"`
		TotalOutputTokens int     `json:"total_output_tokens"`
	} `json:"context_window"`
	RateLimits struct {
		FiveHour *rateLimitWindow `json:"five_hour"`
		SevenDay *rateLimitWindow `json:"seven_day"`
	} `json:"rate_limits"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Version        string `json:"version"`
	Workspace      struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
}

func readStatuslineInput(r io.Reader) (*StatuslineInput, error) {
	var input StatuslineInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	return &input, nil
}

func parseSession(transcriptPath string) (map[string]*dedupRecord, error) {
	deduped := make(map[string]*dedupRecord)

	if _, _, err := parseFile(transcriptPath, time.Time{}, false, "", deduped, nil); err != nil {
		return nil, fmt.Errorf("parsing transcript: %w", err)
	}

	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	subagentDir := filepath.Join(base, "subagents")

	// Recurse: legacy subagents sit directly in subagents/, workflow agents nest
	// under subagents/workflows/wf_*/agent-*.jsonl.
	_ = filepath.WalkDir(subagentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path != subagentDir || !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "goccc: warning: subagent %s: %v\n", filepath.Base(path), err)
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if _, _, err := parseFile(path, time.Time{}, false, "", deduped, nil); err != nil {
			fmt.Fprintf(os.Stderr, "goccc: warning: subagent %s: %v\n", filepath.Base(path), err)
		}
		return nil
	})

	return deduped, nil
}

func sessionCost(deduped map[string]*dedupRecord) float64 {
	var total float64
	for _, r := range deduped {
		total += calcCost(r.Model, r.Usage, r.Timestamp)
	}
	return total
}

func sessionBranch(deduped map[string]*dedupRecord) string {
	var latest time.Time
	var branch string
	for _, r := range deduped {
		if r.Branch != "" && r.Branch != "(no branch)" && r.Timestamp.After(latest) {
			latest = r.Timestamp
			branch = r.Branch
		}
	}
	return branch
}

func formatStatuslineWithConfig(sCost, tCost float64, input *StatuslineInput, mcpNames []string, branch string, cfg *StatuslineConfig) string {
	segments, sep, opts := resolveStatuslineConfig(cfg)
	ctx := &StatuslineContext{
		SessionCost: sCost,
		TodayCost:   tCost,
		Input:       input,
		MCPNames:    mcpNames,
		Branch:      branch,
		Options:     opts,
		Style:       statuslineStyle(cfg),
	}
	if cfg != nil {
		ctx.Pet = resolvePet(cfg.Pet)
	} else {
		ctx.Pet = defaultPet
	}
	if ctx.Style == stylePowerline {
		return assemblePowerline(segments, ctx, resolvePowerlineTheme(cfg.Powerline))
	}
	return assembleStatusline(segments, sep, ctx)
}

func runStatusline(baseDir string) {
	input, err := readStatuslineInput(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "goccc: %v\n", err)
		os.Exit(1)
	}

	segments, _, _ := resolveStatuslineConfig(statuslineConfig)

	costs := statuslineCosts(input, segments, baseDir)

	var mcpNames []string
	if hasSegment(segments, "mcp") {
		mcpNames = detectMCPs(baseDir, input.TranscriptPath)
	}

	fmt.Print(formatStatuslineWithConfig(costs.SessionCost, costs.TodayCost, input, mcpNames, costs.Branch, statuslineConfig))
}

func hasSegment(segments []string, name string) bool {
	return slices.Contains(segments, name)
}

type SessionEndInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Reason         string `json:"reason"`
}

func readSessionEndInput(r io.Reader) (*SessionEndInput, error) {
	var input SessionEndInput
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		return nil, fmt.Errorf("reading stdin: %w", err)
	}
	return &input, nil
}

func sessionModels(deduped map[string]*dedupRecord) []string {
	costByModel := make(map[string]float64)
	for _, r := range deduped {
		costByModel[r.Model] += calcCost(r.Model, r.Usage, r.Timestamp)
	}
	type mc struct {
		name string
		cost float64
	}
	var models []mc
	for model, cost := range costByModel {
		models = append(models, mc{shortModel(model), cost})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].cost > models[j].cost })
	result := make([]string, len(models))
	for i, m := range models {
		result[i] = m.name
	}
	return result
}

func sessionDuration(deduped map[string]*dedupRecord) time.Duration {
	var earliest, latest time.Time
	for _, r := range deduped {
		if r.Timestamp.IsZero() {
			continue
		}
		if earliest.IsZero() || r.Timestamp.Before(earliest) {
			earliest = r.Timestamp
		}
		if latest.IsZero() || r.Timestamp.After(latest) {
			latest = r.Timestamp
		}
	}
	if earliest.IsZero() || latest.IsZero() {
		return 0
	}
	return latest.Sub(earliest)
}

func fmtSessionDuration(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatSessionEnd(sCost, tCost float64, reqs int, dur time.Duration, models []string) string {
	var parts []string

	reqLabel := "reqs"
	if reqs == 1 {
		reqLabel = "req"
	}
	parts = append(parts, fmt.Sprintf("💸 %s session (%d %s, %s)",
		colorCost(sCost, 0), reqs, reqLabel, fmtSessionDuration(dur)))

	if tCost-sCost > 0.001 {
		parts = append(parts, "💰 "+colorCost(tCost, 0)+" today")
	}

	if len(models) > 0 {
		parts = append(parts, "🤖 "+strings.Join(models, ", "))
	}

	return strings.Join(parts, " · ")
}

func runSessionEnd(baseDir string) {
	input, err := readSessionEndInput(os.Stdin)
	if err != nil {
		return
	}
	removeSessionState(input.SessionID)

	if input.TranscriptPath == "" {
		return
	}

	deduped, err := parseSession(input.TranscriptPath)
	if err != nil {
		return
	}

	reqs := len(deduped)
	if reqs == 0 {
		return
	}

	sCost := sessionCost(deduped)
	dur := sessionDuration(deduped)
	models := sessionModels(deduped)

	var tCost float64
	if todayData, err := parseLogs(baseDir, 1, ""); err == nil {
		tCost = todayData.Totals().Cost
	}

	line := formatSessionEnd(sCost, tCost, reqs, dur, models)
	// Write to /dev/tty to bypass Claude Code's stderr capture.
	// Falls back to stderr for platforms without /dev/tty (Windows).
	w := os.Stderr
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		w = tty
		defer func() { _ = tty.Close() }()
	}
	_, _ = fmt.Fprintf(w, "\x1b[2K\r\n%s\n", line)
	os.Exit(2)
}

// With refreshInterval set, the statusline reruns every second or so; reparsing
// the session and today's logs each time burns CPU for identical numbers.
const costCacheTTL = 10 * time.Second

type costSnapshot struct {
	TranscriptSize int64     `json:"transcript_size"`
	At             time.Time `json:"at"`
	SessionCost    float64   `json:"session_cost"`
	TodayCost      float64   `json:"today_cost"`
	Branch         string    `json:"branch"`
}

func transcriptSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func loadCostSnapshot(sessionID string, size int64, now time.Time) (*costSnapshot, bool) {
	path := sessionStatePath(sessionID, ".costs.json")
	if path == "" {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var s costSnapshot
	if json.Unmarshal(data, &s) != nil {
		return nil, false
	}
	if s.TranscriptSize != size || now.Sub(s.At) >= costCacheTTL || now.Before(s.At) {
		return nil, false
	}
	return &s, true
}

func saveCostSnapshot(sessionID string, s *costSnapshot) {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_, statErr := os.Stat(sessionStatePath(sessionID, ".costs.json"))
	if writeSessionState(sessionID, ".costs.json", data) == nil && errors.Is(statErr, fs.ErrNotExist) {
		pruneSessionState(s.At)
	}
}

func computeCosts(input *StatuslineInput, segments []string, baseDir string) *costSnapshot {
	s := &costSnapshot{SessionCost: input.Cost.TotalCostUSD}
	if input.TranscriptPath != "" {
		if deduped, err := parseSession(input.TranscriptPath); err == nil {
			s.SessionCost = sessionCost(deduped)
			s.Branch = sessionBranch(deduped)
		}
	}
	if hasSegment(segments, "today_cost") {
		if todayData, err := parseLogs(baseDir, 1, ""); err == nil {
			s.TodayCost = todayData.Totals().Cost
		}
	}
	return s
}

func statuslineCosts(input *StatuslineInput, segments []string, baseDir string) *costSnapshot {
	now := time.Now()
	size := transcriptSize(input.TranscriptPath)
	if s, ok := loadCostSnapshot(input.SessionID, size, now); ok {
		return s
	}
	s := computeCosts(input, segments, baseDir)
	s.TranscriptSize = size
	s.At = now
	saveCostSnapshot(input.SessionID, s)
	return s
}
