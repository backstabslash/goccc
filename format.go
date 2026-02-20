package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
)

const (
	costThresholdRed    = 25.0
	costThresholdYellow = 10.0
)

func fmtTokens(n int) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

func fmtCost(c float64) string {
	if c >= 1.0 {
		return fmt.Sprintf("$%.2f", c)
	}
	return fmt.Sprintf("$%.4f", c)
}

func colorize(s string, cost float64) string {
	switch {
	case cost >= costThresholdRed:
		return color.RedString(s)
	case cost >= costThresholdYellow:
		return color.YellowString(s)
	default:
		return color.GreenString(s)
	}
}

func colorCost(c float64, width int) string {
	return colorize(fmt.Sprintf("%*s", width, fmtCost(c)), c)
}

func shortProject(slug string) string {
	s := slug
	for _, prefix := range []string{"-Users-", "-home-"} {
		if idx := strings.Index(s, prefix); idx >= 0 {
			rest := s[idx+len(prefix):]
			if slashIdx := strings.Index(rest, "-"); slashIdx >= 0 {
				s = rest[slashIdx+1:]
			}
		}
	}

	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")

	if idx := strings.Index(s, "-"); idx >= 0 {
		s = s[:idx] + "/" + s[idx+1:]
	}

	if s == "" {
		s = slug
	}
	if len(s) > 40 {
		s = "..." + s[len(s)-37:]
	}
	return s
}

type OutputOptions struct {
	ShowDaily    bool
	ShowProjects bool
	TopN         int
}

