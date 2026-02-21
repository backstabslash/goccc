# goccc

CLI tool that parses Claude Code JSONL logs from `~/.claude/projects/` and calculates API usage costs by model, day, project, and branch.

## Stack

Go 1.26, stdlib + fatih/color, GoReleaser for cross-platform builds

## Structure

```text
.
├── main.go            # CLI flags and entrypoint
├── parser.go          # JSONL log walking, file parsing (parseFile), deduplication by requestId
├── pricing.go         # Per-model pricing table, cost calculation, model name resolution
├── format.go          # Terminal and JSON output formatting
├── statusline.go      # Claude Code statusline mode (reads stdin JSON, outputs formatted cost line)
├── mcp.go             # MCP server detection, per-project disable filtering, plugin walk
├── *_test.go          # Table-driven tests for each module
├── fixture_test.go    # Integration test against realistic JSONL fixture
├── testdata/          # Static JSONL fixture (multi-turn convo with subagents)
├── .goreleaser.yml    # Release config (darwin/linux/windows, amd64/arm64)
└── README.md          # Usage docs and supported models
```

## Commands

```bash
go build -o goccc .          # Build binary
go test ./...                # Run all tests
go vet ./...                 # Static analysis
go run . -days 7 -all        # Dev: run with flags directly
```

## Verification

Run before every commit:

```bash
go vet ./... && go test ./...
```

## JSONL Log Format

Claude Code stores conversation logs at `~/.claude/projects/<project-slug>/`.

### File layout

```text
<project-slug>/
  <session-uuid>.jsonl              # main conversation
  <session-uuid>/subagents/
    agent-<agentId>.jsonl           # one file per subagent
```

### Entry types

Only `type: "assistant"` entries carry `message.model` and `message.usage`. All others are skipped:
`user`, `progress`, `summary`, `queue-operation`, `file-history-snapshot`.

### Usage object (the fields that matter for cost)

```json
"usage": {
  "input_tokens": 2739,
  "output_tokens": 823,
  "cache_read_input_tokens": 23154,
  "cache_creation_input_tokens": 2125,
  "cache_creation": {
    "ephemeral_5m_input_tokens": 0,
    "ephemeral_1h_input_tokens": 2125
  }
}
```

- `output_tokens` already includes thinking tokens — there is no separate counter
- `cache_creation` sub-object breaks down 5m/1h tiers; `cache_creation_input_tokens` is the flat total (fallback when sub-object is absent in older logs)
- Extra fields (`server_tool_use`, `service_tier`, `inference_geo`, `speed`, `iterations`) are informational only

### Streaming dedup

One API call produces multiple JSONL entries sharing the same `requestId`. `input_tokens` and cache fields are identical across them; `output_tokens` grows. The last entry has the final count — our map-based dedup (overwrite) handles this correctly.

### Special entries

- `model: "<synthetic>"` + `isApiErrorMessage: true` — rate-limit/error placeholders with all-zero tokens. Filtered out to avoid inflating request counts.
- `isSidechain: true` — present on subagent entries. Informational only; we process all assistant entries regardless.

### Validated accuracy

Independently verified against a Python parser on 272 requests across 11 files (main + subagent). All token counts, dedup stats, and costs match exactly to 6 decimal places.

## Conventions

