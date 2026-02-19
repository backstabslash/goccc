# goccc

CLI tool that parses Claude Code JSONL logs from `~/.claude/projects/` and calculates API usage costs by model, day, and project.

## Stack

Go 1.24, stdlib + fatih/color, GoReleaser for cross-platform builds

## Structure

```text
.
├── main.go            # CLI flags and entrypoint
├── parser.go          # JSONL log walking, deduplication by requestId
├── pricing.go         # Per-model pricing table, cost calculation, model name resolution
├── format.go          # Terminal and JSON output formatting
├── *_test.go          # Table-driven tests for each module
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

## Conventions

- **Flat package structure** — all code in `package main`, one concern per file (parser, pricing, format)
- **Dedup by requestId** — streaming duplicates are collapsed by keeping the last entry per `requestId` in a map
- **Pricing via prefix matching** — exact model ID lookup first, then longest-prefix match from `familyPrefixes`, then fallback to `defaultPricing`
- **Cache write tiers** — always distinguish 5-minute (`1.25x input`) and 1-hour (`2.0x input`) cache write costs separately
- **Table-driven tests** — all tests use `[]struct{ name; input; expected }` pattern with `t.Run` subtests

## Don't

- Don't add new model pricing without updating both `pricingTable` and `familyPrefixes` — prefix matching depends on both
- Don't use `log.Fatal` or `panic` — the project uses `fmt.Fprintf(os.Stderr, ...)` + `os.Exit(1)` for errors
- Don't parse timestamps with custom layouts — use `time.RFC3339` consistently, matching Claude Code's log format
- Don't collapse `CacheWrite5m` and `CacheWrite1h` into a single field — they have different pricing multipliers
