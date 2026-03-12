package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	ctxThresholdRed    = 70.0
	ctxThresholdYellow = 50.0
)

type StatuslineInput struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"`
	} `json:"cost"`
	ContextWindow struct {
		UsedPercentage float64 `json:"used_percentage"`
	} `json:"context_window"`
	TranscriptPath string `json:"transcript_path"`
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

	if _, _, err := parseFile(transcriptPath, time.Time{}, false, "", deduped); err != nil {
		return nil, fmt.Errorf("parsing transcript: %w", err)
	}

	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	subagentDir := filepath.Join(base, "subagents")

	entries, err := os.ReadDir(subagentDir)
	if err != nil {
		if os.IsNotExist(err) {
			return deduped, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(subagentDir, entry.Name())
		if _, _, err := parseFile(path, time.Time{}, false, "", deduped); err != nil {
			fmt.Fprintf(os.Stderr, "goccc: warning: subagent %s: %v\n", entry.Name(), err)
		}
	}

	return deduped, nil
}

func sessionCost(deduped map[string]*dedupRecord) float64 {
	var total float64
	for _, r := range deduped {
		total += calcCost(r.Model, r.Usage)
	}
	return total
}

func formatStatusline(sCost, tCost float64, input *StatuslineInput, mcpNames []string) string {
	ctxPct := input.ContextWindow.UsedPercentage
	pctStr := fmt.Sprintf("%.0f%%", ctxPct)
	switch {
	case ctxPct >= ctxThresholdRed:
		pctStr = redString(pctStr)
	case ctxPct >= ctxThresholdYellow:
		pctStr = yellowString(pctStr)
	}
	ctxStr := pctStr + " ctx"

	modelStr := shortModel(input.Model.ID)

	var parts []string
	if sCost > 0 {
		parts = append(parts, "💸 "+colorCost(sCost, 0)+" session")
	}
	if tCost > 0 {
		parts = append(parts, "💰 "+colorCost(tCost, 0)+" today")
	}
	parts = append(parts, "💭 "+ctxStr)
	if len(mcpNames) > 0 {
		label := "MCPs"
		if len(mcpNames) == 1 {
			label = "MCP"
		}
		const maxShown = 3
		shown := mcpNames
		if len(shown) > maxShown {
			shown = shown[:maxShown]
		}
		list := strings.Join(shown, ", ")
		if len(mcpNames) > maxShown {
			list += ", ..."
		}
		parts = append(parts, fmt.Sprintf("🔌 %d %s (%s)", len(mcpNames), label, list))
	}
	parts = append(parts, "🤖 "+modelStr)

	return strings.Join(parts, " · ")
}

func runStatusline(baseDir string, noMCP bool) {
	input, err := readStatuslineInput(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "goccc: %v\n", err)
		os.Exit(1)
	}

	var sCost float64
	if input.TranscriptPath != "" {
		deduped, err := parseSession(input.TranscriptPath)
		if err == nil {
			sCost = sessionCost(deduped)
		} else {
			sCost = input.Cost.TotalCostUSD
		}
	} else {
		sCost = input.Cost.TotalCostUSD
	}

	var tCost float64
	todayData, err := parseLogs(baseDir, 1, "")
	if err == nil {
		tCost = todayData.Totals().Cost
	}

	var mcpNames []string
	if !noMCP {
		mcpNames = detectMCPs(baseDir, input.TranscriptPath)
	}
	fmt.Print(formatStatusline(sCost, tCost, input, mcpNames))
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
		costByModel[r.Model] += calcCost(r.Model, r.Usage)
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
	// ANSI: erase line + carriage return to overwrite Claude Code's
	// "SessionEnd hook [...] failed: " prefix before our content.
	fmt.Fprintf(os.Stderr, "\x1b[2K\r\n%s", line)
	os.Exit(2)
}
