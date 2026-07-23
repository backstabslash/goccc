package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// StatuslineConfig holds user-customizable statusline settings from ~/.goccc.json.
type StatuslineConfig struct {
	Segments       []string                  `json:"segments,omitempty"`
	Separator      string                    `json:"separator,omitempty"`
	SegmentOptions map[string]SegmentOptions `json:"segment_options,omitempty"`
}

type SegmentOptions struct {
	Emoji string `json:"emoji,omitempty"`
	Label string `json:"label,omitempty"`
}

var defaultSegments = []string{"session_cost", "today_cost", "ctx", "5h", "model"}

const defaultSeparator = " · "

func resolveStatuslineConfig(cfg *StatuslineConfig) ([]string, string, map[string]SegmentOptions) {
	segments := defaultSegments
	sep := defaultSeparator
	var opts map[string]SegmentOptions

	if cfg != nil {
		if len(cfg.Segments) > 0 {
			segments = cfg.Segments
		}
		if cfg.Separator != "" {
			sep = cfg.Separator
		}
		opts = cfg.SegmentOptions
	}
	return append([]string(nil), segments...), sep, opts
}

func segmentEmoji(name, fallback string, opts map[string]SegmentOptions) string {
	if o, ok := opts[name]; ok && o.Emoji != "" {
		return o.Emoji
	}
	return fallback
}

func segmentLabel(name, fallback string, opts map[string]SegmentOptions) string {
	if o, ok := opts[name]; ok && o.Label != "" {
		return o.Label
	}
	return fallback
}

// StatuslineContext holds all computed data needed by segment renderers.
type StatuslineContext struct {
	SessionCost float64
	TodayCost   float64
	Input       *StatuslineInput
	MCPNames    []string
	Branch      string
	Options     map[string]SegmentOptions
}

type segmentRenderer func(ctx *StatuslineContext) string

var segmentRegistry = map[string]segmentRenderer{
	"session_cost": renderSessionCost,
	"today_cost":   renderTodayCost,
	"ctx":          renderCtx,
	"model":        renderModel,
	"mcp":          renderMCP,
	"branch":       renderBranch,
	"5h":           render5h,
	"7d":           render7d,
	"tokens":       renderTokens,
	"lines":        renderLines,
	"duration":     renderDuration,
	"cwd":          renderCwd,
	"worktree":     renderWorktree,
	"version":      renderVersion,
}

func renderSessionCost(ctx *StatuslineContext) string {
	if ctx.SessionCost <= 0 {
		return ""
	}
	emoji := segmentEmoji("session_cost", "💸", ctx.Options)
	label := segmentLabel("session_cost", "session", ctx.Options)
	return fmt.Sprintf("%s %s %s", emoji, colorCost(ctx.SessionCost, 0), label)
}

func renderTodayCost(ctx *StatuslineContext) string {
	if ctx.TodayCost <= 0 {
		return ""
	}
	emoji := segmentEmoji("today_cost", "💰", ctx.Options)
	label := segmentLabel("today_cost", "today", ctx.Options)
	return fmt.Sprintf("%s %s %s", emoji, colorCost(ctx.TodayCost, 0), label)
}

func renderCtx(ctx *StatuslineContext) string {
	ctxPct := ctx.Input.ContextWindow.UsedPercentage
	pctStr := fmt.Sprintf("%.0f%%", ctxPct)
	switch {
	case ctxPct >= ctxThresholdRed:
		pctStr = redString(pctStr)
	case ctxPct >= ctxThresholdYellow:
		pctStr = yellowString(pctStr)
	}
	emoji := segmentEmoji("ctx", "💭", ctx.Options)
	label := segmentLabel("ctx", "ctx", ctx.Options)
	return fmt.Sprintf("%s %s %s", emoji, pctStr, label)
}

func renderModel(ctx *StatuslineContext) string {
	emoji := segmentEmoji("model", "🤖", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, shortModel(ctx.Input.Model.ID))
}

func renderMCP(ctx *StatuslineContext) string {
	if len(ctx.MCPNames) == 0 {
		return ""
	}
	emoji := segmentEmoji("mcp", "🔌", ctx.Options)
	label := "MCPs"
	if len(ctx.MCPNames) == 1 {
		label = "MCP"
	}
	label = segmentLabel("mcp", label, ctx.Options)
	const maxShown = 3
	shown := ctx.MCPNames
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}
	list := strings.Join(shown, ", ")
	if len(ctx.MCPNames) > maxShown {
		list += ", ..."
	}
	return fmt.Sprintf("%s %d %s (%s)", emoji, len(ctx.MCPNames), label, list)
}

const (
	sevenDayWindow          = 7 * 24 * time.Hour
	sevenDayThresholdRed    = 25.0
	sevenDayThresholdYellow = 50.0
	sevenDayLowBattery      = 25.0
)

