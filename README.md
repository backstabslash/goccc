[![CI](https://github.com/backstabslash/goccc/actions/workflows/ci.yml/badge.svg)](https://github.com/backstabslash/goccc/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/backstabslash/goccc)](https://goreportcard.com/report/github.com/backstabslash/goccc)
[![Go Reference](https://pkg.go.dev/badge/github.com/backstabslash/goccc.svg)](https://pkg.go.dev/github.com/backstabslash/goccc)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A fast CLI cost calculator and [statusline provider](#claude-code-statusline) for [Claude Code](https://code.claude.com/docs/en/overview).

Parses JSONL logs from `~/.claude/projects/`, deduplicates streaming responses, and breaks down spending by model, day, and project — with accurate per-model pricing including separate cache write tiers.

![goccc output](https://github.com/user-attachments/assets/2f1127b9-1c53-4949-b111-2e5ef7186a7d)

## Table of Contents

- [Install](#install)
- [Usage](#usage)
- [Claude Code Statusline](#claude-code-statusline)
- [Example Output](#example-output)
- [Flags](#flags)
- [How It Works](#how-it-works)
- [Preserving Log History](#preserving-log-history)

## Install

### Go

```bash
go install github.com/backstabslash/goccc@latest
```

### From source

```bash
git clone https://github.com/backstabslash/goccc.git
cd goccc

# macOS / Linux
go build -o goccc .

# Windows
go build -o goccc.exe .
```

## Usage

```bash
# Summary of all-time usage
goccc

# Last 7 days with daily and project breakdowns
goccc -days 7 -all

# Daily breakdown only
goccc -daily

# Project breakdown only
goccc -projects

# Filter by project name (substring match)
goccc -project webapp -daily

# Today's usage
goccc -days 1

# Top 5 most expensive projects
goccc -projects -top 5

# JSON output for scripting
goccc -days 30 -all -json

# Pipe to jq for custom analysis
goccc -json | jq '.summary.total_cost'
```

## Claude Code Statusline

goccc can serve as a [Claude Code statusline](https://code.claude.com/docs/en/statusline) provider — a live cost dashboard right in your terminal prompt.

```text
💸 $1.23 session | 💰 $5.67 today | 💭 45% ctx | 🤖 Opus 4.6
```

- **💸 Session cost** — parsed from the current session's JSONL files using goccc's pricing table
- **💰 Today's total** — aggregated across all sessions today (shown only when higher than session cost)
- **💭 Context %** — context window usage percentage
- **🤖 Model** — current model

Cost and context values are color-coded: green → yellow → red as they increase.

### Setup

Add this to `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "go run github.com/backstabslash/goccc@latest -statusline"
  }
}
```

Using `go run ...@latest` ensures you always get the latest version (cached after first download). This requires Go to be installed.

## Example Output

```text
═══════════════════════════════════════════════════════════════════════════════
  Claude Code Usage Report
═══════════════════════════════════════════════════════════════════════════════
  Parsed 1430 log files, 17588 API calls (173ms)
  Period: 2026-01-22 to 2026-02-20

───────────────────────────────────────────────────────────────────────────────
  MODEL BREAKDOWN
───────────────────────────────────────────────────────────────────────────────
  Model              Input    Output   Cache R   Cache W    Reqs       Cost
  ───────────────────────────────────────────────────────────────────────────
  Opus 4.6           14.8K    305.8K     78.0M      2.4M    1157     $70.62
  Haiku 4.5          13.3K       544      3.2M    438.2K     140      $1.22
  ───────────────────────────────────────────────────────────────────────────
  TOTAL              28.1K    306.3K     81.2M      2.9M    1297     $71.83
```

## Flags

| Flag | Short | Default | Description |
| ------ | ------- | --------- | ------------- |
| `-days` | `-d` | `0` | Only show the last N calendar days (0 = all time) |
| `-project` | `-p` | | Filter by project name (substring, case-insensitive) |
| `-daily` | | `false` | Show daily breakdown |
| `-projects` | | `false` | Show per-project breakdown |
| `-all` | | `false` | Show all breakdowns (daily + projects) |
| `-top` | `-n` | `0` | Max entries in breakdowns (0 = all) |
| `-json` | | `false` | Output as JSON |
| `-no-color` | | `false` | Disable colored output (also respects `NO_COLOR` env) |
| `-base-dir` | | `~/.claude` | Base directory for Claude Code data |
| `-statusline` | | `false` | Statusline mode for Claude Code (reads session JSON from stdin) |
| `-version` | `-V` | | Print version and exit |

## How It Works

Claude Code stores conversation logs as JSONL files under `~/.claude/projects/<project-slug>/`. Each API call produces one or more log entries — streaming responses generate duplicates with the same `requestId`.

goccc:

1. Walks `.jsonl` files under the projects directory, skipping non-matching project directories and files older than the date range (by mtime)
2. Pre-filters lines with a byte scan before JSON parsing — only `"type":"assistant"` entries carry billing data (tolerates both compact and spaced JSON formatting)
3. Deduplicates streaming entries by `requestId` (last entry wins)
4. Calculates costs using [Anthropic's published pricing](https://platform.claude.com/docs/en/about-claude/pricing), including separate rates for 5-minute and 1-hour cache writes
5. Aggregates by model, date (local timezone), and project

## Preserving Log History

Claude Code periodically deletes old log files. To keep more history for cost tracking, increase the cleanup period in `~/.claude/settings.json`:

```json
{
  "cleanupPeriodDays": 365
}
```

The default is 30 days. Set it higher to retain more data for goccc to analyze.