- **Flat package structure** — all code in `package main`, one concern per file (parser, pricing, format)
- **Dedup by requestId** — streaming duplicates are collapsed by keeping the last entry per `requestId` in a map
- **Pricing via prefix matching** — exact model ID lookup first, then longest-prefix match from `familyPrefixes`, then fallback to `defaultPricing`
- **Cache write tiers** — always distinguish 5-minute (`1.25x input`) and 1-hour (`2.0x input`) cache write costs separately
- **Table-driven tests** — all tests use `[]struct{ name; input; expected }` pattern with `t.Run` subtests
- **Shared file parsing** — `parseFile()` in parser.go is used by both `parseLogs` (directory walk) and `parseSession` (statusline single-session)
- **Local timezone everywhere** — cutoff uses local midnight (`time.Date` with `now.Location()`), date bucketing uses `parsed.Local().Format("2006-01-02")`. Never use `UTC()` for user-facing date logic.
- **Pre-filter before JSON parsing** — checks for both `"type":"assistant"` and `"type": "assistant"` to tolerate compact and spaced JSON formatting before full `json.Unmarshal`
- **Scanner error checking** — always check `scanner.Err()` after the scan loop to catch I/O errors and buffer overflows
- **Mtime-based file skipping** — when a day cutoff is active, files with `ModTime` before the cutoff are skipped entirely (safe because JSONL logs are append-only)
- **Directory-level project filter** — `fs.SkipDir` skips entire non-matching project directories during walk
- **Aggregate helpers on ParseResult** — `Totals()` returns a `UsageTotals` struct; `DateRange()` returns the earliest and latest dates. Both are used by format.go, statusline.go, and JSON output to avoid duplicating accumulation logic.
- **Named threshold constants** — cost color thresholds (`costThresholdRed`, `costThresholdYellow`) and context percentage thresholds (`ctxThresholdRed`, `ctxThresholdYellow`) are named constants, not magic numbers
- **Model name resolution via HasPrefix** — `shortModel()` strips the `claude-` prefix then uses `strings.HasPrefix` (not `Contains`) for precise matching
- **3-level bucket nesting for branches** — `BranchUsage` is `map[project]map[branch]map[model]*Bucket`, using `getOrCreate3LevelBucket` which reuses `getOrCreateNestedBucket` for inner levels
- **Git branch from JSONL** — `gitBranch` field on assistant entries; empty branch defaults to `"(no branch)"`
- **MCP detection is best-effort** — all MCP detection functions return nil/empty on error; statusline never fails due to missing config
- **MCP sources** — three detection paths: `mcpServers` in settings.json, marketplace `enabledPlugins` with `.mcp.json` walk, and project-level `.mcp.json` via `cwd` from transcript
- **MCP per-project disable filtering** — `~/.claude.json` stores `disabledMcpServers` and `disabledMcpjsonServers` per project path; entries use plain names (`"my-server"`) or plugin format (`"plugin:<name>:<server>"`)
- **MCP project resolution from slug** — when transcript has no `cwd` yet (fresh session), the project path is derived by matching the transcript directory slug against `~/.claude.json` project keys (slug = path with `/` replaced by `-`)
- **Single-read config files** — `settings.json` and `~/.claude.json` are each read once and their parsed data shared across detection and filtering
- **Plugin walk is layout-agnostic** — `parseEnabledPluginMCPs` walks the plugins dir for `.mcp.json` files and matches ancestor directory names to enabled plugin names, supporting any directory structure

## Don't

- Don't add new model pricing without updating both `pricingTable`, `familyPrefixes`, and `shortModel()` — prefix matching and display names depend on all three
- Don't use `log.Fatal` or `panic` — the project uses `fmt.Fprintf(os.Stderr, ...)` + `os.Exit(1)` for errors
- Don't parse timestamps with custom layouts — use `time.RFC3339` consistently, matching Claude Code's log format
- Don't collapse `CacheWrite5m` and `CacheWrite1h` into a single field — they have different pricing multipliers
- Don't use `time.Now().UTC().Truncate(24h)` for day boundaries — use `time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())` for local midnight
- Don't slice timestamp strings for date extraction (`rec.Timestamp[:10]`) — parse with `time.Parse(time.RFC3339, ...)` and convert to local
- Don't add JSON tags to `Bucket` — it's never directly marshalled; `printJSON` defines its own output structs
- Don't use `strings.Contains` for model name matching in `shortModel()` — use `strings.HasPrefix` after stripping the `claude-` prefix to avoid false substring matches
- Don't duplicate total accumulation — use `ParseResult.Totals()` instead of manually summing across `ModelUsage`
- Don't ignore `scanner.Err()` after scan loops or discard `parseFile` errors from subagents — surface them to stderr
