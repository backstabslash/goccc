package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"time"

	"github.com/fatih/color"
)

var version string

func resolveVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return "dev"
}

func init() {
	if version == "" {
		version = resolveVersion()
	}
}

func main() {
	days := flag.Int("days", 0, "Only show usage from the last N days (0 = all time)")
	project := flag.String("project", "", "Filter by project name (substring match)")
	daily := flag.Bool("daily", false, "Show daily breakdown")
	projects := flag.Bool("projects", false, "Show per-project breakdown")
	all := flag.Bool("all", false, "Show all breakdowns (daily + projects)")
	topN := flag.Int("top", 15, "Number of entries to show in breakdowns")
	homeDir, _ := os.UserHomeDir()
	baseDir := flag.String("base-dir", homeDir+"/.claude", "Base directory for Claude Code data")
	jsonOutput := flag.Bool("json", false, "Output as JSON")
	noColor := flag.Bool("no-color", false, "Disable colored output")
	showVersion := flag.Bool("version", false, "Show version")
	statusline := flag.Bool("statusline", false, "Statusline mode: read session JSON from stdin, output formatted cost line")

	flag.IntVar(days, "d", 0, "Short for -days")
	flag.StringVar(project, "p", "", "Short for -project")
	flag.IntVar(topN, "n", 15, "Short for -top")

	flag.Parse()

	if *showVersion {
		fmt.Printf("goccc %s\n", version)
		os.Exit(0)
	}

	if *noColor || os.Getenv("NO_COLOR") != "" {
		color.NoColor = true
	}

	if *statusline {
		runStatusline(*baseDir)
		return
	}

	if *all {
		*daily = true
		*projects = true
	}

	start := time.Now()
	data, err := parseLogs(*baseDir, *days, *project)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	data.Duration = time.Since(start)

	if data.TotalRecords == 0 {
		fmt.Println("No usage data found.")
		os.Exit(0)
	}

	opts := OutputOptions{
		ShowDaily:    *daily,
		ShowProjects: *projects,
		TopN:         *topN,
		JSONOutput:   *jsonOutput,
	}

	if *jsonOutput {
		printJSON(data, opts)
	} else {
		printSummary(data, opts)
	}
}
