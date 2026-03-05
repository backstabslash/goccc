[![CI](https://github.com/backstabslash/goccc/actions/workflows/ci.yml/badge.svg)](https://github.com/backstabslash/goccc/actions/workflows/ci.yml)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/backstabslash/goccc)](https://goreportcard.com/report/github.com/backstabslash/goccc)
[![Latest Release](https://img.shields.io/github/v/release/backstabslash/goccc?color=blue)](https://github.com/backstabslash/goccc/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A fast, zero-dependency CLI cost calculator and [statusline provider](#claude-code-statusline) for [Claude Code](https://code.claude.com/docs/en/overview) — single binary, no runtime needed.

Parses JSONL logs from `~/.claude/projects/`, deduplicates streaming responses, and breaks down spending by model, day, project, and month — with accurate per-model pricing including cache write tiers, long-context premiums, and web search costs.

![goccc output](https://github.com/user-attachments/assets/e711b832-b5c2-44c3-a627-428dba350cf6)

## Table of Contents

- [Installation](#installation)
- [Usage](#usage)
- [Claude Code Statusline](#claude-code-statusline)
- [Flags](#flags)
- [How It Works](#how-it-works)
- [Preserving Log History](#preserving-log-history)

## Installation

### Homebrew (macOS / Linux)

```bash
brew install backstabslash/tap/goccc
```

### Alternatives

**Go install** (requires Go):

```bash
go install github.com/backstabslash/goccc@latest
```

**From source:**

```bash
git clone https://github.com/backstabslash/goccc.git && cd goccc
go build -o goccc .     # macOS / Linux
go build -o goccc.exe . # Windows
```

**Pre-built binaries** are available on the [releases page](https://github.com/backstabslash/goccc/releases/latest) for macOS, Linux, and Windows (amd64 / arm64).

## Usage

```bash
goccc                              # Summary of all-time usage
goccc -days 7 -all                 # Last 7 days with daily and project breakdowns
goccc -daily                       # Daily breakdown only
goccc -monthly                     # Monthly breakdown
goccc -projects                    # Project breakdown only
goccc -project webapp -daily       # Filter by project name (substring match)
goccc -days 1                      # Today's usage
goccc -projects -top 5             # Top 5 most expensive projects
goccc -cache-5m                    # Use 5-minute cache pricing instead of 1-hour
goccc -days 30 -all -json          # JSON output for scripting
goccc -json | jq '.summary.total_cost'  # Pipe to jq for custom analysis
goccc -currency-symbol "€" -currency-rate 0.92  # One-off currency override
```

### Local Currency

To display costs in your local currency, create `~/.goccc.json`:

```json
{
  "currency": "ZAR"
}
```

goccc will auto-fetch the exchange rate from USD and cache it for 24 hours. If the API is unreachable, the last cached rate is used. Set `currency` to any [ISO 4217](https://en.wikipedia.org/wiki/ISO_4217) code (e.g., `EUR`, `GBP`, `ZAR`, `JPY`).

For one-off overrides without a config file, use both flags together:

```bash
goccc -currency-symbol "€" -currency-rate 0.92
```

JSON output always reports costs in USD for backward compatibility, with a `currency` metadata object when a non-USD currency is active.

## Claude Code Statusline

goccc can serve as a [Claude Code statusline](https://code.claude.com/docs/en/statusline) provider — a live cost dashboard right in your terminal prompt.

```text
💸 $1.23 session · 💰 $5.67 today · 💭 45% ctx · 🔌 2 MCPs (confluence, jira) · 🤖 Opus 4.6
```

- **💸 Session cost** — parsed from the current session's JSONL files using goccc's pricing table
- **💰 Today's total** — aggregated across all sessions today (shown only when higher than session cost)
- **💭 Context %** — context window usage percentage
- **🔌 MCPs** — active MCP servers (from settings, marketplace plugins, and project config; respects per-project disables)
- **🤖 Model** — current model

Cost and context values are color-coded yellow → red as they increase.

### Setup

Add to `~/.claude/settings.json`:

**Using Homebrew** (recommended — fast, no runtime needed):

```json
{
  "statusLine": {
    "type": "command",
    "command": "goccc -statusline"
  }
}
```

**Using Go** (requires Go installed; binary is cached after first download):

```json
{
  "statusLine": {
    "type": "command",
    "command": "go run github.com/backstabslash/goccc@latest -statusline"
  }
}
```

To hide the MCP indicator, add `-no-mcp`.

## Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `-days` | `-d` | `0` | Only show the last N calendar days (0 = all time) |
| `-project` | `-p` | | Filter by project name (substring, case-insensitive) |
| `-daily` | | `false` | Show daily breakdown |
| `-monthly` | `-m` | `false` | Show monthly breakdown (mutually exclusive with `-daily`) |
| `-projects` | | `false` | Show per-project breakdown |
| `-all` | | `false` | Show all breakdowns (daily + projects) |
| `-top` | `-n` | `0` | Max entries in breakdowns (0 = all) |
| `-cache-5m` | | `false` | Price cache writes at 5-minute tier (1.25x input) instead of 1-hour (2x) |
| `-json` | | `false` | Output as JSON |
| `-no-color` | | `false` | Disable colored output (also respects `NO_COLOR` env) |
| `-base-dir` | | `~/.claude` | Base directory for Claude Code data |
| `-statusline` | | `false` | Statusline mode for Claude Code (reads session JSON from stdin) |
| `-no-mcp` | | `false` | Hide MCP servers from statusline output |
| `-currency-symbol` | | | Override currency symbol (requires `-currency-rate`) |
| `-currency-rate` | | `0` | Override exchange rate from USD (requires `-currency-symbol`) |
| `-version` | `-V` | | Print version and exit |

## How It Works

Claude Code stores conversation logs as JSONL files under `~/.claude/projects/<project-slug>/`. Each API call produces one or more log entries — streaming responses generate duplicates with the same `requestId`.

goccc:

1. Walks `.jsonl` files under the projects directory, skipping non-matching project directories and files older than the date range (by mtime)
2. Pre-filters lines with a byte scan before JSON parsing — only `"type":"assistant"` entries carry billing data (tolerates both compact and spaced JSON formatting)
3. Deduplicates streaming entries by `requestId` (last entry wins)
4. Calculates costs using [Anthropic's published pricing](https://platform.claude.com/docs/en/about-claude/pricing), including cache write tiers, long-context premiums (>200K input tokens), and web search costs
5. Aggregates by model, date (local timezone), project, and month

### Cache Write Pricing

Claude Code JSONL logs report all cache writes as `ephemeral_5m`, but Anthropic billing matches 1-hour tier pricing (2x input price). goccc defaults to 1-hour pricing to align with actual bills. Use `-cache-5m` to switch to 5-minute pricing (1.25x input) if your billing differs.

## Preserving Log History

Claude Code periodically deletes old log files. To keep more history for cost tracking, increase the cleanup period in `~/.claude/settings.json`:

```json
{
  "cleanupPeriodDays": 365
}
```

The default is 30 days. Set it higher to retain more data for goccc to analyze.
