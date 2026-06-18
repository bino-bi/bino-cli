---
name: bino-data-modeling
description: Map raw source columns onto IBCS scenario slots (ac/pp/fc/pl), derive variances (d_/dr_),
  choose the grain, and write disciplined typed DataSet SQL for a bino report. Use when the bino-data
  autopilot phase models a source into datasets, or when co-authoring a DataSet. Owns the honest
  "the data can't satisfy this" — populate unmet[] rather than fabricate a column.
---

# Modeling source data into a bino DataSet

Turn raw, probed source columns into the IBCS data model the report needs — and be honest when the
data can't answer the brief. Apply `bino-ibcs` for the scenario/variance meanings.

## Map raw columns → scenarios

From the brief's `scenario_setup`, decide which raw column carries each scenario slot:

- A realized/actuals column → `ac1` (a second, *distinct* actuals measure → `ac2`, …; the numbered
  slots are different scenarios of the same type — IBCS WG1 "more scenarios per type").
- A prior-period / last-year column → `pp1` (alias `py`).
- A budget / target / plan column → `pl1` (alias `bu`).
- A forecast column → `fc1`.

Name the `DataSet`'s output measure columns with these codes so bino applies IBCS notation
automatically. If a raw column's scenario is ambiguous, that is an `open_question` for the
human — don't pick silently.

## Derive variances

Only the variances the **primary message** needs (`d_` absolute, `dr_` relative), with the favorable
direction the brief specified:

```
{type}{base}_{delta}_{direction}    e.g.  dac1_pl1_pos   drac1_pp1_neg
```

Compute them in SQL or let the component derive them — follow the schema (`describe_kind`) for how the
chosen component expects variances.

The `direction` (`pos`/`neg`/`neu`) is the measure's **polarity** (UN 4.1): is "more" favorable
(revenue) or unfavorable (cost)? It drives the variance colour (green/red/blue), so it encodes
*impact*, not sign — a cost decrease is `pos`. If the favorable sign is genuinely ambiguous, it's an
`open_question` for the human, not a guess.

## DataSet SQL discipline

1. `get_columns("$<source>")` to get the **real** raw column names before writing any SQL.
2. Write typed SQL that outputs the scenario-coded measure columns + the grain dimension(s).
3. `validate_draft(yaml)` before writing the manifest; fix every diagnostic.
4. After the data manifests exist, run the **single** `validate_project(execute_queries:true)` pass
   for the run (this executes the SQL — see the safety rules in `bino-orchestration`). Read its
   data-validation warnings: a null scenario fill or a missing column is a **blocker**, not a nuance.

## Honest failure — unmet[] over fabrication

If the brief asks for a measure or comparison the source **cannot** provide (no plan column, no prior
year, wrong grain), **do not invent it**. Record it:

```json
{ "question_or_measure": "plan vs actual by month", "reason": "source has no plan/budget column" }
```

in the DATA PLAN's `unmet[]`, and stop rather than guess. An aspirational brief that the data can't
satisfy is a human decision (reduce scope / find more data), never a fabricated column.

## Grain

Pick the grain from the brief's `granularity`. Aggregate to it in SQL; don't emit a finer grain than
the report uses (it inflates rows and risks the row-count limit).

> Cite, don't copy: `bn-template-engine/doc/data-model.md`, `src/utils/ibcsRuleSet.ts`. For the
> scenario/variance semantics, see `bino-ibcs` and its `references/ibcs-standard.md`.
