---
name: bino-concepts
description: bino's mental model in one read — the manifest envelope, the DataSource → DataSet →
  LayoutPage → artefact pipeline, why metadata.name is the SQL table name, dependencies, the standard
  dataset columns, ref vs inline children, LayoutPage params, constraints, inline datasets, ${VAR}
  substitution, bino.toml, and what validate_draft / validate_project / build each catch. Read before
  authoring any bino manifest, and whenever a diagnostic or a brief mentions dependencies, a scenario
  column, ref, params, a constraint, an inline dataset, an unresolved variable, or bino.toml.
---

# How bino thinks

bino is **Report-as-Code**: a report is a directory of YAML manifests plus SQL, rendered by the `bino`
CLI to an IBCS-style PDF. This skill names the concepts; `bino-authoring` is the loop that uses them,
`bino-ibcs` says what the scenario and variance columns *mean*.

## The envelope

Every manifest is one YAML document with four top-level keys: `apiVersion: bino.bi/v1alpha1`, `kind`,
`metadata` and `spec`. `metadata` carries `name` (the identity), optional `description` and `labels`,
and — where relevant — `constraints` and `params`. `spec` is what `kind` decides. One file may hold
many documents separated by `---`; that is normal, not a smell.
Docs: https://cli.bino.bi/concepts/workdir-and-manifests/

## The pipeline

Four stages, each a kind, each referencing the previous one by name:

1. **DataSource** — raw input: `csv`, `excel`, `parquet`, `inline`, `postgres_query`, `mysql_query`.
2. **DataSet** — a typed table computed from sources. Exactly one of `spec.query` (DuckDB SQL),
   `spec.prql`, or `spec.source` (pass-through of one DataSource, no transformation).
3. **LayoutPage** — `spec.children` arranges the visual components: `Table`, `Text`, `ChartTime`,
   `ChartStructure`, `ChartScatter`, `ChartBubble`, `ChartBullet`, `Tree`, `Grid`, `LayoutCard`,
   `Image`. `LayoutCard` and `Grid` nest further children.
4. **Artefact** — `ReportArtefact` (PDF from pages), `LiveReportArtefact` (interactive web app),
   `DocumentArtefact` (PDF from Markdown), `ScreenshotArtefact` (PNG/JPEG of single components).

Wire it leaves-first so every reference resolves when you write it.
Docs: https://cli.bino.bi/getting-started/key-ideas/

## metadata.name is the table name

`metadata.name` is how documents reference each other. For `DataSource` and `DataSet` it is also the
DuckDB table (view) name the query engine registers, so it must be a SQL identifier: snake_case
matching `^[a-z_][a-z0-9_]*$`. SQL always references these names — `FROM sales_csv` — never a file
path or a file name. Do not start a name with `_inline_`; bino reserves that prefix.
Docs: https://cli.bino.bi/concepts/data-model/

## dependencies

A `DataSet` lists in `spec.dependencies` every DataSource or DataSet its SQL uses in `FROM` / `JOIN`.
bino builds the dependency graph from these lists and registers only the listed tables for that query,
so a table you forgot to list is "not found" even though it exists in the project. With `spec.source`
the single dependency is inferred. `graph_deps` shows the resolved graph.
Docs: https://cli.bino.bi/reference/dataset/

## The standard dataset columns

Components expect a DataSet with these column names (this section says what they are *called*;
`bino-ibcs` says what they mean and how to choose them):

- **Scenarios**, four families with four slots each: `ac1`..`ac4` (actual), `pp1`..`pp4` (previous
  period), `fc1`..`fc4` (forecast), `pl1`..`pl4` (plan). Numeric, nullable.
- **Grouping**: `rowGroup`, `category`, `subCategory`, `columnGroup` — each with a matching
  `rowGroupIndex`, `categoryIndex`, `subCategoryIndex`, `columnGroupIndex` that fixes the order.
