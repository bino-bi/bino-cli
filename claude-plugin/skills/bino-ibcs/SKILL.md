---
name: bino-ibcs
description: The IBCS rubric for bino reports — the semantics the schema can't carry. The SUCCESS
  rules (SAY · UNIFY · CONDENSE · CHECK · EXPRESS · SIMPLIFY · STRUCTURE), scenarios (ac/pp/fc/pl),
  variances (d_/dr_ … pos/neg/neu), analysis notations (_ ~ Ø ..), choosing between Table / ChartTime /
  ChartStructure / Tree, and data-aware narrative Text. Use when modeling a report's scenarios and
  variances, picking a visualization, writing commentary, or auditing IBCS compliance.
---

# IBCS semantics for bino

bino renders reports to the [IBCS](https://www.ibcs.com/) notation standard. IBCS is a *meaning*
layer the JSON Schema can't express: which column is an actual vs a plan, how a variance is named and
coloured, which component fits the question. This skill is that rubric. It teaches semantics, not
structure — for the exact fields of any kind, always `describe_kind` (see `bino-authoring`).

> Source of truth (cite, don't copy): `bn-template-engine/src/utils/ibcsRuleSet.ts`,
> `src/utils/model/VarianceScenario.ts`, `doc/data-model.md`, `src/components/bn-text/bn-text.tsx`.
> For the full IBCS standard (the SUCCESS rules, the rationale, the concepts bino doesn't yet model),
> read `references/ibcs-standard.md`.

## SUCCESS at a glance

IBCS is the **SUCCESS** framework — seven rule families. bino renders IBCS *by construction*, so some
families are the engine's job and some are yours:

| | Family | What it means | Who owns it in bino |
| --- | --- | --- | --- |
| **S** | **SAY** | a message as a full sentence, said first, in the title | **you** (author) |
| **U** | **UNIFY** | semantic notation — scenarios, variances, units, terms | **you** (codes) + engine (drawing) |
| **C** | CONDENSE | high density; everything on one page | you (layout) + engine |
| **C** | CHECK | visual integrity — no truncated axes, one scale per unit | engine + `ScalingGroup` |
| **E** | **EXPRESS** | the right component for the question | **you** (component choice) |
| **S** | SIMPLIFY | no clutter; colour only for meaning | engine + you (no decoration) |
| **S** | **STRUCTURE** | MECE — non-overlapping and exhaustive | **you** (author) |

Your levers are **SAY, UNIFY, EXPRESS, STRUCTURE**. CHECK / CONDENSE / SIMPLIFY are mostly guaranteed
by bino's IBCS-native components — flag only authoring-level slips (decorative colour, a non-MECE
breakdown), not things bino can't do wrong (it has no pie chart, no truncatable axis).

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

The numbered slots are **distinct scenarios of the same type** (IBCS WG1 "more scenarios per
type"): `ac1` and `ac2` are two different actuals (e.g. two collection dates), not a typo. Keep it to
**3–4 scenarios per visualization** for clarity, and let the title carry what each slot means.

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

**Direction encodes good/bad _impact_, not the raw sign.** A cost *decrease* is favorable (`pos`,
teal) even though the number is negative — so you must know each measure's **polarity**: is "more"
better (revenue) or worse (cost)? This is a business decision: if the favorable sign is ambiguous,
ask — don't assume. (And these are *impact* colours, **not** traffic lights — IBCS bans those.)

## Analysis notations (time & structure)

When the question is a *derived* time view — year-to-date, year-to-go, a moving total, an average —
bino renders the IBCS analysis notation on the period label automatically. These are real features
(the `_dateLinkFormat` map in `ibcsRuleSet.ts`), not decoration:

| Analysis | Notation | Example |
| --- | --- | --- |
| YTD accumulation | `_` **prefix** | `_Jun 2021` |
| Year-to-go (YTG) | `_` **suffix** | `Jun 2021_` |
| Moving annual total | `~` **prefix** | `~Jun 2021` |
| Average | `Ø` | `ØJun 2021`, `ØEurope` |
| Span / interval | `..` | `Jan..Mar` |
| First / last day | leading / trailing `.` | `.2021` / `2021.` |

Reach for these when the brief asks for an accumulated, projected, or smoothed view rather than the
raw period. Confirm the component's support with `describe_kind` before authoring.

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

## IBCS self-audit (the author-owned checks)

Before calling a report IBCS-compliant, run the items you actually own (CHECK/CONDENSE/SIMPLIFY are
engine-enforced — see `references/ibcs-standard.md` §10 for the full list):

- **SAY** — the primary message is a *full sentence* in the title/headline, said first; every number
  in the narrative is grounded with `get_rows`.
- **STRUCTURE** — the breakdown is **MECE** (categories don't overlap and together cover the whole).
- **UNIFY** — scenario codes correct (`ac/pp/fc/pl`); variance `direction` = impact not sign, polarity
  respected; analysis notations (`_`/`~`/`Ø`/`..`) used where the question calls for them.
- **EXPRESS** — the component fits the question (Table / ChartTime / ChartStructure / Tree).
- **SIMPLIFY** — colour is used only for semantics, never decoration.
