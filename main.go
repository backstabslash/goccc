package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
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
	monthly := flag.Bool("monthly", false, "Show monthly breakdown")
	projects := flag.Bool("projects", false, "Show per-project breakdown")
	all := flag.Bool("all", false, "Show all breakdowns (daily + projects)")
	topN := flag.Int("top", 0, "Max entries in breakdowns (0 = all)")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	baseDir := flag.String("base-dir", filepath.Join(homeDir, ".claude"), "Base directory for Claude Code data")
	jsonOutput := flag.Bool("json", false, "Output as JSON")
	noColor := flag.Bool("no-color", false, "Disable colored output")
	showVersion := flag.Bool("version", false, "Show version")
	statusline := flag.Bool("statusline", false, "Statusline mode: read session JSON from stdin, output formatted cost line")
	noMCP := flag.Bool("no-mcp", false, "Hide MCP servers from statusline output")
	cache5m := flag.Bool("cache-5m", false, "Price cache writes at 5-minute tier (1.25x) instead of 1-hour (2x)")
	currencySymbolFlag := flag.String("currency-symbol", "", "Override currency symbol (requires -currency-rate)")
	currencyRateFlag := flag.Float64("currency-rate", 0, "Override exchange rate from USD (requires -currency-symbol)")

	flag.IntVar(days, "d", 0, "Short for -days")
	flag.StringVar(project, "p", "", "Short for -project")
	flag.BoolVar(monthly, "m", false, "Short for -monthly")
	flag.IntVar(topN, "n", 0, "Short for -top")
	flag.BoolVar(showVersion, "V", false, "Short for -version")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: goccc [flags]\n\n")
		fmt.Fprintf(os.Stderr, "A CLI cost calculator for Claude Code.\n")
		fmt.Fprintf(os.Stderr, "Parses JSONL logs from ~/.claude/projects/ and breaks down\n")
		fmt.Fprintf(os.Stderr, "spending by model, day, and project.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  goccc                          All-time summary\n")
		fmt.Fprintf(os.Stderr, "  goccc -days 7 -all             Last 7 days, all breakdowns\n")
		fmt.Fprintf(os.Stderr, "  goccc -days 1                  Today's usage\n")
		fmt.Fprintf(os.Stderr, "  goccc -monthly                 Monthly cost summary\n")
		fmt.Fprintf(os.Stderr, "  goccc -project webapp -daily   Filter by project with daily breakdown\n")
		fmt.Fprintf(os.Stderr, "  goccc -json | jq '.summary'    JSON output for scripting\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showVersion {
		fmt.Printf("goccc %s\n", version)
		os.Exit(0)
	}

	if *noColor {
		noColorFlag = true
	}

	if *cache5m {
		cacheWriteAs1h = false
	}

	if err := initCurrency(*currencySymbolFlag, *currencyRateFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *statusline {
		runStatusline(*baseDir, *noMCP)
		return
	}

	updateCh := checkForUpdate(version)

	if *daily && *monthly {
		fmt.Fprintf(os.Stderr, "Error: -daily and -monthly are mutually exclusive\n")
		os.Exit(1)
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
		ShowMonthly:  *monthly,
		ShowProjects: *projects,
		ShowBranches: *project != "",
		TopN:         *topN,
	}

	if *jsonOutput {
		printJSON(data, opts)
	} else {
		printSummary(data, opts)
	}

	printUpdateNotice(<-updateCh)
}
