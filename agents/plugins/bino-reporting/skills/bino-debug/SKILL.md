---
name: bino-debug
description: Diagnose `bino build` and `bino lint` failures, schema validation errors, query errors, naming-rule errors, and rendering issues. Use whenever the user pastes a bino error, says "my build is failing", "the lint complains about X", "invalid datasource name", "missing required reference", "the PDF is empty / wrong", or asks why bino is rejecting a manifest. Also use when a dataset returns the wrong columns or a chart renders blank.
---

# Diagnosing bino failures

When a bino command fails, the user wants the *root cause* and a
specific fix. Avoid surface patches.

## First moves

1. **Get the full error.** Re-run the failing command (or ask the user
   to) and capture stderr + the build log. `--log-format json` on
   `bino build` / `bino lint` produces structured output that's easier
   to parse.
2. **Run `bino lsp-helper validate <project-root>`** for structured
   diagnostics with file / line / column / severity / code. This is
   strictly more informative than human-formatted lint output for
   schema problems.
3. **Sanity-check the data** with `bino lsp-helper columns` and
   `bino lsp-helper rows` (see the `bino-data-explorer` skill). A lot
   of "build failures" are really "the SQL returned no rows" or "the
   column the chart references doesn't exist".

## The single biggest category: naming-rule violations

`invalid datasource name: sales-data` is the most common bino error
and always has the same root cause:

- **DataSource names must be valid SQL identifiers** (regex
  `^[a-z_][a-z0-9_]*$`) because they become DuckDB table names.
- Hyphens, uppercase letters, and leading digits are rejected.
- **Every other kind** — DataSet, LayoutPage, ReportArtefact, etc. —
  allows the looser pattern `^[A-Za-z0-9_]([-A-Za-z0-9_]*[A-Za-z0-9_])?$`
  with internal hyphens allowed.

Fix: rename the DataSource to snake_case **and** update every
reference — the `FROM` clause of every downstream DataSet's query,
every `dependencies:` entry, any `source:` field. Forgetting a
reference produces a follow-up `missing-required-reference` error that
masks the real fix.

## Other categories of failure

### Schema validation errors

Hallmarks: error code starts with the kind (e.g. `dataset.query.required`,
`reportartefact.layoutPages.required`, `condition_then`,
`number_all_of`). Source: `internal/report/spec` JSON-Schema rules.

What to check:
- The manifest's `apiVersion` is exactly `bino.bi/v1alpha1`. Any other
  value and the doc is silently skipped — which surfaces later as
  "document not found" rather than a typo.
- `kind` matches one of the documented Kinds (no typos: `Dataset` vs.
  `DataSet`, `Reportartefact` vs. `ReportArtefact`).
- Required fields per Kind:
  - **DataSource**: `spec.type` + the type's required field
    (`path`, `connection`+`query`, or `inline`).
  - **DataSet**: exactly one of `spec.query`, `spec.prql`, `spec.source`.
  - **LayoutPage**: `spec.children` is non-empty.
  - **ReportArtefact**: `spec.filename` and `spec.title` are set;
    `spec.layoutPages` is non-empty (defaults to `["*"]`).
- `metadata.name` follows snake_case for DataSource; looser for
  everything else.

`condition_then` errors mean a JSON-Schema `if/then` rule failed —
typically a field combination is invalid (e.g. `type: csv` requires
`path` but you provided `inline.content`). The `field` in the
diagnostic tells you which path failed.

### Reference errors

Hallmarks: `missing-required-reference`, "dataset not found", "page
not found", "datasource not found".

Run `bino lsp-helper index <project>` to list all available names. Then:
- For a missing DataSet: search component `spec.dataset` references.
- For a missing LayoutPage: check `ReportArtefact.spec.layoutPages`
  (note glob patterns are supported in string form only, not the
  `{page, params}` object form).
