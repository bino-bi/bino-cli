---
description: Show the bino dependency graph for the current project (bino graph wrapper)
argument-hint: "[--view tree|flat] [--artefact name]... [--exclude-artefact name]..."
allowed-tools: [Bash, Read]
---

# /bino-graph

Inspect manifest dependencies — which artefacts include which pages,
which pages bind which datasets, which datasets read which datasources.
Wraps `bino graph`.

```bash
bino graph $ARGUMENTS
```

## Views

- `--view tree` (default) — hierarchical tree per artefact.
- `--view flat` — flat table with content hashes (useful for diffing
  cached builds).

## Filtering

- `--artefact <name>` (repeatable) — only graph these artefacts.
- `--exclude-artefact <name>` (repeatable) — skip them.

## When to use it

- Before a build, to confirm an artefact really references the pages
  you expect.
- After a refactor, to verify a removed manifest had no callers.
- During code review, to communicate impact of a change.
- For machine-readable per-document graphs, prefer
  `bino lsp-helper graph-deps <dir> <kind>/<name>` (JSON output, see
  the **bino-data-explorer** skill).

## Empty output?

If `bino graph` reports "no ReportArtefact or DocumentArtefact found",
the project has data and pages but nothing to render. Add a
`ReportArtefact` (see the **bino-author** skill) or check that you're
running from the right project root (the directory with `bino.toml`).
