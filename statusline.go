package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
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
	ctxStr := fmt.Sprintf("%.0f%% ctx", ctxPct)
	switch {
	case ctxPct >= ctxThresholdRed:
		ctxStr = color.RedString(ctxStr)
	case ctxPct >= ctxThresholdYellow:
		ctxStr = color.YellowString(ctxStr)
	default:
		ctxStr = color.GreenString(ctxStr)
	}

	modelStr := color.CyanString(shortModel(input.Model.ID))

	parts := []string{"💸 " + colorCost(sCost, 0) + " session"}
	if tCost > 0 {
		parts = append(parts, "💰 "+colorCost(tCost, 0)+" today")
	}
	parts = append(parts, "💭 "+ctxStr)
	if len(mcpNames) > 0 {
		label := "MCPs"
		if len(mcpNames) == 1 {
			label = "MCP"
		}
		parts = append(parts, fmt.Sprintf("🔌 %d %s (%s)", len(mcpNames), label, strings.Join(mcpNames, ", ")))
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
