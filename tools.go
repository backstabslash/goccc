package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runToolUsage(baseDir string, days int, project string, topN int, jsonOutput bool) {
	start := time.Now()
	toolData, err := parseTools(baseDir, days, project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	toolData.Duration = time.Since(start)
	if jsonOutput {
		printToolsJSON(toolData, topN)
	} else {
		printToolUsage(toolData, topN)
	}
}

type ToolResult struct {
	ToolCounts      map[string]int
	ToolErrors      map[string]int
	SkillCounts     map[string]int
	AgentCounts     map[string]int
	AgentTotalTime  map[string]time.Duration
	ToolProjects    map[string]map[string]bool
	SkillProjects   map[string]map[string]bool
	AgentProjects   map[string]map[string]bool
	ToolSessions    map[string]bool
	AgentSessions   map[string]bool
	SkillSessions   map[string]bool
	AvailableSkills []string
	TotalFiles      int
	Duration        time.Duration
}

type toolCollector struct {
	seenToolUse    map[string]bool
	toolCounts     map[string]int
	toolErrors     map[string]int
	skillCounts    map[string]int
	agentCounts    map[string]int
	toolProjects   map[string]map[string]bool
	skillProjects  map[string]map[string]bool
	agentProjects  map[string]map[string]bool
	toolSessions   map[string]bool
	agentSessions  map[string]bool
	skillSessions  map[string]bool
	latestSkills   []string
	latestSkillTS  time.Time
	toolUseNames   map[string]string    // tool_use_id → tool_name
	agentUseTS     map[string]time.Time // tool_use_id → timestamp (Agent calls)
	agentUseType   map[string]string    // tool_use_id → agent type
	agentTotalTime map[string]time.Duration
}

func newToolResult() *ToolResult {
	return &ToolResult{
		ToolCounts:     make(map[string]int),
		ToolErrors:     make(map[string]int),
		SkillCounts:    make(map[string]int),
		AgentCounts:    make(map[string]int),
		AgentTotalTime: make(map[string]time.Duration),
		ToolProjects:   make(map[string]map[string]bool),
		SkillProjects:  make(map[string]map[string]bool),
		AgentProjects:  make(map[string]map[string]bool),
		ToolSessions:   make(map[string]bool),
		AgentSessions:  make(map[string]bool),
		SkillSessions:  make(map[string]bool),
	}
}

func newToolCollector() *toolCollector {
	return &toolCollector{
		seenToolUse:    make(map[string]bool),
		toolCounts:     make(map[string]int),
		toolErrors:     make(map[string]int),
		skillCounts:    make(map[string]int),
		agentCounts:    make(map[string]int),
		toolProjects:   make(map[string]map[string]bool),
		skillProjects:  make(map[string]map[string]bool),
		agentProjects:  make(map[string]map[string]bool),
		toolSessions:   make(map[string]bool),
		agentSessions:  make(map[string]bool),
		skillSessions:  make(map[string]bool),
		toolUseNames:   make(map[string]string),
		agentUseTS:     make(map[string]time.Time),
		agentUseType:   make(map[string]string),
		agentTotalTime: make(map[string]time.Duration),
	}
}

type toolRecord struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Attachment struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"attachment"`
}

type toolContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type skillInput struct {
	Skill string `json:"skill"`
}

type agentInput struct {
	SubagentType string `json:"subagent_type"`
}

func addToolProject(m map[string]map[string]bool, key, project string) {
	if m[key] == nil {
		m[key] = make(map[string]bool)
	}
	m[key][project] = true
}

func parseSkillListing(content string) []string {
	var skills []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		if idx := strings.Index(line, ": "); idx > 0 {
			line = line[:idx]
		}
		if line = strings.TrimSpace(line); line != "" {
			skills = append(skills, line)
		}
	}
	return skills
}

func (col *toolCollector) processAssistant(rec *toolRecord, projectSlug, sessionID string) error {
	if rec.Message.Content == nil {
		return nil
	}
	var blocks []toolContentBlock
	if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
		return err
	}

	var recTS time.Time
	if rec.Timestamp != "" {
		recTS, _ = time.Parse(time.RFC3339, rec.Timestamp)
	}

	for _, block := range blocks {
		if block.Type != "tool_use" || block.ID == "" {
			continue
		}
		if col.seenToolUse[block.ID] {
			continue
		}
		col.seenToolUse[block.ID] = true
		col.toolUseNames[block.ID] = block.Name

		col.toolCounts[block.Name]++
		addToolProject(col.toolProjects, block.Name, projectSlug)
		col.toolSessions[sessionID] = true

		if block.Name == "Skill" && block.Input != nil {
			var si skillInput
			if err := json.Unmarshal(block.Input, &si); err == nil && si.Skill != "" {
				col.skillCounts[si.Skill]++
				addToolProject(col.skillProjects, si.Skill, projectSlug)
				col.skillSessions[sessionID] = true
			}
		}

		if block.Name == "Agent" {
			col.trackAgent(block, recTS, projectSlug, sessionID)
		}
	}
	return nil
}

func (col *toolCollector) trackAgent(block toolContentBlock, recTS time.Time, projectSlug, sessionID string) {
	agentType := "general-purpose"
	if block.Input != nil {
		var ai agentInput
		if err := json.Unmarshal(block.Input, &ai); err == nil && ai.SubagentType != "" {
			agentType = ai.SubagentType
		}
	}
	col.agentCounts[agentType]++
	addToolProject(col.agentProjects, agentType, projectSlug)
	col.agentSessions[sessionID] = true
	if !recTS.IsZero() {
		col.agentUseTS[block.ID] = recTS
		col.agentUseType[block.ID] = agentType
	}
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

func (col *toolCollector) processUser(rec *toolRecord) {
	if rec.Message.Content == nil {
		return
	}
	var blocks []toolResultBlock
	if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
		return
	}

	var recTS time.Time
	if rec.Timestamp != "" {
		recTS, _ = time.Parse(time.RFC3339, rec.Timestamp)
	}

	for _, block := range blocks {
		if block.Type != "tool_result" || block.ToolUseID == "" {
			continue
		}
		if block.IsError {
			if name, ok := col.toolUseNames[block.ToolUseID]; ok {
				col.toolErrors[name]++
			}
		}
		if !recTS.IsZero() {
			if startTS, ok := col.agentUseTS[block.ToolUseID]; ok {
				agentType := col.agentUseType[block.ToolUseID]
				col.agentTotalTime[agentType] += recTS.Sub(startTS)
			}
		}
	}
}

func (col *toolCollector) processSkillListing(rec *toolRecord) {
	if rec.Attachment.Type != "skill_listing" || rec.Timestamp == "" {
		return
	}
	parsed, err := time.Parse(time.RFC3339, rec.Timestamp)
	if err == nil && parsed.After(col.latestSkillTS) {
		col.latestSkillTS = parsed
		col.latestSkills = parseSkillListing(rec.Attachment.Content)
	}
}

func isToolLine(line []byte) bool {
	switch {
	case hasJSONType(line, "assistant"):
		return bytes.Contains(line, []byte(`"tool_use"`))
	case hasJSONType(line, "user"):
		return bytes.Contains(line, []byte(`"tool_result"`))
	case hasJSONType(line, "attachment"):
		return bytes.Contains(line, []byte(`"skill_listing"`))
	}
	return false
}

func beforeCutoff(ts string, cutoff time.Time) (skip bool, err error) {
	if ts == "" {
		return false, nil
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false, err
	}
	return parsed.Before(cutoff), nil
}

func parseToolFile(path string, cutoff time.Time, hasCutoff bool, projectSlug, sessionID string, col *toolCollector) (parseErrs int, fileErr error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 100*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || !isToolLine(line) {
			continue
		}

		var rec toolRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			parseErrs++
			continue
		}

		if hasCutoff {
			skip, err := beforeCutoff(rec.Timestamp, cutoff)
			if err != nil {
				parseErrs++
				continue
			}
			if skip {
				continue
			}
		}

		switch rec.Type {
		case "assistant":
			if err := col.processAssistant(&rec, projectSlug, sessionID); err != nil {
				parseErrs++
			}
		case "user":
			col.processUser(&rec)
		case "attachment":
			col.processSkillListing(&rec)
		}
	}

	if err := scanner.Err(); err != nil {
		return parseErrs, err
	}
	return parseErrs, nil
}

func parseTools(baseDir string, days int, projectFilter string) (*ToolResult, error) {
	cutoff, hasCutoff := localMidnightCutoff(days)

	projectsDir := filepath.Join(baseDir, "projects")
	if info, err := os.Stat(projectsDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("no projects directory found at %s", projectsDir)
	}

	matchedSlug := resolveProjectSlug(projectsDir, projectFilter)
	if projectFilter != "" && matchedSlug == "" {
		return newToolResult(), nil
	}

	col := newToolCollector()
	w := &dirWalker{
		projectsDir: projectsDir,
		matchedSlug: matchedSlug,
		hasCutoff:   hasCutoff,
		cutoff:      cutoff,
		onFile: func(path, projectSlug, sessionID string) {
			if _, fErr := parseToolFile(path, cutoff, hasCutoff, projectSlug, sessionID, col); fErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not read %s: %v\n", path, fErr)
			}
		},
	}

	if err := filepath.WalkDir(projectsDir, w.walk); err != nil {
		return nil, err
	}

	return &ToolResult{
		ToolCounts:      col.toolCounts,
		ToolErrors:      col.toolErrors,
		SkillCounts:     col.skillCounts,
		AgentCounts:     col.agentCounts,
		AgentTotalTime:  col.agentTotalTime,
		ToolProjects:    col.toolProjects,
		SkillProjects:   col.skillProjects,
		AgentProjects:   col.agentProjects,
		ToolSessions:    col.toolSessions,
		AgentSessions:   col.agentSessions,
		SkillSessions:   col.skillSessions,
		AvailableSkills: col.latestSkills,
		TotalFiles:      w.totalFiles,
	}, nil
}
