---
name: bino-author
description: Author bino reports (YAML manifests + SQL) and run the bino CLI correctly. Use this skill whenever the user mentions bino, bino.bi, bino.toml, `bino build / preview / init / add / lint / serve`, `apiVersion: bino.bi/v1alpha1`, or manifest kinds like DataSource, DataSet, ConnectionSecret, LayoutPage, LayoutCard, ReportArtefact, LiveReportArtefact, ScreenshotArtefact, DocumentArtefact, ChartStructure, ChartTime, Table, Tree, Grid, ScalingGroup, ComponentStyle, Internationalization, SigningProfile, Asset, Text — and also when the user is plainly doing "reporting as code" work (YAML + SQL → PDF / PNG, declarative report bundles, IBCS scenarios like ac/pp/fc/pl, pixel-perfect PDF generation) even if they don't type the word "bino" explicitly. Trigger proactively; bino's manifest surface is wide and easy to get wrong from memory, and this skill encodes the naming rules and IBCS conventions that lint failures usually come from.
---

# bino-author

`bino` is a declarative "Reporting as Code" CLI: YAML manifests + SQL queries go in, pixel-perfect PDFs (and PNGs, interactive web apps) come out. The query engine is embedded DuckDB; rendering is headless Chrome. Everything the user writes is either a YAML manifest under `apiVersion: bino.bi/v1alpha1`, a SQL/PRQL file referenced by a manifest, or `bino.toml` at the project root.

Pipeline: **manifest discovery → YAML parsing → lint/validation → DataSource collection → DataSet execution (DuckDB) → HTML rendering → PDF/PNG generation (Chrome)**.

## Golden path

Every bino project follows the same loop. When a user asks for help with bino, ground your answer in this loop unless they're asking about a specific subproblem.

```bash
bino init                      # scaffold workdir (bino.toml, data.yaml, pages.yaml, report.yaml)
cd <workdir>
bino add datasource <name>     # or edit YAML by hand
bino add dataset <name>
bino preview                   # live HTTP server at http://127.0.0.1:45678, hot reload on file change
bino build                     # validate + render PDFs into dist/
```

`bino init` generates a starter bundle with a sample CSV, one DataSet, one LayoutPage, and one ReportArtefact — the same 4 kinds you'll use on 90% of tasks.

In Claude Code, the slash commands `/bino-init`, `/bino-lint`, `/bino-build`, `/bino-preview`, `/bino-graph` wrap these for you.

## Manifest envelope

Every YAML document has the same shape. Write it once, reuse it:

```yaml
apiVersion: bino.bi/v1alpha1
kind: <Kind>                 # DataSource | DataSet | LayoutPage | ReportArtefact | ...
metadata:
  name: <identifier>
  labels: {}                 # optional, used with constraints
  description: ""            # optional
  constraints: []            # optional; scope by mode, artefact, labels
spec:
  # kind-specific fields
```

A single `.yaml` file can hold multiple manifests separated by `---`. bino discovers every YAML under the workdir that has a matching `apiVersion`/`kind` header.

## Pick the right kind

Use this routing table to decide what to write or read:

| User intent | Kind | Reference file |
|---|---|---|
| Load data from a CSV, Excel, Parquet, JSON file | `DataSource` (type: csv/excel/parquet/json) | `references/manifests-data.md` |
| Query Postgres / MySQL | `DataSource` (type: postgres_query / mysql_query) + `ConnectionSecret` | `references/manifests-data.md` |
| Transform data with SQL or PRQL | `DataSet` | `references/manifests-data.md` |
| Store a credential (DB password, S3 key, bearer token) | `ConnectionSecret` | `references/manifests-data.md` |
| Arrange components on a page | `LayoutPage` | `references/manifests-layout.md` |
| Reusable card you drop into multiple pages | `LayoutCard` | `references/manifests-layout.md` |
| Define the PDF output (pages + filename + title) | `ReportArtefact` | `references/manifests-layout.md` |
| Interactive web app with URL routes and query params | `LiveReportArtefact` | `references/manifests-layout.md` |
| Export a chart/table/card as PNG or JPEG | `ScreenshotArtefact` | `references/manifests-layout.md` |
| Markdown-driven PDF (narrative docs, manuals) | `DocumentArtefact` | `references/manifests-layout.md` |
| Register an image, font, or file by name | `Asset` | `references/manifests-layout.md` |
| Rich text / Markdown block on a page | `Text` | `references/manifests-layout.md` |
| Categorical chart — bars by segment, ranking, structure | `ChartStructure` | `references/manifests-viz.md` |
| Time-series chart — monthly trend, daily line | `ChartTime` | `references/manifests-viz.md` |
| Table with scenarios, grouping, inline variance bars | `Table` | `references/manifests-viz.md` |
| Driver tree / decomposition diagram | `Tree` | `references/manifests-viz.md` |
| Matrix layout of small charts | `Grid` | `references/manifests-viz.md` |
| Shared scaling across multiple charts/tables | `ScalingGroup` | `references/manifests-viz.md` |
| Corporate theme, fonts, colors | `ComponentStyle` | `references/styling-i18n.md` |
| Translations for report text | `Internationalization` | `references/styling-i18n.md` |
| Digitally sign the PDF | `SigningProfile` | `references/styling-i18n.md` |
| Flags, bino.toml, a specific command's behavior | — | `references/commands-reference.md` |
| `${VAR}` substitution, secrets, preview/build asymmetry | — | `references/env-substitution.md` |
| End-to-end recipe (CSV report, DB-backed report, CI/CD) | — | `references/workflows.md` |

Open the matching reference file before authoring a manifest. You'll land on a minimal example plus every available field.

## Naming rules — this is the #1 lint failure

Two different naming patterns, and conflating them produces the most common bino build error:

- **`DataSource` names must be valid SQL identifiers** because they become DuckDB table names: regex `^[a-z_][a-z0-9_]*$`, max 64 chars, no hyphens, no uppercase, no leading digits. Good: `sales_csv`, `orders_pg`, `_staging`. Bad: `sales-data`, `SalesData`, `2024_orders`.
- **Every other kind** (`DataSet`, `LayoutPage`, `ChartStructure`, `ReportArtefact`, …) allows the looser pattern `^[A-Za-z0-9_]([-A-Za-z0-9_]*[A-Za-z0-9_])?$`: letters, digits, underscores, and internal hyphens are fine. Good: `sales-summary`, `monthlyReport`, `kpi_card`.

When a user pastes an error like `invalid datasource name: sales-data`, the fix is almost always rename `sales-data` → `sales_data` (snake_case) **and** update every `FROM sales-data` / `dependencies: [sales-data]` reference to match.

Reserved prefix: `_inline_` on any kind — bino uses it for generated inline sources.

## Environment variables & secrets

Any string in any manifest supports substitution:

- `${VAR}` — replaced with `$VAR`; blank if unset.
- `${VAR:default}` — replaced with `$VAR`, or `default` if unset. **Single colon** — not bash's `:-`.
- `\${VAR}` — literal.

Behavior asymmetry (important):

- `bino preview` **warns** on unresolved vars and substitutes empty string — lets iteration continue.
- `bino build` (and `bino serve`) **fails** on unresolved vars — prevents silent corruption of production output.

Credentials never go in manifests directly. Use `ConnectionSecret` with `*FromEnv` fields:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: postgresCredentials }
spec:
  type: postgres
  postgres:
    passwordFromEnv: DB_PASSWORD   # reads $DB_PASSWORD at run time
