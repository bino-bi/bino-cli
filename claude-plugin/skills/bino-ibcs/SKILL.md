---
name: bino-ibcs
description: The IBCS rubric for bino reports — the semantics the schema can't carry. Scenarios
  (ac/pp/fc/pl), variances (d_/dr_ … pos/neg/neu), choosing between Table / ChartTime /
  ChartStructure, and data-aware narrative Text. Use when modeling a report's scenarios and
  variances, picking a visualization, or writing commentary, so the report is IBCS-compliant.
---

# IBCS semantics for bino

bino renders reports to the [IBCS](https://www.ibcs.com/) notation standard. IBCS is a *meaning*
layer the JSON Schema can't express: which column is an actual vs a plan, how a variance is named and
coloured, which component fits the question. This skill is that rubric. It teaches semantics, not
structure — for the exact fields of any kind, always `describe_kind` (see `bino-authoring`).

> Source of truth (cite, don't copy): `bn-template-engine/src/utils/ibcsRuleSet.ts`,
> `src/utils/model/VarianceScenario.ts`, `doc/data-model.md`, `src/components/bn-text/bn-text.tsx`.

## Scenarios — the four data families

Every measure column belongs to a **scenario**. There are four families, each with up to four slots
(`ac1`…`ac4`):

| Code | Scenario | Meaning | IBCS encoding |
| --- | --- | --- | --- |
| `ac` | Actual | realized values | solid dark fill |
| `pp` | Previous Period | prior-period comparison | light-grey fill |
| `fc` | Forecast | projected values | hatched fill |
| `pl` | Plan | target / budget | outlined (no fill) |

Aliases: `bu` (budget) → `pl`, `py` (prior year) → `pp`.

**Column display order** follows IBCS: `pp` < `pl` < `fc` < `ac`. When you model a dataset, name the
measure columns with these prefixes so bino applies the right notation automatically.

## Variances — comparing two scenarios

A variance column compares two scenarios. The naming convention is:

```
{type}{base}_{delta}_{direction}
```

| Part | Values | Meaning |
| --- | --- | --- |
| `type` | `d` / `dr` | `d` = absolute delta; `dr` = relative (%) delta |
| `base` | `ac1`…`pl4` | the value subtracted **from** (minuend) |
| `delta` | `ac1`…`pl4` | the value subtracted (subtrahend) |
| `direction` | `pos` / `neg` / `neu` | which sign is *good* |

`direction` sets the colour: `pos` = favorable (teal/green), `neg` = unfavorable (red), `neu` =
neutral (grey).

Examples:

- `dac1_pl1_pos` — `ac1 − pl1`, absolute, favorable when positive (actual beat plan).
- `drac1_pp1_neg` — `(ac1 − pp1) / pp1`, relative %, unfavorable when positive (e.g. cost growth).
- `dac1_fc1_neu` — `ac1 − fc1`, absolute, neutral colour.

**Direction is a business decision.** If the favorable sign is ambiguous (is a positive cost variance
good or bad?), ask — don't assume.

## Choosing a component

Pick by the *question*, not by habit:

| Use | When |
| --- | --- |
| **Table** | Detailed comparison across a categorical dimension, drill-down (thereof / partof), and explicit variance columns. The IBCS default for "show me the numbers." |
| **ChartTime** | A **time** dimension (a date field): trends across year / quarter / month / week / day. Bars by default; lines for many points. |
| **ChartStructure** | A categorical / structural breakdown **without** time (e.g. by region, product) — a bar chart over a set. |

Confirm the exact kind names and fields with `list_kinds` / `describe_kind` — this list is the
guidance, the schema is the contract.

## Narrative — data-aware Text

`Text` (the `bn-text` component) renders commentary with live data interpolation, so the prose stays
true to the numbers:

```
${data.<dataset>[<index>].<field>}      e.g.  Revenue reached ${data.kpi[0].ac1}
${t('<i18n-key>')}                       e.g.  ${t('report.title')}
```

- The template is evaluated in a sandbox: only `data` and `t` are in scope (no `window`/`document`).
- A safe subset of HTML is allowed (`b`, `i`, `strong`, `em`, `span`, `p`, `br`, `table`, `ul`, `h1`–`h6`,
  `a`, `img`, …) with `class`/`style`/`href`/`src`/… attributes; everything else is stripped.
- **Ground every claim in the data.** Before writing a number into narrative, confirm it with
  `get_rows(<dataset>)`. Don't state a takeaway the data doesn't support.

## Putting it together

When you model a report: map raw columns onto the scenario codes (`ac/pp/fc/pl`), derive the
variances the message needs (`d_`/`dr_` with the right direction), choose the component that answers
the question, and tie any narrative back to the report's primary message — with each number verified
against the dataset.
