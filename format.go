package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

var (
	costThresholdRed    = 50.0
	costThresholdYellow = 25.0
)

func fmtCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

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
	sym := "$"
	v := c
	suffix := false
	if activeCurrency.Rate > 0 {
		sym = activeCurrency.Symbol
		v = c * activeCurrency.Rate
		suffix = activeCurrency.Suffix
	}
	num := fmt.Sprintf("%.2f", v)
	if suffix {
		return num + " " + sym
	}
	return sym + num
}

func colorize(s string, cost float64) string {
	c := cost
	if activeCurrency.Rate > 0 {
		c = cost * activeCurrency.Rate
	}
	switch {
	case c >= costThresholdRed:
		return redString(s)
	case c >= costThresholdYellow:
		return yellowString(s)
	default:
		return s
	}
}

func colorCost(c float64, width int) string {
	s := fmtCost(c)
	if width == 0 {
		return colorize(s, c)
	}
	if len(s) > width {
		width = len(s)
	}
	return colorize(fmt.Sprintf("%*s", width, s), c)
}

// Prefer real cwd over slug — slug encoding loses '/' vs '_' vs '.'.
func displayProject(slug string, paths map[string]string) string {
	if real := paths[slug]; real != "" {
		return shortenRealPath(real)
	}
	return shortProject(slug)
}

func shortenRealPath(path string) string {
	p := strings.ReplaceAll(path, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		p = p[2:]
	}
	p = strings.TrimRight(p, "/")
	for _, prefix := range []string{"/Users/", "/home/"} {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			if _, after, ok := strings.Cut(rest, "/"); ok && after != "" {
				return after
			}
			return rest
		}
	}
	return p
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
	return s
}

func wrapName(name string, chunkSize int) []string {
	if name == "" {
		return nil
	}
	var chunks []string
	for len(name) > chunkSize {
		chunks = append(chunks, name[:chunkSize])
		name = name[chunkSize:]
	}
	chunks = append(chunks, name)
	return chunks
}

