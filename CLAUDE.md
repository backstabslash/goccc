# goccc

CLI tool that parses Claude Code JSONL logs from `~/.claude/projects/` and calculates API usage costs by model, day, project, and branch.

## Stack

Go 1.26, stdlib only (zero external deps), GoReleaser for cross-platform builds

## Structure

```text
.
├── main.go            # CLI flags and entrypoint
├── parser.go          # JSONL log walking, file parsing (parseFile), deduplication by requestId
├── pricing.go         # Pricing resolution, cost calculation, model name resolution
├── pricing.json       # Externalized model pricing data (embedded + fetched from repo)
├── format.go          # Terminal and JSON output formatting
├── color.go           # ANSI color helpers (custom implementation, no external deps)
├── statusline.go      # Claude Code statusline mode (reads stdin JSON, outputs formatted cost line)
├── currency.go        # Currency config (~/.goccc.json), exchange rate fetching/caching, symbol table
├── mcp.go             # MCP server detection, per-project disable filtering, plugin walk
├── update.go          # Version update checking and remote pricing cache refresh
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
make check
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

- **Flat package structure** — all code in `package main`, one concern per file
- **Dedup by requestId** — streaming duplicates collapsed by keeping the last entry per `requestId` in a map
- **Externalized pricing** — all model pricing, family prefixes, display names, and default model live in `pricing.json`. Embedded via `//go:embed`, with a remote-cached copy fetched from the repo every 24h. `initPricing()` prefers cached over embedded. Adding a new model requires only editing `pricing.json` — no code changes or binary release needed
- **Pricing resolution** — exact model ID → longest family prefix match → `defaultPricing`
- **Cache write pricing defaults to 1h** — Claude Code JSONL logs report all cache writes as `ephemeral_5m`, but Anthropic billing matches 1-hour tier pricing (2x input). Override with `-cache-5m`. `CacheWrite5m`/`CacheWrite1h` remain separate fields (different pricing multipliers)
- **Shared file parsing** — `parseFile()` in parser.go is used by both `parseLogs` (directory walk) and `parseSession` (statusline single-session)
- **Local timezone everywhere** — local midnight for cutoffs, `parsed.Local()` for date bucketing. Never use `UTC()` for user-facing date logic
- **MCP detection is best-effort** — all MCP detection functions return nil/empty on error; statusline never fails due to missing config
- **MCP sources** — five detection paths: `mcpServers` in settings.json, marketplace `enabledPlugins` with `.mcp.json` walk, project-level `.mcp.json` via `cwd` from transcript, `settings.local.json` in project `.claude/`, and per-project `mcpServers` in `~/.claude.json`
- **Local currency** — `~/.goccc.json` stores currency code, cached rate, and timestamp; exchange rates auto-fetched and cached for 24h. `-currency-symbol` and `-currency-rate` flags override config (both required together). JSON output cost fields always in USD

## Don't

- Don't add new model pricing by editing Go code — update `pricing.json` instead (models, families, and display_names sections)
- Don't use `log.Fatal` or `panic` — use `fmt.Fprintf(os.Stderr, ...)` + `os.Exit(1)`
- Don't use UTC for day boundaries — use `time.Date(...)` with `now.Location()` for local midnight
- Don't add JSON tags to `Bucket` — it's never directly marshalled; `printJSON` defines its own output structs
