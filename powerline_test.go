package main

import (
	"strings"
	"testing"
)

func TestHexRGB(t *testing.T) {
	r, g, b, ok := hexRGB("#d9894A")
	if !ok || r != 0xd9 || g != 0x89 || b != 0x4a {
		t.Errorf("got %d %d %d %v", r, g, b, ok)
	}
	for _, bad := range []string{"", "#fff", "#gggggg", "d9894a4"} {
		if _, _, _, ok := hexRGB(bad); ok {
			t.Errorf("%q: expected invalid", bad)
		}
	}
}

func TestResolvePowerlineTheme(t *testing.T) {
	theme := resolvePowerlineTheme(&PowerlineConfig{Palette: []string{"bad", "ABCDEF"}, WarnColor: "nope", AlertStyle: " Text "})
	if len(theme.palette) != 1 || theme.palette[0] != "#abcdef" {
		t.Errorf("palette = %v, want only the valid color", theme.palette)
	}
	if theme.warnColor != defaultWarnColor {
		t.Errorf("warnColor = %q, want default on invalid", theme.warnColor)
	}
	if theme.accents != nil {
		t.Error("a custom palette should drop the default palette's cap/pet colors")
	}
	if theme.alertStyle != alertStyleText {
		t.Error("alert_style should match case- and space-insensitively")
	}
	if def := resolvePowerlineTheme(nil); def.alertStyle != alertStyleBlock || def.accents["cap"].bg == "" || def.fg != defaultFG {
		t.Errorf("defaults: block alerts, a cap color and a real text color, got %+v", def)
	}
	if theme.fg != defaultFG {
		t.Errorf("fg = %q, want the default when unset", theme.fg)
	}
}

func TestTextOn(t *testing.T) {
	if textOn(defaultAlertColor) != "#1c1c1c" || textOn(defaultWarnColor) != "#1c1c1c" {
		t.Error("default warn/alert blocks need dark text")
	}
	if textOn("#2f4227") != "#f5f5f5" {
		t.Error("a dark block needs light text")
	}
}