type OutputOptions struct {
	ShowDaily    bool
	ShowMonthly  bool
	ShowProjects bool
	ShowBranches bool
	TopN         int
}

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
	Date         string  `json:"date"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CacheWrite   int     `json:"cache_write_tokens"`
	Requests     int     `json:"requests"`
	Cost         float64 `json:"cost"`
}

type jsonMonthlyRow struct {
	Month        string  `json:"month"`
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CacheWrite   int     `json:"cache_write_tokens"`
	Requests     int     `json:"requests"`
	Cost         float64 `json:"cost"`
}

type jsonProjectRow struct {
	Project  string  `json:"project"`
	Model    string  `json:"model"`
	Requests int     `json:"requests"`
	Cost     float64 `json:"cost"`
}

type jsonBranchRow struct {
	Branch   string  `json:"branch"`
	Model    string  `json:"model"`
	Requests int     `json:"requests"`
	Cost     float64 `json:"cost"`
}

func buildJSONDaily(data *ParseResult) []jsonDailyRow {
	var daily []jsonDailyRow
	for date, dayModels := range data.DailyUsage {
		for model, b := range dayModels {
			daily = append(daily, jsonDailyRow{
				Date: date, Model: shortModel(model),
				InputTokens: b.InputTokens, OutputTokens: b.OutputTokens,
				CacheRead: b.CacheRead, CacheWrite: b.TotalCacheWrite(),
				Requests: b.Requests, Cost: b.Cost,
			})
		}
	}
	sort.Slice(daily, func(i, j int) bool {
		if daily[i].Date != daily[j].Date {
			return daily[i].Date > daily[j].Date
		}
		return daily[i].Cost > daily[j].Cost
	})
	return daily
}

func buildJSONMonthly(data *ParseResult) []jsonMonthlyRow {
	monthlyData := aggregateMonthly(data.DailyUsage)
	var monthly []jsonMonthlyRow
	for month, monthModels := range monthlyData {
		for model, b := range monthModels {
			monthly = append(monthly, jsonMonthlyRow{
				Month: month, Model: shortModel(model),
				InputTokens: b.InputTokens, OutputTokens: b.OutputTokens,
				CacheRead: b.CacheRead, CacheWrite: b.TotalCacheWrite(),
				Requests: b.Requests, Cost: b.Cost,
			})
		}
	}
	sort.Slice(monthly, func(i, j int) bool {
		if monthly[i].Month != monthly[j].Month {
			return monthly[i].Month > monthly[j].Month
		}
		return monthly[i].Cost > monthly[j].Cost
	})
	return monthly
}

func buildJSONProjects(data *ParseResult) []jsonProjectRow {
	var projects []jsonProjectRow
	for slug, projModels := range data.ProjectUsage {
		for model, b := range projModels {
			projects = append(projects, jsonProjectRow{Project: displayProject(slug, data.ProjectPaths), Model: shortModel(model), Requests: b.Requests, Cost: b.Cost})
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Cost > projects[j].Cost })
	return projects
}

func buildJSONBranches(data *ParseResult) []jsonBranchRow {
	var branches []jsonBranchRow
	for _, branchMap := range data.BranchUsage {
		for branch, models := range branchMap {
			for model, b := range models {
				branches = append(branches, jsonBranchRow{
					Branch: branch,
					Model:  shortModel(model), Requests: b.Requests, Cost: b.Cost,
				})
			}
		}
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Cost > branches[j].Cost })
	return branches
}

func printJSON(data *ParseResult, opts OutputOptions) {
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
		Monthly  interface{} `json:"monthly,omitempty"`
		Projects interface{} `json:"projects,omitempty"`
		Branches interface{} `json:"branches,omitempty"`
		Currency interface{} `json:"currency,omitempty"`
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
			WebSearches       int     `json:"web_searches,omitempty"`
			LongCtxRequests   int     `json:"long_context_requests,omitempty"`
			DateFrom          string  `json:"date_from,omitempty"`
			DateTo            string  `json:"date_to,omitempty"`
			FilesParsed       int     `json:"files_parsed"`
			DurationMs        int64   `json:"duration_ms"`
		}{totals.Cost, data.TotalRecords, totals.Input, totals.Output, totals.CacheR, totals.CacheW, totals.CacheW5m, totals.CacheW1h, totals.WebSearches, totals.LongCtxRequests, dateFrom, dateTo, data.TotalFiles, data.Duration.Milliseconds()},
		Models: models,
	}

	if activeCurrency.Rate > 0 && activeCurrency.Code != "" {
		out.Currency = struct {
			Code   string  `json:"code"`
			Symbol string  `json:"symbol"`
			Rate   float64 `json:"rate"`
		}{activeCurrency.Code, activeCurrency.Symbol, activeCurrency.Rate}
	}

	if opts.ShowDaily {
		out.Daily = buildJSONDaily(data)
	}
	if opts.ShowMonthly {
		out.Monthly = buildJSONMonthly(data)
	}
	if opts.ShowProjects {
		out.Projects = buildJSONProjects(data)
	}
	if opts.ShowBranches {
		out.Branches = buildJSONBranches(data)
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
	fmt.Println()
	bold.Println(strings.Repeat("═", 80))
	bold.Println("  Claude Code Usage Report")
	bold.Println(strings.Repeat("═", 80))
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
	if activeCurrency.Rate > 0 {
		if activeCurrency.Code != "" {
			dim.Printf("  Costs in %s (1 USD = %.4f %s)\n", activeCurrency.Code, activeCurrency.Rate, activeCurrency.Code)
		} else {
			dim.Printf("  Costs converted at 1 USD = %.4f %s\n", activeCurrency.Rate, activeCurrency.Symbol)
		}
	}
	fmt.Println()

	// Model breakdown
	bold.Println(strings.Repeat("─", 80))
	bold.Println("  MODEL BREAKDOWN")
	bold.Println(strings.Repeat("─", 80))
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
	if totals.WebSearches > 0 {
		dim.Printf("  Web searches: %d (%s)\n", totals.WebSearches, fmtCost(float64(totals.WebSearches)*webSearchCostPerSearch))
	}
	if totals.LongCtxRequests > 0 {
		dim.Printf("  Long-context requests (>%dK): %d (premium pricing applied)\n", longCtxThreshold/1000, totals.LongCtxRequests)
	}
	fmt.Println()

	if opts.ShowDaily {
		printDailyBreakdown(data, opts)
	}
	if opts.ShowMonthly {
		printMonthlyBreakdown(data, opts)
	}
	if opts.ShowProjects {
		printProjectBreakdown(data, opts)
	}
	if opts.ShowBranches {
		printBranchBreakdown(data, opts)
	}
}

func printDailyBreakdown(data *ParseResult, opts OutputOptions) {

	bold.Println(strings.Repeat("─", 80))
	bold.Println("  DAILY BREAKDOWN")
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-12s %-11s %7s %7s %8s %8s %6s %8s\n",
		"Date", "Model", "Input", "Output", "Cache R", "Cache W", "Reqs", "Cost")
	fmt.Println("  " + strings.Repeat("─", 75))

	var dates []string
	for d := range data.DailyUsage {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	if opts.TopN > 0 && len(dates) > opts.TopN {
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
			fmt.Printf("  %-12s %s %7s %7s %8s %8s %6d %s\n",
				d, cyan.Sprintf("%-11s", shortModel(m.name)),
				fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
				fmtTokens(b.CacheRead), fmtTokens(b.TotalCacheWrite()),
				b.Requests, colorCost(b.Cost, 8))
			first = false
		}
		fmt.Printf("  %-12s %-11s %7s %7s %8s %8s %6d %s\n",
			"", "", "", "", "", "", dayReqs, colorCost(dayCost, 8))
		fmt.Println()
	}
}

func aggregateMonthly(dailyUsage map[string]map[string]*Bucket) map[string]map[string]*Bucket {
	monthly := make(map[string]map[string]*Bucket)
	for date, dayModels := range dailyUsage {
		month := date
		if len(date) >= 7 {
			month = date[:7]
		}
		for model, b := range dayModels {
			mb := getOrCreateNestedBucket(monthly, month, model)
			mb.InputTokens += b.InputTokens
			mb.OutputTokens += b.OutputTokens
			mb.CacheRead += b.CacheRead
			mb.CacheWrite5m += b.CacheWrite5m
			mb.CacheWrite1h += b.CacheWrite1h
			mb.Cost += b.Cost
			mb.Requests += b.Requests
		}
	}
	return monthly
}

func printMonthlyBreakdown(data *ParseResult, opts OutputOptions) {

	bold.Println(strings.Repeat("─", 80))
	bold.Println("  MONTHLY BREAKDOWN")
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-12s %-11s %7s %7s %8s %8s %6s %8s\n",
		"Month", "Model", "Input", "Output", "Cache R", "Cache W", "Reqs", "Cost")
	fmt.Println("  " + strings.Repeat("─", 75))

	monthly := aggregateMonthly(data.DailyUsage)

	var months []string
	for m := range monthly {
		months = append(months, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(months)))
	if opts.TopN > 0 && len(months) > opts.TopN {
		months = months[:opts.TopN]
	}

	for _, month := range months {
		monthModels := monthly[month]
		var monthCost float64
		var monthReqs int

		var sorted []modelEntry
		for name, b := range monthModels {
			sorted = append(sorted, modelEntry{name, b})
			monthCost += b.Cost
			monthReqs += b.Requests
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].bucket.Cost > sorted[j].bucket.Cost })

		first := true
		for _, m := range sorted {
			b := m.bucket
			label := ""
			if first {
				label = month
			}
			fmt.Printf("  %-12s %s %7s %7s %8s %8s %6d %s\n",
				label, cyan.Sprintf("%-11s", shortModel(m.name)),
				fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
				fmtTokens(b.CacheRead), fmtTokens(b.TotalCacheWrite()),
				b.Requests, colorCost(b.Cost, 8))
			first = false
		}
		fmt.Printf("  %-12s %-11s %7s %7s %8s %8s %6d %s\n",
			"", "", "", "", "", "", monthReqs, colorCost(monthCost, 8))
		fmt.Println()
	}
}

func printProjectBreakdown(data *ParseResult, opts OutputOptions) {

	bold.Println(strings.Repeat("─", 80))
	bold.Println("  PROJECT BREAKDOWN")
	bold.Println(strings.Repeat("─", 80))
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
	if opts.TopN > 0 && len(projects) > opts.TopN {
		projects = projects[:opts.TopN]
	}

	for _, proj := range projects {
		projModels := data.ProjectUsage[proj.slug]
		name := displayProject(proj.slug, data.ProjectPaths)

		var sorted []modelEntry
		for mname, b := range projModels {
			sorted = append(sorted, modelEntry{mname, b})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].bucket.Cost > sorted[j].bucket.Cost })

		names := wrapName(name, 30)
		for i, m := range sorted {
			n := ""
			if i < len(names) {
				n = names[i]
			}
			b := m.bucket
			fmt.Printf("  %-35s %s %7d %s\n",
				n, cyan.Sprintf("%-16s", shortModel(m.name)),
				b.Requests, colorCost(b.Cost, 10))
		}
		for i := len(sorted); i < len(names)-1; i++ {
			fmt.Printf("  %s\n", names[i])
		}
		subtotalName := ""
		if len(names) > len(sorted) {
			subtotalName = names[len(names)-1]
		}
		fmt.Printf("  %-35s %-16s %7s %s\n",
			subtotalName, "SUBTOTAL", "", colorCost(proj.total, 10))
		fmt.Println()
	}
}

func printBranchBreakdown(data *ParseResult, opts OutputOptions) {

	bold.Println(strings.Repeat("─", 80))
	bold.Println("  BRANCH BREAKDOWN")
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-30s %-16s %7s %10s\n",
		"Branch", "Model", "Reqs", "Cost")
	fmt.Println("  " + strings.Repeat("─", 75))

	type branchTotal struct {
		branch string
		total  float64
	}

	for _, branchMap := range data.BranchUsage {
		var branchList []branchTotal
		for br, models := range branchMap {
			var bt float64
			for _, b := range models {
				bt += b.Cost
			}
			branchList = append(branchList, branchTotal{br, bt})
		}
		sort.Slice(branchList, func(i, j int) bool { return branchList[i].total > branchList[j].total })
		if opts.TopN > 0 && len(branchList) > opts.TopN {
			branchList = branchList[:opts.TopN]
		}

		for _, br := range branchList {
			models := branchMap[br.branch]
			var sorted []modelEntry
			for mname, b := range models {
				sorted = append(sorted, modelEntry{mname, b})
			}
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].bucket.Cost > sorted[j].bucket.Cost })

			names := wrapName(br.branch, 25)
			for i, m := range sorted {
				n := ""
				if i < len(names) {
					n = names[i]
				}
				b := m.bucket
				fmt.Printf("  %-30s %s %7d %s\n",
					n, cyan.Sprintf("%-16s", shortModel(m.name)),
					b.Requests, colorCost(b.Cost, 10))
			}
			for i := len(sorted); i < len(names)-1; i++ {
				fmt.Printf("  %s\n", names[i])
			}
			subtotalName := ""
			if len(names) > len(sorted) {
				subtotalName = names[len(names)-1]
			}
			fmt.Printf("  %-30s %-16s %7s %s\n",
				subtotalName, "SUBTOTAL", "", colorCost(br.total, 10))
			fmt.Println()
		}
	}
	fmt.Println()
}

func printWrappedList(items []string, maxWidth int) {
	line := "  "
	for i, s := range items {
		entry := s
		if i < len(items)-1 {
			entry += ", "
		}
		if len(line)+len(entry) > maxWidth {
			fmt.Println(line)
			line = "  "
		}
		line += entry
	}
	if len(line) > 2 {
		fmt.Println(line)
	}
}

func unusedSkills(data *ToolResult) []string {
	used := make(map[string]bool)
	for name := range data.SkillCounts {
		used[name] = true
	}
	var unused []string
	for _, skill := range data.AvailableSkills {
		if !used[skill] {
			unused = append(unused, skill)
		}
	}
	return unused
}

type toolEntry struct {
	name   string
	count  int
	errors int
	projs  int
}

func fmtAgentDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	totalSec := int(d.Seconds())
	if totalSec < 60 {
		return fmt.Sprintf("%ds", totalSec)
	}
	m := totalSec / 60
	s := totalSec % 60
	if m < 60 {
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := m / 60
	m = m % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func printAgentBreakdown(data *ToolResult, topN int) {
	if len(data.AgentCounts) == 0 {
		return
	}

	var totalAgents int
	for _, c := range data.AgentCounts {
		totalAgents += c
	}

	bold.Println(strings.Repeat("─", 80))
	bold.Printf("  AGENT BREAKDOWN (%d spawned, %d unique, %s sessions)\n",
		totalAgents, len(data.AgentCounts), fmtCount(len(data.AgentSessions)))
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-28s  %12s  %10s  %8s  %10s\n", "Agent Type", "Invocations", "Avg Time", "Total", "Projects")
	fmt.Println("  " + strings.Repeat("─", 76))

	type agentEntry struct {
		name      string
		count     int
		totalTime time.Duration
		projs     int
	}

	var agents []agentEntry
	for name, count := range data.AgentCounts {
		agents = append(agents, agentEntry{name, count, data.AgentTotalTime[name], len(data.AgentProjects[name])})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].count > agents[j].count })
	if topN > 0 && len(agents) > topN {
		agents = agents[:topN]
	}

	for _, a := range agents {
		avgStr := "-"
		totalStr := "-"
		if a.totalTime > 0 {
			avg := a.totalTime / time.Duration(a.count)
			avgStr = fmtAgentDuration(avg)
			totalStr = fmtAgentDuration(a.totalTime)
		}
		names := wrapName(a.name, 26)
		fmt.Printf("  %-28s  %12s  %10s  %8s  %10d\n", names[0], fmtCount(a.count), avgStr, totalStr, a.projs)
		for _, n := range names[1:] {
			fmt.Printf("  %s\n", n)
		}
	}
	fmt.Println()
}

func printSkillBreakdown(data *ToolResult, topN int) {
	if len(data.SkillCounts) == 0 && len(data.AvailableSkills) == 0 {
		return
	}

	var totalSkillInvocations int
	for _, c := range data.SkillCounts {
		totalSkillInvocations += c
	}
	header := fmt.Sprintf("  SKILL BREAKDOWN (%d invocations, %d unique, %s sessions",
		totalSkillInvocations, len(data.SkillCounts), fmtCount(len(data.SkillSessions)))
	if len(data.AvailableSkills) > 0 {
		header += fmt.Sprintf(", %d available", len(data.AvailableSkills))
	}
	header += ")"

	bold.Println(strings.Repeat("─", 80))
	bold.Println(header)
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-46s  %14s  %12s\n", "Skill", "Invocations", "Projects")
	fmt.Println("  " + strings.Repeat("─", 76))

	var skills []toolEntry
	for name, count := range data.SkillCounts {
		skills = append(skills, toolEntry{name: name, count: count, projs: len(data.SkillProjects[name])})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].count > skills[j].count })
	if topN > 0 && len(skills) > topN {
		skills = skills[:topN]
	}

	for _, s := range skills {
		names := wrapName(s.name, 44)
		fmt.Printf("  %-46s  %14s  %12d\n", names[0], fmtCount(s.count), s.projs)
		for _, n := range names[1:] {
			fmt.Printf("  %s\n", n)
		}
	}
	fmt.Println()

	if len(data.AvailableSkills) > 0 {
		unused := unusedSkills(data)
		if len(unused) > 0 {
			fmt.Printf("  %s %d unused skills (0 invocations):\n", yellowString("⚠"), len(unused))
			printWrappedList(unused, 78)
			fmt.Println()
		}
	}
}

func printToolUsage(data *ToolResult, topN int) {
	fmt.Println()
	bold.Println(strings.Repeat("═", 80))
	bold.Println("  Tool Usage")
	bold.Println(strings.Repeat("═", 80))
	fmt.Printf("  Parsed %d log files ", data.TotalFiles)
	dim.Printf("(%s)\n", fmtDuration(data.Duration))
	fmt.Println()

	var totalInvocations int
	for _, c := range data.ToolCounts {
		totalInvocations += c
	}
	bold.Println(strings.Repeat("─", 80))
	bold.Printf("  TOOL BREAKDOWN (%s total, %d unique, %s sessions)\n",
		fmtCount(totalInvocations), len(data.ToolCounts), fmtCount(len(data.ToolSessions)))
	bold.Println(strings.Repeat("─", 80))
	fmt.Printf("  %-38s  %12s  %9s  %11s\n", "Tool", "Invocations", "Errors", "Projects")
	fmt.Println("  " + strings.Repeat("─", 76))

	var tools []toolEntry
	for name, count := range data.ToolCounts {
		tools = append(tools, toolEntry{name, count, data.ToolErrors[name], len(data.ToolProjects[name])})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].count > tools[j].count })
	if topN > 0 && len(tools) > topN {
		tools = tools[:topN]
	}

	for _, t := range tools {
		errStr := "-"
		if t.errors > 0 {
			errStr = fmt.Sprintf("%.1f%%", float64(t.errors)/float64(t.count)*100)
		}
		names := wrapName(t.name, 36)
		fmt.Printf("  %-38s  %12s  %9s  %11d\n", names[0], fmtCount(t.count), errStr, t.projs)
		for _, n := range names[1:] {
			fmt.Printf("  %s\n", n)
		}
	}
	fmt.Println()

	printAgentBreakdown(data, topN)
	printSkillBreakdown(data, topN)
}

func printToolsJSON(data *ToolResult, topN int) {
	type jsonToolRow struct {
		Tool        string  `json:"tool"`
		Invocations int     `json:"invocations"`
		Errors      int     `json:"errors"`
		ErrorRate   float64 `json:"error_rate"`
		Projects    int     `json:"projects"`
	}
	type jsonSkillRow struct {
		Skill       string `json:"skill"`
		Invocations int    `json:"invocations"`
		Projects    int    `json:"projects"`
	}
	type jsonAgentRow struct {
		AgentType   string `json:"agent_type"`
		Invocations int    `json:"invocations"`
		AvgTimeMs   int64  `json:"avg_time_ms"`
		TotalTimeMs int64  `json:"total_time_ms"`
		Projects    int    `json:"projects"`
	}

	var tools []jsonToolRow
	for name, count := range data.ToolCounts {
		errs := data.ToolErrors[name]
		rate := 0.0
		if count > 0 {
			rate = float64(errs) / float64(count)
		}
		tools = append(tools, jsonToolRow{name, count, errs, rate, len(data.ToolProjects[name])})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Invocations > tools[j].Invocations })
	if topN > 0 && len(tools) > topN {
		tools = tools[:topN]
	}

	var skills []jsonSkillRow
	for name, count := range data.SkillCounts {
		skills = append(skills, jsonSkillRow{name, count, len(data.SkillProjects[name])})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Invocations > skills[j].Invocations })
	if topN > 0 && len(skills) > topN {
		skills = skills[:topN]
	}

	var agents []jsonAgentRow
	for name, count := range data.AgentCounts {
		totalMs := data.AgentTotalTime[name].Milliseconds()
		avgMs := int64(0)
		if count > 0 && totalMs > 0 {
			avgMs = totalMs / int64(count)
		}
		agents = append(agents, jsonAgentRow{name, count, avgMs, totalMs, len(data.AgentProjects[name])})
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Invocations > agents[j].Invocations })
	if topN > 0 && len(agents) > topN {
		agents = agents[:topN]
	}

	unused := unusedSkills(data)

	out := struct {
		Tools           []jsonToolRow  `json:"tools"`
		Agents          []jsonAgentRow `json:"agents,omitempty"`
		Skills          []jsonSkillRow `json:"skills"`
		UnusedSkills    []string       `json:"unused_skills,omitempty"`
		AvailableSkills int            `json:"available_skills"`
		ToolSessions    int            `json:"tool_sessions"`
		AgentSessions   int            `json:"agent_sessions"`
		SkillSessions   int            `json:"skill_sessions"`
		TotalFiles      int            `json:"total_files"`
		DurationMs      int64          `json:"duration_ms"`
	}{
		Tools:           tools,
		Agents:          agents,
		Skills:          skills,
		UnusedSkills:    unused,
		AvailableSkills: len(data.AvailableSkills),
		ToolSessions:    len(data.ToolSessions),
		AgentSessions:   len(data.AgentSessions),
		SkillSessions:   len(data.SkillSessions),
		TotalFiles:      data.TotalFiles,
		DurationMs:      data.Duration.Milliseconds(),
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}
