package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StatuslineConfig holds user-customizable statusline settings from ~/.goccc.json.
type StatuslineConfig struct {
	Segments       []string                  `json:"segments,omitempty"`
	Separator      string                    `json:"separator,omitempty"`
	Style          string                    `json:"style,omitempty"`
	SegmentOptions map[string]SegmentOptions `json:"segment_options,omitempty"`
	Powerline      *PowerlineConfig          `json:"powerline,omitempty"`
	Pet            *PetConfig                `json:"pet,omitempty"`
}

type SegmentOptions struct {
	Emoji string  `json:"emoji,omitempty"`
	Label *string `json:"label,omitempty"` // pointer so "" can hide the label
	BG    string  `json:"bg,omitempty"`
	FG    string  `json:"fg,omitempty"`
}

var defaultSegments = []string{"session_cost", "today_cost", "ctx", "5h", "model"}

const defaultSeparator = " · "

const stylePowerline = "powerline"

// Powerline needs color escapes, so NO_COLOR falls back to the plain style.
func statuslineStyle(cfg *StatuslineConfig) string {
	if cfg == nil || noColorFlag {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(cfg.Style))
}

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
	if o, ok := opts[name]; ok && o.Label != nil {
		return *o.Label
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
	Style       string
	Pet         PetConfig
}

type segmentRenderer func(ctx *StatuslineContext) string

var segmentRegistry = map[string]segmentRenderer{
	"session_cost": renderSessionCost,
	"today_cost":   renderTodayCost,
	"ctx":          renderCtx,
	"ctx_window":   renderCtxWindow,
	"model":        renderModel,
	"mcp":          renderMCP,
	"branch":       renderBranch,
	"5h":           render5h,
	"7d":           render7d,
	"tokens":       renderTokens,
	"lines":        renderLines,
	"duration":     renderDuration,
	"cwd":          renderCwd,
	"cwd_path":     renderCwdPath,
	"worktree":     renderWorktree,
	"version":      renderVersion,
	"pet":          renderPet,
	"cap":          renderCap,
}

func joinNonEmpty(parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}

func renderSessionCost(ctx *StatuslineContext) string {
	if ctx.SessionCost <= 0 {
		return ""
	}
	emoji := segmentEmoji("session_cost", "💸", ctx.Options)
	label := segmentLabel("session_cost", "session", ctx.Options)
	return joinNonEmpty(emoji, colorCost(ctx.SessionCost, 0), label)
}

func renderTodayCost(ctx *StatuslineContext) string {
	if ctx.TodayCost <= 0 {
		return ""
	}
	emoji := segmentEmoji("today_cost", "💰", ctx.Options)
	label := segmentLabel("today_cost", "today", ctx.Options)
	return joinNonEmpty(emoji, colorCost(ctx.TodayCost, 0), label)
}

func colorCtxPct(s string, pct float64) string {
	switch {
	case pct >= ctxThresholdRed:
		return redString(s)
	case pct >= ctxThresholdYellow:
		return yellowString(s)
	default:
		return s
	}
}

func renderCtx(ctx *StatuslineContext) string {
	ctxPct := ctx.Input.ContextWindow.UsedPercentage
	pctStr := colorCtxPct(fmt.Sprintf("%.0f%%", ctxPct), ctxPct)
	emoji := segmentEmoji("ctx", "💭", ctx.Options)
	label := segmentLabel("ctx", "ctx", ctx.Options)
	return joinNonEmpty(emoji, pctStr, label)
}

func renderCtxWindow(ctx *StatuslineContext) string {
	cw := ctx.Input.ContextWindow
	body := colorCtxPct(fmt.Sprintf("%.1f%%", cw.UsedPercentage), cw.UsedPercentage)
	if cw.ContextWindowSize > 0 {
		body += "/" + fmtTokens(cw.ContextWindowSize)
	}
	return segmentEmoji("ctx_window", "💭", ctx.Options) + " " + body
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
	return joinNonEmpty(emoji, strconv.Itoa(len(ctx.MCPNames)), label, "("+list+")")
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

func statuslineCwd(ctx *StatuslineContext) string {
	if ctx.Input.Cwd != "" {
		return ctx.Input.Cwd
	}
	return ctx.Input.Workspace.CurrentDir
}

// shortCwdPath shows the cwd relative to home, keeping only the last two
// directories of deep paths.
func shortCwdPath(cwd string, homes []string) string {
	isSep := func(r rune) bool { return r == '/' || r == '\\' }
	sep := "/"
	if !strings.Contains(cwd, "/") && strings.Contains(cwd, `\`) {
		sep = `\`
	}
	p := cwd
	for _, home := range homes {
		if home != "" && strings.HasPrefix(cwd, home) && (len(cwd) == len(home) || isSep(rune(cwd[len(home)]))) {
			p = "~" + cwd[len(home):]
			break
		}
	}
	parts := strings.FieldsFunc(p, isSep)
	if len(parts) <= 3 {
		return p
	}
	return "..." + sep + strings.Join(parts[len(parts)-2:], sep)
}

func renderCwdPath(ctx *StatuslineContext) string {
	cwd := statuslineCwd(ctx)
	if cwd == "" {
		return ""
	}
	home, _ := os.UserHomeDir()
	homes := []string{home}
	// Fedora Atomic symlinks /home to /var/home, and cwd arrives resolved.
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		homes = append(homes, resolved)
	}
	emoji := segmentEmoji("cwd_path", "📁", ctx.Options)
	return fmt.Sprintf("%s %s", emoji, shortCwdPath(cwd, homes))
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

// renderCap is a blank block that gives a powerline bar a solid left edge.
func renderCap(ctx *StatuslineContext) string {
	if ctx.Style != stylePowerline {
		return ""
	}
	return powerlineCap
}

type renderedSegment struct {
	name string
	text string
}

// collectSegments renders segments and splits them into lines at "|" markers.
func collectSegments(segments []string, ctx *StatuslineContext) [][]renderedSegment {
	var lines [][]renderedSegment
	var current []renderedSegment

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
		current = append(current, renderedSegment{name: seg, text: s})
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	return lines
}

// assembleStatusline renders segments, joins with separator, and splits at "|" markers.
func assembleStatusline(segments []string, sep string, ctx *StatuslineContext) string {
	lines := collectSegments(segments, ctx)
	rendered := make([]string, len(lines))
	for i, line := range lines {
		texts := make([]string, len(line))
		for j, seg := range line {
			texts[j] = seg.text
		}
		rendered[i] = strings.Join(texts, sep)
	}
	return strings.Join(rendered, "\n")
}
