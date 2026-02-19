# goccc

[![CI](https://github.com/backstabslash/goccc/actions/workflows/ci.yml/badge.svg)](https://github.com/backstabslash/goccc/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A fast CLI tool that parses [Claude Code](https://code.claude.com/docs/en/overview) log files and calculates your API usage costs.

Reads JSONL logs from `~/.claude/projects/`, deduplicates streaming responses, and breaks down spending by model, day, and project.

## Install

### Homebrew

```bash
brew install backstabslash/tap/goccc
```

### Go

```bash
go install github.com/backstabslash/goccc@latest
```

### From source

```bash
git clone https://github.com/backstabslash/goccc.git
cd goccc
go build -o goccc .
```

## Usage

```bash
# Summary of all-time usage
goccc

# Last 7 days with daily and project breakdowns
goccc -days 7 -all

# Filter by project name
goccc -project myapp -daily

# JSON output for scripting
goccc -days 30 -all -json

# Pipe to jq for custom analysis
goccc -json | jq '.summary.total_cost'
```

## Flags

| Flag | Short | Default | Description |
| ------ | ------- | --------- | ------------- |
| `-days` | `-d` | `0` | Only show the last N calendar days (0 = all time) |
| `-project` | `-p` | | Filter by project name (substring, case-insensitive) |
| `-daily` | | `false` | Show daily breakdown |
| `-projects` | | `false` | Show per-project breakdown |
| `-all` | | `false` | Show all breakdowns (daily + projects) |
| `-top` | `-n` | `15` | Max entries in breakdowns |
| `-json` | | `false` | Output as JSON |
| `-no-color` | | `false` | Disable colored output (also respects `NO_COLOR` env) |
| `-base-dir` | | `~/.claude` | Base directory for Claude Code data |
| `-version` | | | Print version and exit |

## Example Output

```text
═══════════════════════════════════════════════════════════════════════════════
  Claude Code Usage Report
═══════════════════════════════════════════════════════════════════════════════
  Parsed 1380 log files, 935 API calls (1317 streaming duplicates removed)

───────────────────────────────────────────────────────────────────────────────
  MODEL BREAKDOWN
───────────────────────────────────────────────────────────────────────────────
  Model              Input    Output   Cache R   Cache W    Reqs       Cost
  ───────────────────────────────────────────────────────────────────────────
  Opus 4.6           24.9K    214.7K     43.2M      5.1M     842     $58.89
  Haiku 4.5           2.8K       168      2.3M    388.2K      80    $0.7225
  Sonnet 4.6            13        61    206.6K     27.8K      11    $0.1670
  ───────────────────────────────────────────────────────────────────────────
  TOTAL              27.8K    214.9K     45.8M      5.5M     935     $59.82
```

## How It Works

Claude Code stores conversation logs as JSONL files under `~/.claude/projects/<project-slug>/`. Each API call produces one or more log entries — streaming responses generate duplicates with the same `requestId`.

goccc:

1. Walks all `.jsonl` files under the projects directory
2. Filters by project name and date range
3. Deduplicates streaming entries by `requestId` (last entry wins)
4. Calculates costs using [Anthropic's published pricing](https://platform.claude.com/docs/en/about-claude/pricing), including separate rates for 5-minute and 1-hour cache writes
5. Aggregates by model, date, and project

## Preserving Log History

Claude Code periodically deletes old log files. To keep more history for cost tracking, increase the cleanup period in `~/.claude/settings.json`:

```json
{
  "cleanupPeriodDays": 365
}
```

The default is 30 days. Set it higher to retain more data for goccc to analyze.
