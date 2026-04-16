---
name: bino-data-explorer
description: Introspect a bino project's data using `bino lsp-helper`. Use when the user asks "what columns does X have?", "show me sample rows of Y", "list all manifests", "show the dependency graph", "what does this dataset return?", or any time you need to verify a column name, sample some data, or understand the dependency structure before authoring or debugging.
---

# Exploring data inside a bino project

`bino lsp-helper` is the agent-friendly introspection surface — every
subcommand emits JSON, parsed reliably without screen-scraping. Use it
to sanity-check column names, peek at sample rows, list every document
in a project, or trace dependencies before editing.

All commands take a project root or workdir as the first argument. The
project root is the directory containing `bino.toml` (lsp-helper resolves
upward from the given path, so a subdirectory works too).

## Commands

### `bino lsp-helper index <dir>`

List every manifest in the project.

```bash
bino lsp-helper index ./my-report
```

Returns:
```json
{
  "documents": [
    {"kind": "DataSource", "name": "revenue_data", "file": "data.yaml", "position": 1},
    {"kind": "DataSet", "name": "revenue_by_region", "file": "datasets.yaml", "position": 1},
    {"kind": "LayoutPage", "name": "sales-dashboard-page", "file": "pages.yaml", "position": 1},
    {"kind": "ReportArtefact", "name": "sales-dashboard", "file": "report.yaml", "position": 1}
  ]
}
```

`position` is 1-based document index inside multi-doc YAML files.

### `bino lsp-helper columns <dir> <name>`

Get column names from a DataSource or DataSet. Executes the underlying
query (cheap on inline / cached data, can be expensive on remote DBs).

```bash
bino lsp-helper columns ./my-report revenue_by_region
```

Returns:
```json
{ "name": "revenue_by_region", "columns": ["region", "ac1", "pp1"] }
```

Use this *before* writing a component spec to make sure the columns you
reference (e.g. `level: region`, `scenarios: [ac1, pp1]`) actually exist.

### `bino lsp-helper rows <dir> <name> [--limit N]`

Sample rows from a DataSource or DataSet. Default limit is 10.

```bash
bino lsp-helper rows ./my-report revenue_by_region --limit 5
```

Returns:
```json
{
  "name": "revenue_by_region",
  "kind": "DataSet",
  "columns": ["region", "ac1", "pp1"],
  "rows": [
    {"region": "DACH", "ac1": 4250, "pp1": 3800},
    {"region": "Nordics", "ac1": 2870, "pp1": 2650}
  ],
  "limit": 5,
  "truncated": true
}
```

`truncated: true` means the underlying query produced more rows than the
limit. Use this to understand the *shape* of a dataset (numeric vs.
categorical columns, value ranges, etc.) before designing visualizations.

### `bino lsp-helper validate <dir>`

Run schema validation on every manifest, return diagnostics with file +
line + column. This is what an editor-side LSP would surface as red
underlines.

```bash
bino lsp-helper validate ./my-report
```

Returns:
```json
{
  "valid": false,
  "diagnostics": [
    {
      "file": "datasets.yaml",
      "position": 2,
      "line": 14,
      "column": 5,
      "severity": "error",
      "message": "spec.query: must be specified when source is omitted",
      "code": "dataset.query.required",
      "field": "spec.query"
    }
  ]
}
```

Prefer this over `bino lint` when you want machine-readable output. Use
`bino lint` (or `/bino-lint`) when you want the human-friendly colored
view.

### `bino lsp-helper graph-deps <dir> <document-id> [--direction in|out|both]`

Dependency graph for a single document. `direction`:

- `out` — what does this document depend on? (default)
- `in` — what depends on this document?
- `both` — both directions

```bash
bino lsp-helper graph-deps ./my-report DataSet/revenue_by_region --direction both
```

Returns nodes + edges, where each node has `id`, `kind`, `name`, `file`,
`hash`, and edges describe the direction (`out` = root→dependency,
`in` = dependent→root).

Use this to answer "what breaks if I remove X?" or "what feeds into the
sales-dashboard report?".

## Workflow

1. **Find the project root.** Look for `bino.toml`. If you're running
   from a subdirectory, lsp-helper resolves upward, but be explicit
   when possible.
2. **Index first** if you don't know what's in the project. The output
   tells you every Kind/name available.
3. **Columns + rows** to understand a single dataset. Always check
   columns *before* recommending or writing component specs that
   reference them.
4. **Validate** when you suspect a manifest issue and want structured
   diagnostics. For human output, hand off to the `bino-debug` skill.
5. **graph-deps** when investigating impact, refactoring, or explaining
   to the user how a report fits together.

## Tips

- Output is always JSON. Pipe through `jq` for filtering — e.g.
  `bino lsp-helper index ./my-report | jq '.documents[] | select(.kind=="DataSource")'`.
- `lsp-helper` is exposed as a hidden command but is fully supported and
  stable for tooling.
- Connection-backed DataSources (postgres / mysql) need their secrets
  in the env at lsp-helper time — same as `bino build`.
- For large databases, consider adding a `spec.sample` clause on the
  DataSource so `columns` / `rows` stay fast.
- The `bino-data-analyst` subagent wraps these commands into an
  end-to-end "describe this data + propose visualizations" workflow.
  Dispatch it for substantial exploration tasks.