```

Then in a `DataSource` or `DataSet`, reference the secret by name: `connection.secret: postgresCredentials`.

## Command cheatsheet

One-liners. Full flags and `bino.toml` overrides are in `references/commands-reference.md`.

| Command | Purpose |
|---|---|
| `bino init` | Scaffold new workdir + sample manifests |
| `bino add dataset <name>` | Interactive wizard to create a `DataSet` |
| `bino add datasource <name>` | Interactive wizard to create a `DataSource` |
| `bino preview [--log-sql] [--lint]` | Live HTTP preview with hot reload (port 45678) |
| `bino build [--artefact <name>] [--out-dir dist/]` | Validate + execute + render to `dist/` |
| `bino lint [--execute-queries] [--fail-on-warnings]` | Validate manifests without rendering |
| `bino graph [--view tree\|flat]` | Visualize dependency graph |
| `bino setup` | Download Chrome headless shell + template engine (one-time) |
| `bino serve --live <name>` | Production HTTP server for `LiveReportArtefact` |
| `bino cache clean [--global]` | Clear local (`.bino/cache/`) and/or global (`~/.bino/`) caches |

Notable behavior:

- `bino build` runs lint by default; disable with `--no-lint`.
- `bino preview` does **not** run lint by default; enable with `--lint`.
- `bino build` and `bino serve` both fail on missing env vars; `preview` warns and continues.
- Chrome/template engine must be installed via `bino setup` once per machine before `build`/`preview`/`serve` work.

## Common pitfalls — check these before declaring work done

1. **DataSource name isn't a SQL identifier.** Rename to snake_case; update all references.
2. **`bino setup` hasn't been run.** `build` and `preview` need the cached Chrome shell.
3. **Forgot env vars on `bino build`.** Preview works but build fails — always `export VAR=…` or set `[build.env]` in `bino.toml`.
4. **Target component has no `metadata.name`.** Required for `ScreenshotArtefact.refs` and for lint rules that reference specific components.
5. **`apiVersion` typo.** Must be exactly `bino.bi/v1alpha1` — any other value and the doc is silently skipped by discovery.
6. **Glob in `ReportArtefact.layoutPages` didn't match.** Patterns are case-sensitive and match exact page names; they work in string form only (`detail-*`), not in the object form with `params`.
7. **Query references a name that isn't in `dependencies`.** DuckDB sees the raw SQL; bino only wires up the names you list. Always add DataSource/DataSet names used in the query to `spec.dependencies`.
8. **`scenarios:` in a chart includes a scenario not in the dataset.** Chart renders with empty slots. DataSets should produce the standard columns (`ac1-4`, `pp1-4`, `fc1-4`, `pl1-4`) when used with charts — see the "Standard dataset schema" section of `references/manifests-data.md`.

## When to read which reference file

Open a reference file **before** writing YAML — don't reconstruct schemas from memory.

- **Authoring data (CSV, DB, transforms, secrets):** `references/manifests-data.md`
- **Composing a page, card, or artefact:** `references/manifests-layout.md`
- **Charts, tables, trees, grids, IBCS (scenarios/variances):** `references/manifests-viz.md`
- **Styling, themes, translations, signing:** `references/styling-i18n.md`
- **`${VAR}` rules, env secrets, preview/build asymmetry:** `references/env-substitution.md`
- **CLI flag questions, bino.toml shape, runtime env vars:** `references/commands-reference.md`
- **End-to-end recipe (CSV report, DB report, CI/CD, multi-artefact):** `references/workflows.md`

## A note on IBCS

bino follows IBCS (International Business Communication Standards). Charts expect specific column codes in the dataset: `ac1-4` (actual), `pp1-4` (prior period), `fc1-4` (forecast), `pl1-4` (plan). Variance codes on charts/tables take the form `d<B>_<A>_<pos|neg|neu>` — e.g., `dpp1_ac1_pos` means "previous minus actual, rendered as positive-good". If a user asks for "a chart comparing actual vs plan", that's `scenarios: ["ac1", "pl1"]` and optionally `variances: ["dpl1_ac1_pos"]`. Full details in `references/manifests-viz.md`.

## When to dispatch a subagent

- For a multi-step "design a whole report from scratch" brief, dispatch
  the **`bino-report-architect`** subagent. It owns the planning step
  (which datasets, which pages, which artefact) and returns a structured
  plan you then implement.
- For "what's interesting in this CSV / table?" exploration, dispatch
  **`bino-data-analyst`** — it uses `bino lsp-helper` to introspect and
  comes back with chart proposals.