- For a missing DataSource: check DataSet `spec.dependencies` — every
  DataSource or DataSet name used in the query must be listed there
  (this is the #1 cause of "table not found" errors at query time).

### Query / data errors

Hallmarks: "could not find column", "no such table", DuckDB error codes.

Use `bino lsp-helper columns <project> <name>` to see what the dataset
*actually* returns vs. what your component / downstream DataSet expects.
Run with `bino build --log-sql` to see every query DuckDB executes.

For database sources:
- Confirm the `ConnectionSecret`'s `*FromEnv` env var is set.
- Confirm the DB is reachable from wherever bino is running.
- Confirm the user has SELECT on the queried tables.
- Use `secret: <ConnectionSecret name>` in `connection:`, not a bare
  `password:` field.

### Env var errors

Hallmarks: "unresolved variable", build fails when preview worked.

This is *not* a bug — it's the preview/build asymmetry:

- `bino preview` warns and substitutes empty string → you can iterate
  without setting every var.
- `bino build` and `bino serve` fail fast → they won't silently emit a
  report with blank SQL or empty paths.

Fix: `export VAR=…` before the build, or put the values into
`[build.env]` in `bino.toml`.

### Render / PDF errors

Hallmarks: "chrome not found", "render timeout", empty PDF, missing chart.

- `bino setup` downloads Chrome headless shell. Run it once per machine.
  Override with `CHROME_PATH` or `--chrome-path` if you need a specific
  build.
- Empty PDF: usually a query returns zero rows. Confirm with
  `bino lsp-helper rows`.
- Missing chart: check that the dataset has the columns the component
  references. For charts, that means IBCS columns — `ac1`/`pp1`/`fc1`/
  `pl1`, `category`, `categoryIndex`, etc. See the "Standard dataset
  schema" section of `../bino-author/references/manifests-data.md`.
- Screenshot artefacts silently skip components without an inline
  `metadata.name` — always name your child components when you intend
  to reference them from a `ScreenshotArtefact`.

### Runtime limits

Hallmarks: "manifest scan limit exceeded", "row limit exceeded", "query
timeout".

| Symptom | Knob |
|---|---|
| Too many YAML files scanned | `BNR_MAX_MANIFEST_FILES` (default 500) |
| Query returns too many rows | `BNR_MAX_QUERY_ROWS` |
| Query takes too long | `BNR_MAX_QUERY_DURATION_MS` |
| Asset / CDN fetch fails | `BNR_CDN_MAX_BYTES` |

Bumping a limit to "make the error go away" is a yellow flag — first
ask whether the data should really be that large, or whether
`spec.sample` on the DataSource (scoped to `mode!=build`) is a better
answer.

### Cache staleness

If "the build doesn't reflect my change", clear the cache:

```bash
bino cache clean              # local project cache only
bino cache clean --global     # plus ~/.bino/ (Chrome shell, template engine)
```

Local-only is almost always enough. `--global` forces Chrome + template
engine re-download, which is slow.

## Useful flags when reproducing

```bash
bino build --log-sql --detailed-execution-plan --log-format json
bino lint --execute-queries --log-format json
bino lsp-helper validate ./my-report | jq '.diagnostics'
```

`--detailed-execution-plan` writes a JSON file under `dist/` describing
every step of the pipeline — invaluable for "which dataset took 30
seconds?" investigations.

## Lint rules you'll see in diagnostics

| Rule | Severity | What it catches |
|---|---|---|
| `inline-ref-bounds` | warning | `@inline(N)` index out of range |
| `dataset-source-exclusive` | warning | Using more than one of `query` / `prql` / `source` |
| `inline-naming-conflict` | warning | `metadata.name` starting with `_inline_` |
| `missing-required-reference` | **error** | A referenced DataSource/DataSet/page doesn't exist |

`--fail-on-warnings` turns every warning into a CI-failing error.

## When to give up and ask

If validation passes, queries run clean, and the PDF still looks wrong,
ask the user for:

- A screenshot or copy of the bad PDF.
- The expected vs. actual output for the failing component.
- Whether `bino preview` shows the same defect (rules out PDF-render-only
  bugs).

That triage is faster than guessing at component specs.