func printJSON(data *ParseResult, opts OutputOptions) {
	type jsonModelRow struct {
		Model        string  `json:"model"`
		InputTokens  int     `json:"input_tokens"`
		OutputTokens int     `json:"output_tokens"`
		CacheRead    int     `json:"cache_read_tokens"`
		CacheWrite   int     `json:"cache_write_tokens"`
		CacheWrite5m int     `json:"cache_write_5m_tokens"`
		CacheWrite1h int     `json:"cache_write_1h_tokens"`
		Requests     int     `json:"requests"`
		Cost         float64 `json:"cost"`
	}

	type jsonDailyRow struct {
		Date     string  `json:"date"`
		Model    string  `json:"model"`
		Requests int     `json:"requests"`
		Cost     float64 `json:"cost"`
	}

	type jsonProjectRow struct {
		Project  string  `json:"project"`
		Model    string  `json:"model"`
		Requests int     `json:"requests"`
		Cost     float64 `json:"cost"`
	}

	totals := data.Totals()
	dateFrom, dateTo := data.DateRange()
	var models []jsonModelRow
	for model, b := range data.ModelUsage {
		models = append(models, jsonModelRow{
			Model: shortModel(model), InputTokens: b.InputTokens,
			OutputTokens: b.OutputTokens, CacheRead: b.CacheRead,
			CacheWrite: b.TotalCacheWrite(), CacheWrite5m: b.CacheWrite5m,
			CacheWrite1h: b.CacheWrite1h, Requests: b.Requests, Cost: b.Cost,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Cost > models[j].Cost })

	out := struct {
		Summary  interface{} `json:"summary"`
		Models   interface{} `json:"models"`
		Daily    interface{} `json:"daily,omitempty"`
		Projects interface{} `json:"projects,omitempty"`
	}{
		Summary: struct {
			TotalCost         float64 `json:"total_cost"`
			TotalRequests     int     `json:"total_requests"`
			TotalInput        int     `json:"total_input_tokens"`
			TotalOutput       int     `json:"total_output_tokens"`
			TotalCacheRead    int     `json:"total_cache_read_tokens"`
			TotalCacheWrite   int     `json:"total_cache_write_tokens"`
			TotalCacheWrite5m int     `json:"total_cache_write_5m_tokens"`
			TotalCacheWrite1h int     `json:"total_cache_write_1h_tokens"`
			DateFrom          string  `json:"date_from,omitempty"`
			DateTo            string  `json:"date_to,omitempty"`
			FilesParsed       int     `json:"files_parsed"`
			DurationMs        int64   `json:"duration_ms"`
		}{totals.Cost, data.TotalRecords, totals.Input, totals.Output, totals.CacheR, totals.CacheW, totals.CacheW5m, totals.CacheW1h, dateFrom, dateTo, data.TotalFiles, data.Duration.Milliseconds()},
		Models: models,
	}

	if opts.ShowDaily {
		var daily []jsonDailyRow
		for date, dayModels := range data.DailyUsage {
			for model, b := range dayModels {
				daily = append(daily, jsonDailyRow{Date: date, Model: shortModel(model), Requests: b.Requests, Cost: b.Cost})
			}
		}
		sort.Slice(daily, func(i, j int) bool {
			if daily[i].Date != daily[j].Date {
				return daily[i].Date > daily[j].Date
			}
			return daily[i].Cost > daily[j].Cost
		})
		out.Daily = daily
	}

	if opts.ShowProjects {
		var projects []jsonProjectRow
		for slug, projModels := range data.ProjectUsage {
			for model, b := range projModels {
				projects = append(projects, jsonProjectRow{Project: shortProject(slug), Model: shortModel(model), Requests: b.Requests, Cost: b.Cost})
			}
		}
		sort.Slice(projects, func(i, j int) bool { return projects[i].Cost > projects[j].Cost })
		out.Projects = projects
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

type modelEntry struct {
	name   string
	bucket *Bucket
}

func printSummary(data *ParseResult, opts OutputOptions) {
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)

	fmt.Println()
	bold.Println("═══════════════════════════════════════════════════════════════════════════════")
	bold.Println("  Claude Code Usage Report")
	bold.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("  Parsed %d log files, %d API calls ", data.TotalFiles, data.TotalRecords)
	dim.Printf("(%s)\n", fmtDuration(data.Duration))
	if from, to := data.DateRange(); from != "" {
		if from == to {
			fmt.Printf("  Date: %s\n", from)
		} else {
			fmt.Printf("  Period: %s to %s\n", from, to)
		}
	}
	if data.ParseErrors > 0 {
		dim.Printf("  (%d parse errors skipped)\n", data.ParseErrors)
	}
	fmt.Println()

	// Model breakdown
	bold.Println("───────────────────────────────────────────────────────────────────────────────")
	bold.Println("  MODEL BREAKDOWN")
	bold.Println("───────────────────────────────────────────────────────────────────────────────")
	fmt.Printf("  %-16s %9s %9s %9s %9s %7s %10s\n",
		"Model", "Input", "Output", "Cache R", "Cache W", "Reqs", "Cost")
	fmt.Println("  " + strings.Repeat("─", 75))

	totals := data.Totals()

	var models []modelEntry
	for name, b := range data.ModelUsage {
		models = append(models, modelEntry{name, b})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].bucket.Cost > models[j].bucket.Cost })

	for _, m := range models {
		b := m.bucket
		fmt.Printf("  %s %9s %9s %9s %9s %7d %s\n",
			cyan.Sprintf("%-16s", shortModel(m.name)),
			fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
			fmtTokens(b.CacheRead), fmtTokens(b.TotalCacheWrite()),
			b.Requests, colorCost(b.Cost, 10))
	}

	fmt.Println("  " + strings.Repeat("─", 75))
	bold.Printf("  %-16s %9s %9s %9s %9s %7d %s\n",
		"TOTAL",
		fmtTokens(totals.Input), fmtTokens(totals.Output),
		fmtTokens(totals.CacheR), fmtTokens(totals.CacheW),
		totals.Requests, colorCost(totals.Cost, 10))
	fmt.Println()

	// Daily breakdown
	if opts.ShowDaily {
		bold.Println("───────────────────────────────────────────────────────────────────────────────")
		bold.Println("  DAILY BREAKDOWN")
		bold.Println("───────────────────────────────────────────────────────────────────────────────")
		fmt.Printf("  %-12s %-16s %9s %9s %7s %10s\n",
			"Date", "Model", "Input", "Output", "Reqs", "Cost")
		fmt.Println("  " + strings.Repeat("─", 75))

		var dates []string
		for d := range data.DailyUsage {
			dates = append(dates, d)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(dates)))
		if len(dates) > opts.TopN {
			dates = dates[:opts.TopN]
		}

		for _, date := range dates {
			dayModels := data.DailyUsage[date]
			var dayCost float64
			var dayReqs int

			var sorted []modelEntry
			for name, b := range dayModels {
				sorted = append(sorted, modelEntry{name, b})
				dayCost += b.Cost
				dayReqs += b.Requests
			}
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].bucket.Cost > sorted[j].bucket.Cost })

			first := true
			for _, m := range sorted {
				b := m.bucket
				d := ""
				if first {
					d = date
				}
				fmt.Printf("  %-12s %s %9s %9s %7d %s\n",
					d, cyan.Sprintf("%-16s", shortModel(m.name)),
					fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
					b.Requests, colorCost(b.Cost, 10))
				first = false
			}
			fmt.Printf("  %-12s %-16s %9s %9s %7d %s\n",
				"", "", "", "", dayReqs, colorCost(dayCost, 10))
			fmt.Println()
		}
	}

	// Project breakdown
	if opts.ShowProjects {
		bold.Println("───────────────────────────────────────────────────────────────────────────────")
		bold.Println("  PROJECT BREAKDOWN")
		bold.Println("───────────────────────────────────────────────────────────────────────────────")
		fmt.Printf("  %-35s %-16s %7s %10s\n",
			"Project", "Model", "Reqs", "Cost")
		fmt.Println("  " + strings.Repeat("─", 75))

		type projTotal struct {
			slug  string
			total float64
		}
		var projects []projTotal
		for slug, projModels := range data.ProjectUsage {
			var t float64
			for _, b := range projModels {
				t += b.Cost
			}
			projects = append(projects, projTotal{slug, t})
		}
		sort.Slice(projects, func(i, j int) bool { return projects[i].total > projects[j].total })
		if len(projects) > opts.TopN {
			projects = projects[:opts.TopN]
		}

		for _, proj := range projects {
			projModels := data.ProjectUsage[proj.slug]
			name := shortProject(proj.slug)

			var sorted []modelEntry
			for mname, b := range projModels {
				sorted = append(sorted, modelEntry{mname, b})
			}
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].bucket.Cost > sorted[j].bucket.Cost })

			first := true
			for _, m := range sorted {
				b := m.bucket
				n := ""
				if first {
					n = name
					if len(n) > 35 {
						n = n[:32] + "..."
					}
				}
				fmt.Printf("  %-35s %s %7d %s\n",
					n, cyan.Sprintf("%-16s", shortModel(m.name)),
					b.Requests, colorCost(b.Cost, 10))
				first = false
			}
			fmt.Printf("  %-35s %-16s %7s %s\n",
				"", "SUBTOTAL", "", colorCost(proj.total, 10))
			fmt.Println()
		}
	}
}
