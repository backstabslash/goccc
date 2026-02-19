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
		_, _, _ = parseFile(path, time.Time{}, false, "", deduped)
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

func formatStatusline(sCost, tCost float64, input *StatuslineInput) string {
	costFn := func(c float64) string {
		s := fmtCost(c)
		switch {
		case c >= 10.0:
			return color.RedString(s)
		case c >= 1.0:
			return color.YellowString(s)
		default:
			return color.GreenString(s)
		}
	}

	ctxPct := input.ContextWindow.UsedPercentage
	ctxStr := fmt.Sprintf("%.0f%% ctx", ctxPct)
	switch {
	case ctxPct >= 90:
		ctxStr = color.RedString(ctxStr)
	case ctxPct >= 70:
		ctxStr = color.YellowString(ctxStr)
	default:
		ctxStr = color.GreenString(ctxStr)
	}

	modelStr := color.CyanString(shortModel(input.Model.ID))

	parts := []string{"💸 " + costFn(sCost) + " session"}
	if tCost > sCost {
		parts = append(parts, "💰 "+costFn(tCost)+" today")
	}
	parts = append(parts, "💭 "+ctxStr, "🤖 "+modelStr)

	return strings.Join(parts, " | ")
}

func runStatusline(baseDir string) {
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
		for _, b := range todayData.ModelUsage {
			tCost += b.Cost
		}
	}

	fmt.Print(formatStatusline(sCost, tCost, input))
}
