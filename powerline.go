package main

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	powerlineArrow = "\ue0b0"
	powerlineThin  = "│"
	powerlineCap   = "  "

	alertStyleBlock = "block"
	alertStyleText  = "text"
	alertStyleNone  = "none"
)

var ansiSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var (
	defaultPalette    = []string{"#383a4c", "#404358", "#494c63", "#52556e", "#5a5e7a"}
	defaultFG         = "#f8f8f2"
	defaultWarnColor  = "#f1fa8c"
	defaultAlertColor = "#ff5555"
	defaultAccents    = map[string]accent{"cap": {bg: "#2a2c3a"}, "pet": {bg: "#ff79c6", fg: "#282a36"}}
)

// accent is a segment's own colors, outside the palette cycle.
type accent struct{ bg, fg string }

// PowerlineConfig holds the settings only the powerline style reads.
type PowerlineConfig struct {
	Palette    []string `json:"palette,omitempty"`
	FG         string   `json:"fg,omitempty"`
	AlertStyle string   `json:"alert_style,omitempty"`
	WarnColor  string   `json:"warn_color,omitempty"`
	AlertColor string   `json:"alert_color,omitempty"`
	Arrow      string   `json:"arrow,omitempty"`
	Divider    string   `json:"divider,omitempty"`
}

type powerlineTheme struct {
	palette    []string
	accents    map[string]accent
	fg         string
	alertStyle string
	warnColor  string
	alertColor string
	arrow      string
	divider    string
}

func resolvePowerlineTheme(cfg *PowerlineConfig) powerlineTheme {
	t := powerlineTheme{
		palette:    defaultPalette,
		accents:    defaultAccents,
		fg:         defaultFG,
		warnColor:  defaultWarnColor,
		alertColor: defaultAlertColor,
		alertStyle: alertStyleBlock,
		arrow:      powerlineArrow,
		divider:    powerlineThin,
	}
	if cfg == nil {
		return t
	}
	var palette []string
	for _, c := range cfg.Palette {
		if hex := validHexOr(c, ""); hex != "" {
			palette = append(palette, hex)
		}
	}
	if len(palette) > 0 {
		t.palette = palette
		t.accents = nil
	}
	t.fg = validHexOr(cfg.FG, t.fg)
	switch style := strings.ToLower(strings.TrimSpace(cfg.AlertStyle)); style {
	case alertStyleText, alertStyleNone:
		t.alertStyle = style
	}
	t.warnColor = validHexOr(cfg.WarnColor, t.warnColor)
	t.alertColor = validHexOr(cfg.AlertColor, t.alertColor)
	t.arrow = cmp.Or(cfg.Arrow, t.arrow)
	t.divider = cmp.Or(cfg.Divider, t.divider)
	return t
}

func hexRGB(hex string) (r, g, b int, ok bool) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff), true
}

func ansiTrueColor(layer int, hex string) string {
	r, g, b, _ := hexRGB(hex)
	return fmt.Sprintf("\033[%d;2;%d;%d;%dm", layer, r, g, b)
}

func bgCode(hex string) string { return ansiTrueColor(48, hex) }
func fgCode(hex string) string { return ansiTrueColor(38, hex) }

// validHexOr normalizes to "#rrggbb", so "ABCDEF" and "#abcdef" compare equal.
func validHexOr(hex, fallback string) string {
	if _, _, _, ok := hexRGB(hex); ok {
		return "#" + strings.ToLower(strings.TrimPrefix(hex, "#"))
	}
	return fallback
}

// relativeLuminance is the WCAG luminance of a "#rrggbb" color, 0 (black) to 1 (white).
func relativeLuminance(hex string) float64 {
	r, g, b, _ := hexRGB(hex)
	lin := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// textOn picks near-black or near-white text, whichever reads better on bg.
func textOn(bg string) string {
	if relativeLuminance(bg) > 0.18 {
		return "#1c1c1c"
	}
	return "#f5f5f5"
}

func segmentStyle(bg, fg string) string {
	return ansiReset + bgCode(bg) + fgCode(fg)
}

type styledSegment struct {
	text string
	bg   string
	fg   string
}

// alertLevel reports the plain style's threshold color in text: ansiRed, ansiYellow or "".
func alertLevel(text string) string {
	switch {
	case strings.Contains(text, ansiRed):
		return ansiRed
	case strings.Contains(text, ansiYellow):
		return ansiYellow
	}
	return ""
}

// recolorValues maps the plain style's threshold colors onto the block: as warn/alert
// text in text mode, or dropped in block mode, where the whole block already took
// the color, and in none mode. Any other escape code is dropped.
func recolorValues(text, bg, fg string, theme powerlineTheme) string {
	return ansiSGR.ReplaceAllStringFunc(text, func(code string) string {
		switch {
		case code == ansiReset:
			return segmentStyle(bg, fg)
		case theme.alertStyle != alertStyleText:
			return ""
		case code == ansiRed:
			return ansiReset + bgCode(bg) + fgCode(theme.alertColor)
		case code == ansiYellow:
			return ansiReset + bgCode(bg) + fgCode(theme.warnColor)
		}
		return ""
	})
}

// styleSegments gives each segment its own bg, or else the next palette color.
// In block mode a warn/alert value recolors its whole block, keeping its palette
// slot so the colors after it don't shift.
func styleSegments(segs []renderedSegment, opts map[string]SegmentOptions, theme powerlineTheme) []styledSegment {
	styled := make([]styledSegment, len(segs))
	next := 0
	for i, seg := range segs {
		o := opts[seg.name]
		own := theme.accents[seg.name]
		bg := validHexOr(o.BG, own.bg)
		if bg == "" {
			bg = theme.palette[next%len(theme.palette)]
			next++
		}
		fg := validHexOr(o.FG, cmp.Or(own.fg, theme.fg))
		if level := alertLevel(seg.text); level != "" && theme.alertStyle == alertStyleBlock {
			bg = theme.warnColor
			if level == ansiRed {
				bg = theme.alertColor
			}
			fg = textOn(bg)
		}
		styled[i] = styledSegment{text: recolorValues(seg.text, bg, fg, theme), bg: bg, fg: fg}
	}
	return styled
}

func powerlineLine(segs []renderedSegment, opts map[string]SegmentOptions, theme powerlineTheme) string {
	styled := styleSegments(segs, opts, theme)
	var b strings.Builder
	for i, seg := range styled {
		text := seg.text
		if strings.TrimSpace(text) != "" {
			text = " " + text + " "
		}
		b.WriteString(segmentStyle(seg.bg, seg.fg) + text)

		if i == len(styled)-1 {
			b.WriteString(ansiReset + fgCode(seg.bg) + theme.arrow + ansiReset)
			break
		}
		if next := styled[i+1].bg; next == seg.bg {
			b.WriteString(theme.divider)
		} else {
			b.WriteString(ansiReset + fgCode(seg.bg) + bgCode(next) + theme.arrow)
		}
	}
	return b.String()
}

func assemblePowerline(segments []string, ctx *StatuslineContext, theme powerlineTheme) string {
	var rendered []string
	for _, line := range collectSegments(segments, ctx) {
		// A cap is decoration; alone it would leave a stray block on its own row.
		if !slices.ContainsFunc(line, func(s renderedSegment) bool { return s.name != "cap" }) {
			continue
		}
		rendered = append(rendered, powerlineLine(line, ctx.Options, theme))
	}
	return strings.Join(rendered, "\n")
}
