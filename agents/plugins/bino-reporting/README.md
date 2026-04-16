# bino-reporting

A Claude Code plugin that turns Claude — both interactive (Code) and
headless (Cowork) — into a fluent collaborator on
[bino-cli](https://github.com/bino-bi/bino-cli) PDF reports.

## What it gives you

### Skills (model-invoked)

| Skill | Activates when… |
|---|---|
| `bino-author` | You author or edit a bino report — DataSources, DataSets, LayoutPages, ReportArtefacts, components, styles. |
| `bino-data-explorer` | You ask about columns, sample rows, schema, or the dependency graph of a bino project. |
| `bino-debug` | A `bino build` or `bino lint` produces an error and you need a diagnosis. |
| `bino-build-runner` | You need to drive the build / preview / serve lifecycle, including CI-style runs. |

Skills auto-load reference docs from `skills/<name>/references/` only when
needed, keeping the context budget small.

### Slash commands

| Command | Wraps |
|---|---|
| `/bino-init` | `bino init` (scaffold a new report bundle) |
| `/bino-lint` | `bino lint` (validate manifests) |
| `/bino-build` | `bino build` (render PDFs) |
| `/bino-preview` | `bino preview` (hot-reload dev server, runs in the background) |
| `/bino-graph` | `bino graph` (dependency tree / table) |

### Subagents (for autonomous workflows)

| Subagent | Purpose |
|---|---|
| `bino-report-architect` | Given a brief, designs the manifest layout — which DataSources / DataSets / pages / artefacts to write — and emits a build plan. |
| `bino-data-analyst` | Given an existing DataSource, uses `bino lsp-helper` to introspect the data and proposes useful aggregations and chart components. |

## Installing locally

From the bino-cli repo root:

```text
/plugin marketplace add /Users/sven/Projects/bino-bi/bino-cli
/plugin install bino-reporting@bino-cli-claude-code-plugins
```

The plugin lives at `agents/plugins/bino-reporting/` and is registered
in `.claude-plugin/marketplace.json`.

## Requirements

- `bino` binary on `PATH` (run `bino setup` once to download Chrome
  headless shell).
- Project directory containing a `bino.toml` for most commands.

## Cowork / headless use

Every skill, command, and subagent in this plugin works without a browser
or display. Slash commands shell out to `bino`; skills emit text;
subagents read/write files and call `bino lsp-helper` for JSON output.
The `/bino-preview` command launches the preview server in the
background — useful for live SSE inspection in interactive Claude Code,
optional in Cowork.
