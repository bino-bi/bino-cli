---
name: bino-authoring
description: The disciplined loop for authoring bino report manifests (YAML + SQL) through the bino
  MCP server — outline the kind, scaffold the document, learn a dataset's columns, draft,
  validate_draft, write, then validate_project and build. Use whenever creating, editing, or wiring
  bino manifests (DataSource, DataSet, Table, Text, Tree, ChartTime, ChartStructure, LayoutPage,
  ReportArtefact, …) or fixing validation diagnostics. Assumes bino-concepts; pairs with bino-ibcs for
  the report semantics.
---

# Authoring bino manifests

bino is **Report-as-Code**: a report is a set of YAML manifests (each typed by a `kind`) plus SQL.
bino's JSON Schema and validators tell you precisely whether what you wrote is correct — they are
your guardrails. Lean on them; never guess structure.

Read `bino-concepts` first — it is the mental model this loop assumes (names are tables,
dependencies, the standard columns, ref vs inline children, constraints, variables).

## The one rule: introspect live, never invent

**Always read structure from the running server. Never recall or hard-code a manifest's fields.**
The schema is the source of truth and it evolves.

- `list_kinds` → every available kind and its capability category (`data` / `layout` / `embeddable`
  / `artefact` / `config`). Plugins can add kinds, so always check.
- `outline_kind(kind)` → the **first read for any kind**: one entry per spec field with its path,
  type, required flag, enum, default and description. For a layout kind it lists the allowed child
  kinds as `childKinds` instead of expanding them.
- `scaffold_kind(kind)` → **starts every new document**: the minimal YAML with `apiVersion`, `kind`,
  `metadata.name` and every required spec field pre-filled. Fill the placeholders; do not ship it as is.
- `describe_kind(kind)` (or the `bino://schema/{kind}` resource) → the full spec JSON Schema with
  `$defs`. Large. Use it **only** when the outline leaves a shape ambiguous.
- `describe_project()` / `bino://documents` → every document already in the project.

If you find yourself writing a field you did not see in the outline, stop and re-read it.

If outline_kind is not offered by this server, the bino binary predates it — use describe_kind and
read only its `properties` and `required` keys; ignore allOf/if/then wrappers.

## The authoring loop

**Check the registry before authoring from scratch.** A published predef may already ship the
component: `registry_search("kpi card", kinds: ["Table"])` finds candidates, `registry_info("@acme/kpi-card")`
shows what one contains, and `registry_add(["@acme/kpi-card"])` installs it as a dependency. Call
`registry_packages()` to see what the project already depends on and reference those documents by
`metadata.name` instead of re-authoring them. Author the component yourself only when nothing fits.
If the server does not offer the registry tools, the bino binary predates them — skip this check.

For every manifest you create:

1. **Read the shape** — `outline_kind("Table")` (or whichever kind), then `scaffold_kind("Table")`
   and fill its placeholders. For a layout kind (`LayoutPage`, `LayoutCard`, `Grid`): read
   `childKinds`, then `outline_kind(child)` for each child kind you place.
2. **Learn the data** — before writing any SQL or `${data…}` binding, call `get_columns("sales")`
   to get the real column names (prefix `$` to force a DataSource: `get_columns("$raw")`). Use
   `get_rows("sales", 20)` to ground values you'll reference in narrative.
3. **Draft** the YAML document(s) against the outline you just read.
4. **Validate in memory** — `validate_draft(yaml)` before touching disk. Fix every diagnostic and
   re-validate until clean. This is the pre-write guardrail; use it on every draft.
5. **Write** — `create_manifest(kind, name, spec, …)` to create a new manifest (it builds the
   `apiVersion`/`kind`/`metadata`/`spec` envelope, validates, and places the file by convention), or
   `write_manifest(file, yaml, append?)` for a full document / a multi-doc file.
6. **Validate the project** — `validate_project()` to catch cross-document and reference problems
   that an in-memory draft can't see. Resolve diagnostics (see *Fixing* below).
7. **Build** — `build(artefacts?)` only when the project validates. It renders PDFs via headless
   Chrome: slow, and it writes files. A clean `validate_project` does not guarantee a good-looking
   PDF — a human still needs to look at it.

The MCP server's own instructions summarize this loop; this skill is the disciplined version of it.

## Wiring references — leaves first

A report is a small dependency graph. Build it **from the leaves up** so every reference resolves at
the moment you write it:

1. **Data** first — `DataSource` (raw input), then `DataSet` (typed SQL over sources).
2. **Embeddables** next — `Table`, `Text`, `Tree`, `ChartTime`, `ChartStructure` — each binding to a
   dataset by name.
3. **Layout** — `LayoutPage` / `LayoutCard` / `Grid` arranging the embeddables.
4. **Artefact** last — `ReportArtefact` referencing the pages.

References are by `metadata.name` (and `$ref` where the schema uses it). After wiring, use
`graph_deps(kind, name, direction?)` to confirm dependencies (`out`) and dependents (`in`) resolve.

## Fixing diagnostics

- Prefer **`edit_manifest(file, position?, patch)`** for surgical fixes: it applies dotted-path edits
  (e.g. `{"spec.title": "Q3", "spec.columns[0]": "region"}`) **in place, preserving comments and key
  order**, and validates before writing. Reach for it before rewriting a whole document.
- After any edit, re-run `validate_project()` and confirm the specific diagnostic is gone — don't
  assume a fix worked.
- Read the diagnostic's file/position; bino's messages point at the offending document and key.

## Don't

- Don't copy a kind's schema into your draft from memory — `outline_kind` every time.
- Don't write SQL or data bindings before `get_columns` confirms the column names.
- Don't `build` before `validate_project` is clean.
- Don't treat "validates + builds" as "correct" — that's what the human review is for.