// formatRateLimitBody returns the "XX% (X.X/Xh)" part without the battery emoji.
func formatRateLimitBody(usedPct float64, resetsAt int64, now time.Time, window time.Duration) string {
	remainPct := 100 - usedPct
	if remainPct < 0 {
		remainPct = 0
	}

	resetTime := time.Unix(resetsAt, 0)
	remaining := resetTime.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	elapsed := window - remaining
	if elapsed > window {
		elapsed = window
	}

	windowHours := window.Hours()
	hours := elapsed.Hours()

	windowLabel := fmt.Sprintf("%.0fh", windowHours)
	if window == sevenDayWindow {
		windowLabel = "7d"
	}

	var elapsedStr string
	if hours == float64(int(hours)) {
		elapsedStr = fmt.Sprintf("%d/%s", int(hours), windowLabel)
	} else {
		elapsedStr = fmt.Sprintf("%.1f/%s", hours, windowLabel)
	}

	pctStr := fmt.Sprintf("%.0f%%", remainPct)

	redThreshold := fiveHourThresholdRed
	yellowThreshold := fiveHourThresholdYellow
	if window == sevenDayWindow {
		redThreshold = sevenDayThresholdRed
		yellowThreshold = sevenDayThresholdYellow
	}

	switch {
	case remainPct <= redThreshold:
		pctStr = redString(pctStr)
	case remainPct <= yellowThreshold:
		pctStr = yellowString(pctStr)
	}

	return fmt.Sprintf("%s (%s)", pctStr, elapsedStr)
}

func formatRateLimitUsage(usedPct float64, resetsAt int64, now time.Time, window time.Duration, lowThreshold float64) string {
	remainPct := 100 - usedPct
	if remainPct < 0 {
		remainPct = 0
	}
	emoji := "🔋"
	if remainPct <= lowThreshold {
		emoji = "🪫"
	}
	return fmt.Sprintf("%s %s", emoji, formatRateLimitBody(usedPct, resetsAt, now, window))
}

func renderRateLimit(name string, rl *rateLimitWindow, window time.Duration, lowThreshold float64, ctx *StatuslineContext) string {
	if rl == nil {
		return ""
	}
	customEmoji := segmentEmoji(name, "", ctx.Options)
	if customEmoji != "" {
		// Custom emoji replaces the dynamic battery emoji — render without it
		pctAndTime := formatRateLimitBody(rl.UsedPercentage, rl.ResetsAt, time.Now(), window)
		return fmt.Sprintf("%s %s", customEmoji, pctAndTime)
	}
	return formatRateLimitUsage(rl.UsedPercentage, rl.ResetsAt, time.Now(), window, lowThreshold)
}

func render5h(ctx *StatuslineContext) string {
	return renderRateLimit("5h", ctx.Input.RateLimits.FiveHour, fiveHourWindow, fiveHourLowBattery, ctx)
}

func render7d(ctx *StatuslineContext) string {
	return renderRateLimit("7d", ctx.Input.RateLimits.SevenDay, sevenDayWindow, sevenDayLowBattery, ctx)
}

func renderTokens(ctx *StatuslineContext) string {
	in := ctx.Input.ContextWindow.TotalInputTokens
	out := ctx.Input.ContextWindow.TotalOutputTokens
	if in == 0 && out == 0 {
		return ""
	}
	emoji := segmentEmoji("tokens", "📊", ctx.Options)
	return fmt.Sprintf("%s %s in / %s out", emoji, fmtTokens(in), fmtTokens(out))
}

func renderLines(ctx *StatuslineContext) string {
	added := ctx.Input.Cost.TotalLinesAdded
	removed := ctx.Input.Cost.TotalLinesRemoved
	if added == 0 && removed == 0 {
		return ""
	}
	emoji := segmentEmoji("lines", "📝", ctx.Options)
	return fmt.Sprintf("%s +%d -%d", emoji, added, removed)
}

func renderDuration(ctx *StatuslineContext) string {
	ms := ctx.Input.Cost.TotalDurationMs
	if ms == 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	emoji := segmentEmoji("duration", "⏱️", ctx.Options)
	label := fmtSessionDuration(d)
	return fmt.Sprintf("%s %s", emoji, label)
}

func renderBranch(ctx *StatuslineContext) string {
	if ctx.Branch == "" {
		return ""
	}
	emoji := segmentEmoji("branch", "🌿", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, ctx.Branch)
}

// statuslineCwd returns the session's working directory, preferring the explicit
// cwd and falling back to the workspace directory.
func statuslineCwd(ctx *StatuslineContext) string {
	if ctx.Input.Cwd != "" {
		return ctx.Input.Cwd
	}
	return ctx.Input.Workspace.CurrentDir
}

func renderCwd(ctx *StatuslineContext) string {
	cwd := statuslineCwd(ctx)
	if cwd == "" {
		return ""
	}
	emoji := segmentEmoji("cwd", "📁", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, filepath.Base(cwd))
}

// renderWorktree resolves the worktree from the filesystem on demand — cheap,
// and assembleStatusline only reaches renderers the user actually configured.
func renderWorktree(ctx *StatuslineContext) string {
	name := detectWorktree(statuslineCwd(ctx))
	if name == "" {
		return ""
	}
	emoji := segmentEmoji("worktree", "🌳", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, name)
}

func renderVersion(ctx *StatuslineContext) string {
	v := ctx.Input.Version
	if v == "" {
		return ""
	}
	emoji := segmentEmoji("version", "🏷️", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, v)
}

// assembleStatusline renders segments, joins with separator, and splits at "|" markers.
func assembleStatusline(segments []string, sep string, ctx *StatuslineContext) string {
	var lines [][]string
	var current []string

	for _, seg := range segments {
		if seg == "|" {
			if len(current) > 0 {
				lines = append(lines, current)
				current = nil
			}
			continue
		}
		render, ok := segmentRegistry[seg]
		if !ok {
			continue
		}
		s := render(ctx)
		if s == "" {
			continue
		}
		current = append(current, s)
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}

	rendered := make([]string, len(lines))
	for i, line := range lines {
		rendered[i] = strings.Join(line, sep)
	}
	return strings.Join(rendered, "\n")
}