- **`date`** — ISO-8601 string; `ChartTime` requires it.
- **`operation`** — `'+'` (default) or `'-'`; the sign a row contributes in aggregations.
- **Variance tokens** in component specs follow `d<B>_<A>_[pos|neg|neu]` for absolute and `dr<B>_<A>_…`
  for relative differences, e.g. `dac1_pl1_pos`, `drac1_pp1_neg`.

Docs: https://cli.bino.bi/reference/dataset/#standard-dataset-schema

## Layout children: ref or inline, and params

A child of `LayoutPage` / `LayoutCard` / `Grid` is either **`kind` + `ref`** — a standalone document
referenced by its `metadata.name`; any `spec` given alongside overrides the referenced spec (objects
deep-merge, arrays replace) — or **`kind` + `spec`** — defined inline. `Text`, `Table`, the charts,
`Tree`, `Grid`, `LayoutCard` and `Image` can be referenced; a `LayoutPage` cannot. A missing `ref`
fails the build unless the child is `optional: true`.

A page declares `metadata.params` (`name`, `type`, `default`, …). An artefact then renders the same
page once per parameter set — `layoutPages: [{page: sales, params: {REGION: EU}}, …]` — and
`${REGION}` expands anywhere in the page's spec. Explicit value > declared default > env var.
Docs: https://cli.bino.bi/reference/layout-page/ and https://cli.bino.bi/guides/layoutpage-params/

## Constraints

`metadata.constraints` includes a document only when every condition holds, evaluated per artefact
against `mode` (`build` / `preview` / `serve`), `artefactKind`, `labels.<key>` or `spec.<field>`.
The common case is `constraints: [mode==preview]` on a DataSet that samples rows, so `bino preview`
stays fast while `bino build` uses the full data. Operators: `==`, `!=`, `in`, `not-in`; AND only.
`ReportArtefact` itself cannot carry constraints. Names only need to be unique *after* filtering.
Docs: https://cli.bino.bi/concepts/constraints-and-scoped-names/

## Inline datasets

A component may carry its own query instead of binding to a DataSet: `spec.dataset: {query,
dependencies}`. Entries in that `dependencies` list may themselves be inline DataSource specs; the SQL
then addresses them positionally as `@inline(0)`, `@inline(1)`. Named dependencies are addressed by
name as usual. Prefer a standalone DataSet when more than one component needs the same rows.
Docs: https://cli.bino.bi/reference/dataset/

## Variables

Any string value may contain `${VAR}` or `${VAR:default}`, resolved from the environment. `bino
preview` warns about an unresolved variable and substitutes an empty string; `bino build` fails and
lists it. A manifest that only exists for preview still needs defaults or it breaks the build.
Docs: https://cli.bino.bi/concepts/workdir-and-manifests/

## bino.toml and the output directories

`bino.toml` marks the project root (bino searches upward for it) and holds `report-id` (a UUID from
`bino init`) and an optional `engine-version` pin. `dist/` is build output and `.bino/` is cache and
agent state; both are generated, never edited by hand.
Docs: https://cli.bino.bi/concepts/project-configuration/

## Three levels of checking

- `validate_draft(yaml)` — one document, in memory, against the schema and constraint syntax. It
  cannot see other documents, so a wrong `ref` or a missing dependency passes here.
- `validate_project(execute_queries?)` — the project on disk: references, dependencies, lint rules
  across documents; with `execute_queries` it runs the datasets and reports data-validation warnings.
- `build` — resolves everything, runs the SQL and renders. Only here do layout and rendering problems
  surface, and only a human looking at the PDF catches the rest.
Docs: https://cli.bino.bi/cli/mcp/

## When a concept is missing

If a term in a diagnostic, a brief, or an outline is not covered above, fetch
https://cli.bino.bi/llms-small.txt (the docs site also publishes `llms.txt` and `llms-full.txt`) and
read the matching section before guessing.
