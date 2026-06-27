package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	costThresholdRed    = 50.0
	costThresholdYellow = 25.0
)

// sortAndTrim sorts s in place by less (which should express the desired
// ordering, e.g. higher-cost-first) and returns at most topN elements.
// A topN of 0 (or negative) keeps all of them.
func sortAndTrim[T any](s []T, less func(a, b T) bool, topN int) []T {
	sort.Slice(s, func(i, j int) bool { return less(s[i], s[j]) })
	if topN > 0 && len(s) > topN {
		return s[:topN]
	}
	return s
}

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

func toDisplayCurrency(c float64) float64 {
	if activeCurrency.Rate > 0 {
		return c * activeCurrency.Rate
	}
	return c
}

func fmtCost(c float64) string {
	sym := "$"
	suffix := false
	if activeCurrency.Rate > 0 {
		sym = activeCurrency.Symbol
		suffix = activeCurrency.Suffix
	}
	num := fmt.Sprintf("%.2f", toDisplayCurrency(c))
	if suffix {
		return num + " " + sym
	}
	return sym + num
}

func colorize(s string, cost float64) string {
	c := toDisplayCurrency(cost)
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
	if pad := width - utf8.RuneCountInString(s); pad > 0 {
		s = strings.Repeat(" ", pad) + s
	}
	return colorize(s, c)
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

// jsonPeriodRow holds the columns shared by the daily and monthly JSON rows.
// Each variant embeds it after its own period key, so marshaling keeps the
// original field order (period first, then these).
type jsonPeriodRow struct {
	Model        string  `json:"model"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CacheWrite   int     `json:"cache_write_tokens"`
	Requests     int     `json:"requests"`
	Cost         float64 `json:"cost"`
}

func periodRow(model string, b *Bucket) jsonPeriodRow {
	return jsonPeriodRow{
		Model: shortModel(model), InputTokens: b.InputTokens, OutputTokens: b.OutputTokens,
		CacheRead: b.CacheRead, CacheWrite: b.TotalCacheWrite(),
		Requests: b.Requests, Cost: b.Cost,
	}
}

type jsonDailyRow struct {
	Date string `json:"date"`
	jsonPeriodRow
}

type jsonMonthlyRow struct {
	Month string `json:"month"`
	jsonPeriodRow
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
			daily = append(daily, jsonDailyRow{Date: date, jsonPeriodRow: periodRow(model, b)})
		}
	}
	return sortAndTrim(daily, func(a, b jsonDailyRow) bool {
		if a.Date != b.Date {
			return a.Date > b.Date
		}
		return a.Cost > b.Cost
	}, 0)
}

func buildJSONMonthly(data *ParseResult) []jsonMonthlyRow {
	monthlyData := aggregateMonthly(data.DailyUsage)
	var monthly []jsonMonthlyRow
	for month, monthModels := range monthlyData {
		for model, b := range monthModels {
			monthly = append(monthly, jsonMonthlyRow{Month: month, jsonPeriodRow: periodRow(model, b)})
		}
	}
	return sortAndTrim(monthly, func(a, b jsonMonthlyRow) bool {
		if a.Month != b.Month {
			return a.Month > b.Month
		}
		return a.Cost > b.Cost
	}, 0)
}

func buildJSONProjects(data *ParseResult) []jsonProjectRow {
	var projects []jsonProjectRow
	for slug, projModels := range data.ProjectUsage {
		for model, b := range projModels {
			projects = append(projects, jsonProjectRow{Project: displayProject(slug, data.ProjectPaths), Model: shortModel(model), Requests: b.Requests, Cost: b.Cost})
		}
	}
	return sortAndTrim(projects, func(a, b jsonProjectRow) bool { return a.Cost > b.Cost }, 0)
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
	return sortAndTrim(branches, func(a, b jsonBranchRow) bool { return a.Cost > b.Cost }, 0)
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
	models = sortAndTrim(models, func(a, b jsonModelRow) bool { return a.Cost > b.Cost }, 0)

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

func printSectionHeader(title string) {
	bold.Println(strings.Repeat("─", 80))
	bold.Println("  " + title)
	bold.Println(strings.Repeat("─", 80))
}

// tableRule returns an underline matching the visible width of header
func tableRule(header string) string {
	return "  " + strings.Repeat("─", utf8.RuneCountInString(header)-2)
}

func sortedByCost(models map[string]*Bucket) []modelEntry {
	entries := make([]modelEntry, 0, len(models))
	for name, b := range models {
		entries = append(entries, modelEntry{name, b})
	}
	return sortAndTrim(entries, func(a, b modelEntry) bool { return a.bucket.Cost > b.bucket.Cost }, 0)
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
	printSectionHeader("MODEL BREAKDOWN")
	header := fmt.Sprintf("  %-16s %9s %9s %9s %9s %7s %10s",
		"Model", "Input", "Output", "Cache R", "Cache W", "Reqs", "Cost")
	fmt.Println(header)
	rule := tableRule(header)
	fmt.Println(rule)

	totals := data.Totals()

	models := sortedByCost(data.ModelUsage)

	for _, m := range models {
		b := m.bucket
		fmt.Printf("  %s %9s %9s %9s %9s %7d %s\n",
			cyan.Sprintf("%-16s", shortModel(m.name)),
			fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
			fmtTokens(b.CacheRead), fmtTokens(b.TotalCacheWrite()),
			b.Requests, colorCost(b.Cost, 10))
	}

	fmt.Println(rule)
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

// printPeriodBreakdown renders a date-bucketed table (daily or monthly). Keys
// are sorted descending; each key's models sort by cost, the first row carries
// the period label, and a trailing subtotal row closes the group.
func printPeriodBreakdown(title, colLabel string, source map[string]map[string]*Bucket, topN int) {
	printSectionHeader(title)
	header := fmt.Sprintf("  %-12s %-11s %7s %7s %8s %8s %6s %10s",
		colLabel, "Model", "Input", "Output", "Cache R", "Cache W", "Reqs", "Cost")
	fmt.Println(header)
	fmt.Println(tableRule(header))

	var keys []string
	for k := range source {
		keys = append(keys, k)
	}
	keys = sortAndTrim(keys, func(a, b string) bool { return a > b }, topN)

	for _, key := range keys {
		var cost float64
		var reqs int

		sorted := sortedByCost(source[key])
		for _, m := range sorted {
			cost += m.bucket.Cost
			reqs += m.bucket.Requests
		}

		first := true
		for _, m := range sorted {
			b := m.bucket
			label := ""
			if first {
				label = key
			}
			fmt.Printf("  %-12s %s %7s %7s %8s %8s %6d %s\n",
				label, cyan.Sprintf("%-11s", shortModel(m.name)),
				fmtTokens(b.InputTokens), fmtTokens(b.OutputTokens),
				fmtTokens(b.CacheRead), fmtTokens(b.TotalCacheWrite()),
				b.Requests, colorCost(b.Cost, 10))
			first = false
		}
		fmt.Printf("  %-12s %-11s %7s %7s %8s %8s %6d %s\n",
			"", "", "", "", "", "", reqs, colorCost(cost, 10))
		fmt.Println()
	}
}

func printDailyBreakdown(data *ParseResult, opts OutputOptions) {
	printPeriodBreakdown("DAILY BREAKDOWN", "Date", data.DailyUsage, opts.TopN)
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
	printPeriodBreakdown("MONTHLY BREAKDOWN", "Month", aggregateMonthly(data.DailyUsage), opts.TopN)
}

// printBreakdownGroup renders one named group (a project or a branch): its
// models sorted by cost, the name wrapped across rows at wrapChunk width within
// a nameWidth column, then a SUBTOTAL row. Shared by project/branch breakdowns.
func printBreakdownGroup(name string, models map[string]*Bucket, total float64, nameWidth, wrapChunk int) {
	sorted := sortedByCost(models)

	names := wrapName(name, wrapChunk)
	for i, m := range sorted {
		n := ""
		if i < len(names) {
			n = names[i]
		}
		b := m.bucket
		fmt.Printf("  %-*s %s %7d %s\n",
			nameWidth, n, cyan.Sprintf("%-16s", shortModel(m.name)),
			b.Requests, colorCost(b.Cost, 10))
	}
	for i := len(sorted); i < len(names)-1; i++ {
		fmt.Printf("  %s\n", names[i])
	}
	subtotalName := ""
	if len(names) > len(sorted) {
		subtotalName = names[len(names)-1]
	}
	fmt.Printf("  %-*s %-16s %7s %s\n",
		nameWidth, subtotalName, "SUBTOTAL", "", colorCost(total, 10))
	fmt.Println()
}

func printProjectBreakdown(data *ParseResult, opts OutputOptions) {
	printSectionHeader("PROJECT BREAKDOWN")
	header := fmt.Sprintf("  %-35s %-16s %7s %10s",
		"Project", "Model", "Reqs", "Cost")
	fmt.Println(header)
	fmt.Println(tableRule(header))

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
	projects = sortAndTrim(projects, func(a, b projTotal) bool { return a.total > b.total }, opts.TopN)

	for _, proj := range projects {
		name := displayProject(proj.slug, data.ProjectPaths)
		printBreakdownGroup(name, data.ProjectUsage[proj.slug], proj.total, 35, 30)
	}
}

func printBranchBreakdown(data *ParseResult, opts OutputOptions) {
	printSectionHeader("BRANCH BREAKDOWN")
	header := fmt.Sprintf("  %-30s %-16s %7s %10s",
		"Branch", "Model", "Reqs", "Cost")
	fmt.Println(header)
	fmt.Println(tableRule(header))

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
		branchList = sortAndTrim(branchList, func(a, b branchTotal) bool { return a.total > b.total }, opts.TopN)

		for _, br := range branchList {
			printBreakdownGroup(br.branch, branchMap[br.branch], br.total, 30, 25)
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
	header := fmt.Sprintf("  %-28s  %12s  %10s  %8s  %10s", "Agent Type", "Invocations", "Avg Time", "Total", "Projects")
	fmt.Println(header)
	fmt.Println(tableRule(header))

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
	agents = sortAndTrim(agents, func(a, b agentEntry) bool { return a.count > b.count }, topN)

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
	colHeader := fmt.Sprintf("  %-46s  %14s  %12s", "Skill", "Invocations", "Projects")
	fmt.Println(colHeader)
	fmt.Println(tableRule(colHeader))

	var skills []toolEntry
	for name, count := range data.SkillCounts {
		skills = append(skills, toolEntry{name: name, count: count, projs: len(data.SkillProjects[name])})
	}
	skills = sortAndTrim(skills, func(a, b toolEntry) bool { return a.count > b.count }, topN)

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
	header := fmt.Sprintf("  %-38s  %12s  %9s  %11s", "Tool", "Invocations", "Errors", "Projects")
	fmt.Println(header)
	fmt.Println(tableRule(header))

	var tools []toolEntry
	for name, count := range data.ToolCounts {
		tools = append(tools, toolEntry{name, count, data.ToolErrors[name], len(data.ToolProjects[name])})
	}
	tools = sortAndTrim(tools, func(a, b toolEntry) bool { return a.count > b.count }, topN)

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
	tools = sortAndTrim(tools, func(a, b jsonToolRow) bool { return a.Invocations > b.Invocations }, topN)

	var skills []jsonSkillRow
	for name, count := range data.SkillCounts {
		skills = append(skills, jsonSkillRow{name, count, len(data.SkillProjects[name])})
	}
	skills = sortAndTrim(skills, func(a, b jsonSkillRow) bool { return a.Invocations > b.Invocations }, topN)

	var agents []jsonAgentRow
	for name, count := range data.AgentCounts {
		totalMs := data.AgentTotalTime[name].Milliseconds()
		avgMs := int64(0)
		if count > 0 && totalMs > 0 {
			avgMs = totalMs / int64(count)
		}
		agents = append(agents, jsonAgentRow{name, count, avgMs, totalMs, len(data.AgentProjects[name])})
	}
	agents = sortAndTrim(agents, func(a, b jsonAgentRow) bool { return a.Invocations > b.Invocations }, topN)

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
