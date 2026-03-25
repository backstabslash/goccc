[![CI](https://github.com/backstabslash/goccc/actions/workflows/ci.yml/badge.svg)](https://github.com/backstabslash/goccc/actions/workflows/ci.yml)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/backstabslash/goccc)](https://goreportcard.com/report/github.com/backstabslash/goccc)
[![Latest Release](https://img.shields.io/github/v/release/backstabslash/goccc?color=blue)](https://github.com/backstabslash/goccc/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A fast, zero-dependency CLI cost calculator and [statusline provider](#claude-code-statusline) for [Claude Code](https://code.claude.com/docs/en/overview) — single binary, no runtime needed.

Parses JSONL conversation logs and subagent sessions from `~/.claude/projects/`, deduplicates streaming responses, and breaks down spending by model, day, project, and branch — with accurate cache-tier and web search pricing.

![demo](https://github.com/user-attachments/assets/a65fc389-951d-47bc-9a69-5f498f3c1d32)

## Table of Contents

- [Installation](#installation)
- [Usage](#usage)
- [Claude Code Statusline](#claude-code-statusline)
- [Session Exit Hook](#session-exit-hook)
- [Configuration](#configuration)
- [Flags](#flags)
- [Preserving Log History](#preserving-log-history)

## Installation

### Homebrew (macOS / Linux)

```bash
brew install backstabslash/tap/goccc
```

### Go install

```bash
go install github.com/backstabslash/goccc@latest
```

### Pre-built binaries

Available on the [releases page](https://github.com/backstabslash/goccc/releases/latest) for macOS, Linux, and Windows (amd64 / arm64).

### From source

```bash
git clone https://github.com/backstabslash/goccc.git && cd goccc
go build -o goccc .     # macOS / Linux
go build -o goccc.exe . # Windows
```

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

> See also: [Configuration](#configuration) for threshold customization.

## Claude Code Statusline

goccc can serve as a [Claude Code statusline](https://code.claude.com/docs/en/statusline) provider — a live cost dashboard right in your terminal prompt.

```text
💸 $1.23 session · 💰 $5.67 today · 💭 45% ctx · 🔌 2 MCPs (confluence, jira) · 🔋 94% (1.5/5h) · 🤖 Opus 4.6
```

- **💸 Session cost** — parsed from the current session's JSONL files using goccc's pricing table
- **💰 Today's total** — aggregated across all sessions today (shown only when higher than session cost)
- **💭 Context %** — context window usage percentage
- **🔌 MCPs** — active MCP servers (from settings, marketplace plugins, and project config; respects per-project disables)
- **🔋 5h window** — remaining percentage of the 5-hour usage window with elapsed time (subscription users only; hidden for API billing). Emoji switches to 🪫 below 25%
- **🤖 Model** — current model

Cost and context values are color-coded yellow → red as they increase. The 5h window is color-coded in reverse — yellow below 50%, red below 25%.

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

To hide the MCP indicator, add `-no-mcp`. To hide the 5-hour usage window, add `-no-5h`.

## Session Exit Hook

goccc can show a cost summary when a Claude Code session ends — the feature users miss most since Anthropic removed it.

```text
💸 $1.87 session (14 reqs, 23m) · 💰 $12.34 today · 🤖 Opus 4.6, Haiku 4.5
```

Add to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionEnd": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "goccc -session-end"
          }
        ]
      }
    ]
  }
}
```

The hook runs within Claude Code's 1.5-second timeout. If anything fails, it exits silently — it will never break session teardown.

## Configuration

goccc reads its config from `~/.goccc.json`.

### Cost Thresholds

Cost values are color-coded yellow (warning) and red (alert) when they exceed thresholds. The defaults are $25 and $50 per day. To customize:

```json
{
  "warn_threshold": 30,
  "alert_threshold": 75
}
```

Thresholds are in USD (before currency conversion). They apply to the terminal output, statusline, and session exit hook.

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
| `-json` | | `false` | Output as JSON |
| `-no-color` | | `false` | Disable colored output (also respects `NO_COLOR` env) |
| `-base-dir` | | `~/.claude` | Base directory for Claude Code data |
| `-session-end` | | `false` | Session exit hook mode (reads SessionEnd JSON from stdin) |
| `-statusline` | | `false` | Statusline mode for Claude Code (reads session JSON from stdin) |
| `-no-mcp` | | `false` | Hide MCP servers from statusline output |
| `-no-5h` | | `false` | Hide 5-hour usage window from statusline output |
| `-currency-symbol` | | | Override currency symbol (requires `-currency-rate`) |
| `-currency-rate` | | `0` | Override exchange rate from USD (requires `-currency-symbol`) |
| `-version` | `-V` | | Print version and exit |

## Preserving Log History

Claude Code periodically deletes old log files. To keep more history for cost tracking, increase the cleanup period in `~/.claude/settings.json`:

```json
{
  "cleanupPeriodDays": 365
}
```

The default is 30 days. Set it higher to retain more data for goccc to analyze.