func TestStyleSegments_TextAlerts(t *testing.T) {
	theme := powerlineTheme{
		palette: []string{"#000001", "#000002"}, fg: "#eeeeee", alertStyle: alertStyleText, warnColor: "#00000a", alertColor: "#00000b",
		accents: map[string]accent{"pet": {bg: "#00000e", fg: "#00000f"}},
	}
	opts := map[string]SegmentOptions{"model": {BG: "#00000c", FG: "#ffffff"}, "cap": {BG: "#00000d"}}
	segs := []renderedSegment{
		{"cap", powerlineCap},
		{"today_cost", "$1"},
		{"model", "m"},
		{"session_cost", ansiYellow + "$2" + ansiReset},
		{"ctx", ansiRed + "90%" + ansiReset},
		{"cwd", "d"},
		{"pet", "p"},
	}
	warn := ansiReset + bgCode("#000002") + fgCode("#00000a") + "$2" + segmentStyle("#000002", "#eeeeee")
	alert := ansiReset + bgCode("#000001") + fgCode("#00000b") + "90%" + segmentStyle("#000001", "#eeeeee")
	want := []styledSegment{
		{powerlineCap, "#00000d", "#eeeeee"},
		{"$1", "#000001", "#eeeeee"},
		{"m", "#00000c", "#ffffff"},
		{warn, "#000002", "#eeeeee"},
		{alert, "#000001", "#eeeeee"},
		{"d", "#000002", "#eeeeee"},
		{"p", "#00000e", "#00000f"},
	}
	got := styleSegments(segs, opts, theme)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestStyleSegments_BlockAlerts(t *testing.T) {
	theme := powerlineTheme{palette: []string{"#000001", "#000002"}, alertStyle: alertStyleBlock, warnColor: "#f2c14e", alertColor: "#ff7a6b"}
	segs := []renderedSegment{
		{"ctx", ansiRed + "90%" + ansiReset + " ctx"},
		{"session_cost", ansiYellow + "$2" + ansiReset},
		{"cwd", "d"},
	}
	got := styleSegments(segs, nil, theme)
	if got[0].bg != "#ff7a6b" || got[0].fg != "#1c1c1c" {
		t.Errorf("alert block = %+v, want alert bg with dark text", got[0])
	}
	if got[1].bg != "#f2c14e" {
		t.Errorf("warn block = %+v, want warn bg", got[1])
	}
	if want := "90%" + segmentStyle("#ff7a6b", "#1c1c1c") + " ctx"; got[0].text != want {
		t.Errorf("alert text = %q, want %q", got[0].text, want)
	}
	// Alert blocks keep their palette slots, so later colors don't shift as values cross thresholds.
	if got[2].bg != "#000001" {
		t.Errorf("segment after alerts = %q, want the third palette slot", got[2].bg)
	}
}

func TestStyleSegments_NoAlerts(t *testing.T) {
	theme := resolvePowerlineTheme(&PowerlineConfig{Palette: []string{"#000001"}, AlertStyle: "None"})
	got := styleSegments([]renderedSegment{{"ctx", ansiRed + "90%" + ansiReset + " ctx"}}, nil, theme)
	want := styledSegment{"90%" + segmentStyle("#000001", defaultFG) + " ctx", "#000001", defaultFG}
	if got[0] != want {
		t.Errorf("got %+v, want the palette color with the threshold color dropped: %+v", got[0], want)
	}
}

func TestAssemblePowerline_DropsLoneCap(t *testing.T) {
	ctx := &StatuslineContext{Input: makeTestInput(), Style: stylePowerline}
	got := assemblePowerline([]string{"cap", "mcp", "|", "cap", "model"}, ctx, resolvePowerlineTheme(nil))
	if strings.Count(got, "\n") != 0 || !strings.Contains(got, "Opus 4.6") {
		t.Errorf("a row with only a cap should be dropped: %q", got)
	}
}

func TestResolvePowerlineTheme_Glyphs(t *testing.T) {
	theme := resolvePowerlineTheme(&PowerlineConfig{Arrow: "\ue0b4", Divider: "\ue0b5", FG: "#FFFFFF"})
	segs := []renderedSegment{{"today_cost", "a"}, {"session_cost", "b"}, {"model", "c"}}
	opts := map[string]SegmentOptions{"today_cost": {BG: "#010101"}, "session_cost": {BG: "010101"}}
	got := powerlineLine(segs, opts, theme)
	if !strings.Contains(got, "\ue0b5") || strings.Count(got, "\ue0b4") != 2 {
		t.Errorf("custom glyphs not used, or same color written two ways didn't merge: %q", got)
	}
	if !strings.Contains(got, fgCode("#ffffff")+" a ") {
		t.Errorf("global fg not applied: %q", got)
	}
}

func TestPowerlineLine_SameBGUsesThinSeparator(t *testing.T) {
	opts := map[string]SegmentOptions{
		"today_cost":   {BG: "#2f4a2a"},
		"session_cost": {BG: "#2F4A2A"},
	}
	segs := []renderedSegment{{"today_cost", "a"}, {"session_cost", "b"}}
	got := powerlineLine(segs, opts, resolvePowerlineTheme(nil))
	if !strings.Contains(got, " a "+powerlineThin) {
		t.Errorf("expected thin separator between same-bg segments: %q", got)
	}
	if strings.Count(got, powerlineArrow) != 1 {
		t.Errorf("expected only the closing arrow: %q", got)
	}
}

func TestPowerlineLine_ArrowTransition(t *testing.T) {
	opts := map[string]SegmentOptions{
		"ctx":   {BG: "#010203"},
		"model": {BG: "#040506"},
	}
	got := powerlineLine([]renderedSegment{{"ctx", "a"}, {"model", "b"}}, opts, resolvePowerlineTheme(nil))
	want := fgCode("#010203") + bgCode("#040506") + powerlineArrow
	if !strings.Contains(got, want) {
		t.Errorf("missing transition %q in %q", want, got)
	}
	if !strings.HasSuffix(got, ansiReset+fgCode("#040506")+powerlineArrow+ansiReset) {
		t.Errorf("missing closing arrow: %q", got)
	}
}

func TestPowerlineLine_TextAlertsBecomeTextColor(t *testing.T) {
	opts := map[string]SegmentOptions{"ctx": {BG: "#010203"}}
	theme := resolvePowerlineTheme(&PowerlineConfig{AlertStyle: alertStyleText})
	got := powerlineLine([]renderedSegment{{"ctx", ansiRed + "80%" + ansiReset + " ctx"}}, opts, theme)
	if strings.Contains(got, ansiRed) {
		t.Errorf("plain red should be replaced: %q", got)
	}
	if !strings.Contains(got, fgCode(defaultAlertColor)+"80%"+segmentStyle("#010203", defaultFG)+" ctx ") {
		t.Errorf("alert value should switch to the alert text color: %q", got)
	}
}

func TestSegmentStyle(t *testing.T) {
	if got := segmentStyle("#010203", "#ffffff"); got != ansiReset+bgCode("#010203")+fgCode("#ffffff") {
		t.Errorf("explicit fg: %q", got)
	}
}

func TestFormatStatusline_PowerlineRespectsNoColor(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	cfg := &StatuslineConfig{Segments: []string{"cap", "model", "version"}, Style: stylePowerline}
	got := formatStatuslineWithConfig(0, 0, makeTestInput(), nil, "", cfg)
	if got != "🤖 Opus 4.6"+defaultSeparator+"🏷️ 2.1.83" {
		t.Errorf("expected plain output without cap under NO_COLOR: %q", got)
	}
}

func TestPowerlineLine_CapIsTwoBlankCells(t *testing.T) {
	opts := map[string]SegmentOptions{"cap": {BG: "#010203"}, "model": {BG: "#040506"}}
	got := powerlineLine([]renderedSegment{{"cap", powerlineCap}, {"model", "m"}}, opts, resolvePowerlineTheme(nil))
	want := segmentStyle("#010203", defaultFG) + "  " + ansiReset + fgCode("#010203")
	if !strings.HasPrefix(got, want) {
		t.Errorf("got %q, want prefix %q", got, want)
	}
}

func TestRenderCtxWindow(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	input := makeTestInput()
	input.ContextWindow.UsedPercentage = 10.94
	input.ContextWindow.ContextWindowSize = 1_000_000
	got := renderCtxWindow(&StatuslineContext{Input: input})
	if got != "💭 10.9%/1.0M" {
		t.Errorf("got %q", got)
	}

	input.ContextWindow.ContextWindowSize = 0
	input.ContextWindow.UsedPercentage = 60
	if got := renderCtxWindow(&StatuslineContext{Input: input}); got != "💭 60.0%" {
		t.Errorf("without size: got %q", got)
	}
}

func TestRenderSessionCost_EmptyLabelHides(t *testing.T) {
	noColorFlag = true
	defer func() { noColorFlag = false }()

	ctx := &StatuslineContext{
		SessionCost: 1.50,
		Input:       makeTestInput(),
		Options:     map[string]SegmentOptions{"session_cost": {Label: new("")}},
	}
	if got := renderSessionCost(ctx); got != "💸 $1.50" {
		t.Errorf("got %q", got)
	}
}

func TestShortCwdPath_ResolvedHome(t *testing.T) {
	if got := shortCwdPath("/var/home/u/.claude", []string{"/home/u", "/var/home/u"}); got != "~/.claude" {
		t.Errorf("got %q", got)
	}
}

func TestShortCwdPath(t *testing.T) {
	tests := []struct{ cwd, want string }{
		{"/home/u", "~"},
		{"/home/u/repos/app", "~/repos/app"},
		{"/home/u/a/b/c", ".../b/c"},
		{"/home/user2/x", "/home/user2/x"},
		{"/opt/a/b/c", ".../b/c"},
		{"/home/u2", "/home/u2"},
	}
	for _, tt := range tests {
		if got := shortCwdPath(tt.cwd, []string{"/home/u"}); got != tt.want {
			t.Errorf("shortCwdPath(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestShortCwdPath_Windows(t *testing.T) {
	homes := []string{`C:\Users\me`}
	tests := []struct{ cwd, want string }{
		{`C:\Users\me\src`, `~\src`},
		{`C:\Users\me\src\a\b\repo`, `...\b\repo`},
		{`C:\Users\meg`, `C:\Users\meg`},
	}
	for _, tt := range tests {
		if got := shortCwdPath(tt.cwd, homes); got != tt.want {
			t.Errorf("shortCwdPath(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}
