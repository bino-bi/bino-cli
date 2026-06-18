# bino-report — the bino Claude Code plugin

Co-author and autopilot **pixel-perfect IBCS PDF reports** with Claude, on top of
[bino](https://github.com/bino-bi/bino-cli) — *Report-as-Code*: declarative YAML + SQL that an agent
can author end-to-end, with bino's own schema and validation as the guardrails.

The plugin packages the `bino mcp` server (zero-config registration) plus the IBCS knowledge,
slash commands, and an autopilot pipeline that the raw MCP doesn't carry.

## Prerequisite

The plugin drives the `bino` binary on your `PATH`. You need a build new enough to have the **`mcp`
subcommand** — check with `bino version`, and confirm `bino mcp --help` works. The plugin detects a
missing or too-old binary and tells you; it can't install bino for you. See the
[install guide](https://github.com/bino-bi/bino-cli).

## Install

```bash
claude plugin marketplace add bino-bi/bino-cli
/plugin install bino
```

Installing registers the `bino` MCP server automatically (no `claude mcp add` step). Open `/mcp` to
confirm the `bino` server is connected; `/help` lists the `/bino:*` commands.

## Two modes, one pipeline

### Mode A — Co-authoring (you drive, Claude assists)

Claude follows bino's disciplined loop — read the live schema → learn a dataset's columns →
draft → `validate_draft` → write → `validate_project` → `build` — and applies IBCS semantics the
schema can't carry (scenarios, variances, component choice, narrative).

| Command | What it does |
| --- | --- |
| `/bino:report` | End-to-end: your data + a goal → a built report PDF, step by step. |
| `/bino:new` | Scaffold a fresh report bundle (`init_bundle`). |
| `/bino:add-source` | Probe a CSV / Excel / database source, then scaffold a typed `DataSource` (+ `DataSet`). |
| `/bino:fix` | Validate the project and walk the diagnostics to green. |
| `/bino:build` | Render the report artefacts to PDF. |

Two skills load automatically when relevant: **`bino-authoring`** (the draft→validate→write
discipline) and **`bino-ibcs`** (the IBCS rubric).

### Mode B — Autopilot (you delegate, Claude runs the pipeline)

```
/bino:autopilot <data-or-source> "<goal>"
```

A **main-thread orchestrator** delegates **requirements → data → authoring → validation → build** to
three headless subagents (`bino-data`, `bino-author`, `bino-validation`), pausing for you only at
well-chosen gates. **Tier 1 (checkpointed)** is the ceiling today.

You are asked to confirm at four checkpoints:

1. **the report brief** (after requirements),
2. **the data plan** (after sources + datasets),
3. **before the build** (rendering is slow and writes files), and
4. **the finished PDF** — a mandatory visual sign-off, every run.

## Safety

Autopilot holds hard gates at every tier, because authoring reports naïvely is dangerous:

- **Credentialed sources** (databases / S3 / WebDAV) are a hard human stop. The agent writes only the
  `*FromEnv` skeleton — **never an inline secret** — and never spends a credential unattended.
- **Running a data validation executes the agent's SQL** (DuckDB SQL is not read-only), so it runs
  **once**, only on datasets authored this run, never unattended against a credentialed source.
- **A non-empty / pre-existing project** drops autopilot to confirmed writes — bino has no rollback
  yet, so it never clobbers your hand-written manifests silently.
- **Every build is gated and iteration-capped**, and the finished PDF **always** needs your eyes:
  "validates + builds" is *not* the same as "correct."

`PASS` from autopilot means *mechanically correct and ready for sign-off* — never "done."

## How it relates to `bino mcp`

This plugin **is** the batteries-included way to use the
[`bino mcp`](https://github.com/bino-bi/bino-cli) surface from Claude Code. Other MCP clients
(Claude Desktop, Cursor) can register `bino mcp` directly, but they don't get these skills, commands,
or the autopilot.

## License

AGPL-3.0-or-later.
