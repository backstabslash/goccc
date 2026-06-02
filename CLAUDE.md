# goccc

CLI tool that parses Claude Code JSONL logs from `~/.claude/projects/` and calculates API usage costs by model, day, project, and branch.

## Stack

Go 1.26, stdlib only (zero external deps), GoReleaser for cross-platform builds

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

Claude Code stores logs at `~/.claude/projects/<project-slug>/`. Sessions are `<uuid>.jsonl` with subagents in `<uuid>/subagents/agent-<id>.jsonl`. Workflow-spawned agents nest deeper, under `<uuid>/subagents/workflows/wf_*/agent-<id>.jsonl`.

- Only `type: "assistant"` entries carry `message.model` and `message.usage` — skip all others
- `output_tokens` already includes thinking tokens — no separate counter
- `cache_creation` sub-object breaks down 5m/1h tiers; `cache_creation_input_tokens` is the flat total (fallback for older logs)
- One API call produces multiple entries sharing the same `requestId` — dedup by keeping the last (highest `output_tokens`)
- `model: "<synthetic>"` + `isApiErrorMessage: true` are rate-limit placeholders — filter out to avoid inflating counts

## Conventions

- **Flat package** — all code in `package main`, one concern per file
- **Externalized pricing** — all pricing lives in `pricing.json` (embedded via `//go:embed`, remote-cached 24h). Adding a model or adjusting pricing = edit `pricing.json` only, no code changes. Cache fields are optional — `fillCacheDefaults()` derives from input price. Fast mode pricing lives in `fast_models` map — same structure as `models`, resolved when `usage.speed == "fast"`
- **Pricing resolution** — exact model ID → longest family prefix → `defaultPricing`
- **Fast mode bucketing** — parser appends `:fast` suffix to model key when `speed == "fast"`, creating separate buckets. `shortModel()` and `resolvePricing()` strip the suffix for display/lookup
- **Local timezone everywhere** — local midnight for cutoffs, `parsed.Local()` for date bucketing. Never `UTC()` for user-facing dates
- **MCP detection is best-effort** — returns nil/empty on error; statusline never fails due to missing config
- **Config** — `~/.goccc.json` stores currency, thresholds, and statusline config. `initConfig()` loads once. JSON output costs always in USD
- **Session end hook** — writes to `/dev/tty` (bypasses Claude Code's stderr capture), falls back to stderr on Windows. Silently exits on any error
- **Statusline segments** — registry in `statusline_config.go`. Segments with no data auto-hide. `"|"` forces line break

## Don't

- Don't change pricing in Go code — edit `pricing.json` (models, fast_models, families, display_names, long_context_threshold, web_search_cost)
- Don't use `log.Fatal` or `panic` — use `fmt.Fprintf(os.Stderr, ...)` + `os.Exit(1)`
- Don't use UTC for day boundaries — use `time.Date(...)` with `now.Location()` for local midnight
- Don't add JSON tags to `Bucket` — it's never directly marshalled; `printJSON` defines its own output structs
