[![CI](https://github.com/backstabslash/goccc/actions/workflows/ci.yml/badge.svg)](https://github.com/backstabslash/goccc/actions/workflows/ci.yml)
[![Zero Dependencies](https://img.shields.io/badge/dependencies-0-brightgreen)](go.mod)
[![Go Report Card](https://goreportcard.com/badge/github.com/backstabslash/goccc)](https://goreportcard.com/report/github.com/backstabslash/goccc)
[![Latest Release](https://img.shields.io/github/v/release/backstabslash/goccc?color=blue)](https://github.com/backstabslash/goccc/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A fast, zero-dependency CLI cost calculator, [tool analytics](#tool--skill-analytics), and [customizable statusline](#claude-code-statusline) for [Claude Code](https://code.claude.com/docs/en/overview). Breakdowns by model, day, project, and branch. Single binary, no runtime needed.

![demo](https://github.com/user-attachments/assets/a65fc389-951d-47bc-9a69-5f498f3c1d32)

## Table of Contents

- [Installation](#installation)
- [Usage](#usage)
- [Claude Code Statusline](#claude-code-statusline)
- [Session Exit Hook](#session-exit-hook)
- [Tool & Skill Analytics](#tool--skill-analytics)
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
goccc -tools -days 30             # Tool & skill usage analytics
goccc -tools -project myapp -json # Tool analytics filtered by project, as JSON
```

## Claude Code Statusline

goccc can serve as a [Claude Code statusline](https://code.claude.com/docs/en/statusline) — a fully customizable, live cost dashboard right in your terminal prompt.

```text
💸 $1.23 session · 💰 $5.67 today · 💭 45% ctx · 🔋 94% (1.5/5h) · 🤖 Opus 4.6
```

- **💸 Session cost** — parsed from the current session's JSONL files using goccc's pricing table
- **💰 Today's total** — aggregated across all sessions today (shown only when higher than session cost)
- **💭 Context %** — context window usage percentage
- **🔋 5h / 7d window** — remaining percentage of the usage window with elapsed time (subscription users only; hidden for API billing). Emoji switches to 🪫 below 25%
- **🤖 Model** — current model

The `mcp` segment (active MCP servers) is available but off by default — add it to `segments` to enable it.

Values are color-coded: cost and context turn yellow → red as they increase; rate limit windows are inverted — yellow below 50%, red below 25% remaining.

### Setup

Add to `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "goccc -statusline"
  }
}
```

Works with any [install method](#installation). To run without installing: `go run github.com/backstabslash/goccc@latest -statusline`.

### Customization

The statusline is fully customizable via `~/.goccc.json`. With no config, you get the default layout shown above.

```json
{
  "statusline": {
    "segments": ["session_cost", "today_cost", "ctx", "model", "|", "5h", "cwd", "branch"],
    "separator": " · ",
    "segment_options": {
      "session_cost": { "emoji": "🤑", "label": "sess" },
      "today_cost": { "label": "day" },
      "ctx": { "emoji": "🧠", "label": "context" },
      "5h": { "emoji": "⏳" },
      "branch": { "emoji": "🔀" }
    }
  }
}
```

**`segments`** — ordered list of segments to display. Only listed segments are shown; segments with no data auto-hide. `"|"` forces a line break.

The config above produces:

```text
🤑 $1.23 sess · 💰 $5.67 day · 🧠 45% context · 🤖 Opus 4.6
⏳ 94% (1.5/5h) · 📁 my-project · 🔀 feature/auth
```

Available segments:

| Segment | Default | Auto-hides when | Overrides |
| --- | --- | --- | --- |
| `session_cost` | `💸 $X.XX session` | cost is $0 | emoji, label |
| `today_cost` | `💰 $X.XX today` | cost is $0 | emoji, label |
| `ctx` | `💭 XX% ctx` | — | emoji, label |
| `model` | `🤖 Model Name` | — | emoji |
| `mcp` | `🔌 N MCPs (...)` | no MCPs detected | emoji, label |
| `branch` | `🌿 branch-name` | no branch | emoji |
| `5h` | `🔋 XX% (X/5h)` | absent (API billing) | emoji |
| `7d` | `🔋 XX% (X/7d)` | absent (API billing) | emoji |
| `tokens` | `📊 XK in / XK out` | both zero | emoji |
| `lines` | `📝 +N -N` | both zero | emoji |
| `duration` | `⏱️ Xm` | zero | emoji |
| `cwd` | `📁 dirname` | empty | emoji |
| `version` | `🏷️ X.Y.Z` | empty | emoji |

**`separator`** — string between segments (default: `" · "`).

**`segment_options`** — per-segment overrides. `emoji` replaces the default icon (for `5h`/`7d`, replaces the dynamic 🔋/🪫). `label` replaces trailing text (only on segments marked above).

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

## Tool & Skill Analytics

The `-tools` flag shows how often each tool and skill was invoked across your sessions — useful for understanding your workflow patterns, auditing MCP tool usage, and spotting unused skills.

```bash
goccc -tools                    # All-time tool usage
goccc -tools -days 7            # Last 7 days
goccc -tools -project myapp     # Filter by project
goccc -tools -top 5             # Top 5 in each breakdown
goccc -tools -json              # JSON output for scripting
```

```text
────────────────────────────────────────────────────────────────────────────────
  TOOL BREAKDOWN (3,960 total, 24 unique, 82 sessions)
────────────────────────────────────────────────────────────────────────────────
  Tool                                     Invocations     Errors     Projects
  ────────────────────────────────────────────────────────────────────────────
  Bash                                           1,373       5.1%           12
  Read                                           1,231       4.3%           13
  Edit                                             459       2.4%            9
  ...

────────────────────────────────────────────────────────────────────────────────
  AGENT BREAKDOWN (83 spawned, 5 unique, 33 sessions)
────────────────────────────────────────────────────────────────────────────────
  Agent Type                     Invocations    Avg Time     Total    Projects
  ────────────────────────────────────────────────────────────────────────────
  Explore                                 46      2m 39s     2h 2m           8
  general-purpose                         29       4m 8s        2h           6
  ...

────────────────────────────────────────────────────────────────────────────────
  SKILL BREAKDOWN (32 invocations, 13 unique, 24 sessions, 46 available)
────────────────────────────────────────────────────────────────────────────────
  Skill                                              Invocations      Projects
  ────────────────────────────────────────────────────────────────────────────
  superpowers:brainstorming                                   14             7
  review-diff                                                  1             1
  ...

  ⚠ 37 unused skills (0 invocations):
    update-config, create-prd, ...
```

Supports all the same filtering flags (`-days`, `-project`) and output modes (`-json`) as the cost report.

## Configuration

All configuration lives in `~/.goccc.json`. Every field is optional.

| Key | Description |
| --- | --- |
| `currency` | [ISO 4217](https://en.wikipedia.org/wiki/ISO_4217) currency code (e.g. `EUR`, `GBP`, `JPY`). Rate auto-fetched and cached 24h |
| `warn_threshold` | Yellow color-coding threshold (default: `$25`, auto-scales with currency; custom values used as-is) |
| `alert_threshold` | Red color-coding threshold (default: `$50`, auto-scales with currency; custom values used as-is) |
| `statusline` | [Statusline customization](#customization) — segments, separator, per-segment overrides |

### Local Currency

Set `"currency": "EUR"` (or any ISO 4217 code) in `~/.goccc.json`. goccc auto-fetches the exchange rate from USD and caches it for 24 hours. If the API is unreachable, the last cached rate is used. For one-off overrides without a config file, use `-currency-symbol "€" -currency-rate 0.92` together.

JSON output always reports costs in USD, with a `currency` metadata object when a non-USD currency is active.

## Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `-days` | `-d` | `0` | Only show the last N calendar days (0 = all time) |
| `-project` | `-p` | — | Filter by project name (substring, case-insensitive) |
| `-daily` | — | `false` | Show daily breakdown |
| `-monthly` | `-m` | `false` | Show monthly breakdown (mutually exclusive with `-daily`) |
| `-projects` | — | `false` | Show per-project breakdown |
| `-all` | — | `false` | Show all breakdowns (daily + projects) |
| `-top` | `-n` | `0` | Max entries in breakdowns (0 = all) |
| `-tools` | — | `false` | Show tool and skill usage analytics |
| `-json` | — | `false` | Output as JSON |
| `-no-color` | — | `false` | Disable colored output (also respects `NO_COLOR` env) |
| `-base-dir` | — | `~/.claude` | Base directory for Claude Code data |
| `-utc` | — | `false` | Bucket days by UTC instead of local time (matches Anthropic API reporting) |
| `-session-end` | — | `false` | Session exit hook mode (reads SessionEnd JSON from stdin) |
| `-statusline` | — | `false` | Statusline mode for Claude Code (reads session JSON from stdin) |
| `-currency-symbol` | — | — | Override currency symbol (requires `-currency-rate`) |
| `-currency-rate` | — | `0` | Override exchange rate from USD (requires `-currency-symbol`) |
| `-version` | `-V` | — | Print version and exit |

## Preserving Log History

Claude Code periodically deletes old log files. To keep more history for cost tracking, increase the cleanup period in `~/.claude/settings.json`:

```json
{
  "cleanupPeriodDays": 365
}
```

The default is 30 days. Set it higher to retain more data for goccc to analyze.
