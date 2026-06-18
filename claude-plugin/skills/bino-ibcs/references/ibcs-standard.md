# IBCS® Standard — reference for the bino-ibcs skill

> Deep reference bundled with the `bino-ibcs` skill, loaded **on demand**. The skill body
> (`../SKILL.md`) is the always-in-context rubric; read this file when you need the full IBCS
> rationale behind a rule, the SUCCESS rule codes, or the standard concepts bino does not yet model.
>
> Distilled from **IBCS Standards v1.2 (2022-01-28)** and the IBCS Work Group 1 discussion paper
> *"Semantic notation concept for more scenarios per scenario type"*. IBCS® is a registered trademark
> of the IBCS Association; the standard is published under CC BY-SA 4.0 (ibcs.com/standards). This is a
> working reference, not the licensed text — cite the official standard for definitive wording.

---

## How this maps to bino (read first)

bino renders IBCS notation **by construction**: you choose components and name columns by IBCS code,
and the engine draws the correct fills, colours, and order. So most of this reference is *grounding* —
the "why" — and a few sections are **author decisions**. The bridges between IBCS wording and bino's
concrete model:

| IBCS standard says | In bino it is |
| --- | --- |
| Scenario codes AC / PY / PL / BU / FC | `ac` / `pp` (alias `py`) / `pl` (alias `bu`) / `fc` — see `ibcsRuleSet.ts`. **`PY → pp`, `BU → pl`.** |
| Solid / lighter-solid / hatched / outlined fills | `colorIndex` 10 (ac) / 20 (pp) / 30 (fc) / 50 (pl) — applied automatically from the code. |
| Variance `ΔPL` = AC − PL (Δ + subtrahend) | column code `d{base}_{delta}_{direction}`, e.g. `dac1_pl1_pos` (base = minuend, delta = subtrahend). **Same meaning, different notation.** |
| Positive/negative/neutral impact = green/red/blue | the `direction` suffix `pos` / `neg` / `neu`. |
| WG1 "more scenarios per scenario type" (AC1, AC2 …) | the numbered slots `ac1…ac4`, `fc1…fc4`, `pl1…pl4`, `pp1…pp4`. |
| Time-series analyses (`_` YTD, `~` moving, `Ø` average, `..` span) | the `_dateLinkFormat` map in `ibcsRuleSet.ts` — bino renders these on period labels. |
| Identical scale across charts (CH 4.1) | the **`ScalingGroup`** kind. |
| EXPRESS bans (no pie / gauge / radar / funnel / traffic light) | **satisfied by construction** — bino offers no such component. |
| References/benchmarks in a distinct violet base colour (WG1 §6) | **not yet modeled in bino** — standard context only, not an authoring directive. |

> Source of truth (cite, don't copy): `bn-template-engine/src/utils/ibcsRuleSet.ts`,
> `src/utils/model/VarianceScenario.ts`, `doc/data-model.md`, `src/components/bn-text/bn-text.tsx`.

---

## 0. Mental model

IBCS = **a notation, not a template.** Its guiding maxim:

> **"Things that mean the same should look the same."** (semantic notation)

A *report* (broad sense) is a **communication product** → made of **pages** → containing **objects**
(charts, tables, text, pictures) → built from **elements** (columns, bars, lines, axes, labels) plus
**general elements** (titles, footnotes, messages). Compliance is judged at every level.

> → in bino: a report is `ReportArtefact` → `LayoutPage`(s) → embeddables (`Table`, `ChartTime`,
> `ChartStructure`, `Tree`, `Text`), arranged with `LayoutCard` / `Grid`. "Compliance at every level"
> maps to the four validation layers (schema, IBCS, build-readiness, acceptance).

Three rule families, mapped onto the SUCCESS letters:

| Family | Letters | Origin / basis | Concern |
|---|---|---|---|
| **Conceptual** | SAY, STRUCTURE | Minto's *Pyramid Principle* | What you say and how content is organized |
| **Semantic** | UNIFY | Hichert's semantic notation | Uniform visual language (the IBCS differentiator) |
| **Perceptual** | EXPRESS, CHECK, CONDENSE, SIMPLIFY | Playfair, Brinton, Zelazny, Tufte, Few | How it is drawn and perceived |

UNIFY is the **umbrella over all other areas** — it is what sets IBCS apart.

---

## 1. The SUCCESS formula (7 building blocks)

| | Letter | Block | One-line intent | Owner in bino |
|---|---|---|---|---|
| **S** | SAY | Convey a message | Every real report carries a message + evidence. | **author** |
| **U** | UNIFY | Apply semantic notation | Things that mean the same look the same — terms, measures, scenarios, variances. | **author** (codes) + engine (drawing) |
| **C** | CONDENSE | Increase information density | Everything needed on one page; small but recognizable objects. | author (layout) + engine |
| **C** | CHECK | Ensure visual integrity | Truthful, undistorted visuals — no truncated axes, misleading scaling. | **engine-enforced** (+ `ScalingGroup`) |
| **E** | EXPRESS | Choose proper visualization | Pick the object that conveys message + facts most intuitively. | **author** (component choice) |
| **S** | SIMPLIFY | Avoid clutter | Remove anything complicated, redundant, decorative. | engine + author (no decoration) |
| **S** | STRUCTURE | Organize content | Logical, consistent, exhaustive **without overlap** (MECE). | **author** |

> → in bino: the author owns **SAY, UNIFY (codes), EXPRESS, STRUCTURE**. **CHECK / CONDENSE /
> SIMPLIFY** are largely guaranteed by the engine's IBCS-native components — flag only authoring-level
> violations (e.g. decorative colour, a non-MECE breakdown), not things bino cannot do wrong.

---

## 2. UNIFY — semantic notation (the core)

The section to get exactly right; it is where most "looks IBCS / isn't IBCS" judgments are made.

### 2.1 Scenarios — the data layers (UN 3.2)

Scenarios (a.k.a. data types / versions) are layers of the business model. **Recognizable by area fill
alone, without reading labels.** Three base types:

| Base type | Meaning | Examples | Fill notation |
|---|---|---|---|
| **Actual / measured** | Things that already happened | AC, PY | **Solid dark fill**. Earlier-period actuals (scenario comparison) use a **lighter solid**. |
| **Forecast / expected** | Expected, based on measured data, not yet materialized | FC | **Hatched** (outlined + diagonal stripes); stripe colour = the measured-data colour. |
| **Plan / fictitious** | Fictitious, not-yet-materialized data | PL, BU | **Outlined / hollow** (bordered, empty); "fills up when materializing." |

Mnemonic: **solid = real, hollow = planned, hatched = forecast.** Forecast sits *between* actual and
plan (higher certainty than plan, not yet fully materialized).

Standard two-letter codes: **AC** Actual, **PY** Previous Year, **PL** Plan, **BU** Budget,
**FC** Forecast.

> → in bino: codes are `ac`, `pp` (Previous **Period** — the canonical name; `py` is an alias), `pl`
> (`bu` alias), `fc`. Fills are applied automatically via `colorIndex` (ac 10 / pp 20 / fc 30 / pl 50)
> in `ibcsRuleSet.ts`. Column display order follows IBCS: **`pp < pl < fc < ac`** (sortIndex 100 / 200
> / 300 / 400). Name a DataSet's measure columns with these prefixes and bino does the rest.

> Benchmarks (competitor, market average) may also be treated as scenarios. WG1 (§6) recommends
> rendering external **references/benchmarks in a distinct base colour — violet**.
> → in bino: **not yet modeled** — there is no violet/benchmark scenario in `ibcsRuleSet.ts`. Treat as
> standard context, not an authoring instruction.

### 2.2 Variances — the analysis (UN 4.1)

A variance compares two scenarios. Two kinds:

- **Absolute variance** = difference of two values. Notation: `Δ` + the *subtrahend*. `ΔPL` = AC − PL.
  Drawn as **variance columns/bars** — *same width and scale* as the base columns/bars.
- **Relative variance** = absolute variance as % of the subtrahend (`ΔPL%`). Drawn as **thin pins**
  (~⅓ width of base bars).

> → in bino: a variance is a column code `d{base}_{delta}_{direction}` (absolute) or
> `dr{base}_{delta}_{direction}` (relative). `dac1_pl1_pos` = `ac1 − pl1`, favorable when positive.
> The IBCS `ΔPL` label names the **subtrahend**; the bino code names **both** (base = minuend, delta =
> subtrahend). See `VarianceScenario.ts`.

**The variance colour rule (memorize):**

| Impact on business goal | Colour | Mono fallback |
|---|---|---|
| **Positive** (good) | **Green** | light grey |
| **Negative** (bad) | **Red** | dark grey |
| **Neutral** | **Blue** | medium grey |

Critical nuances:

- Colour encodes **good/bad impact, not sign.** A *cost decrease* is positive impact → green, even
  though the number is negative. Know each measure's **polarity** ("more is better" vs "less is
  better"). → in bino, this is the `pos` / `neg` / `neu` suffix, and it is a **business decision** —
  if the favorable sign is ambiguous, ask the human, don't assume.
- These variance colours are **not traffic lights** (traffic lights are banned — EX 2.5).
- Absolute variance of a percentage = **percentage points (pp)**: AC 50% − PL 40% = +10pp.
- Relative variance that can't be interpreted (positive vs negative denominator) → show **"n.a."**
- Positive variances carry an explicit **`+`** sign (`+13`, `+13%`).
- Variance **data labels go outside** the element, aligned to the direction of increase/decrease.

### 2.3 Time vs. structure orientation (UN 3.3 / 3.4)

- **Time series → horizontal axis** (charts), **columns** (tables). Time flows **left → right**.
- **Structure (dimensions) → vertical axis** (charts), **rows** (tables).
- Period abbreviations: ISO 8601 `YYYY-MM-DD` preferred. Leading `.` = first day, trailing `.` = last
  day, `..` = span (`Jan..Mar`).

> → in bino: **`ChartTime`** is the time-on-horizontal component; **`ChartStructure`** is the
> structure (categorical) component; **`Table`** puts time in columns, structure in rows. Period labels
> are formatted per granularity by `_dateFormat` (`YYYY`, `Q[Q] YYYY`, `MMM YYYY`, `[KW]WW YYYY`,
> `DD.MM.YYYY`).

### 2.4 Measures (UN 3.1)

- **Basic/value measures** → full-width columns / thick lines.
- **Ratios** → thin columns (~⅓ width) / thin lines (~50%).

### 2.5 Time-series & structure analyses (UN 4.2 / 4.3)

Lightweight notations layered onto period/element names:

| Analysis | Notation | Example |
|---|---|---|
| YTD accumulation | underscore **prefix** | `_Jun 2021` |
| YTD average | `_` prefix + `Ø` | `Ø_Jun 2021` |
| YTD last-date value | `_` prefix + trailing `.` | `_Jun 2021.` |
| Year-to-go (YTG) | underscore **suffix** | `Jun 2021_` |
| Moving (prev. 12 mo) | tilde **prefix** `~` | `~Jun 2021` (MAT) |
| Structural average | `Ø` prefix or suffix | `ØEurope` |
| Ranking | arrow `↑`/`↓` suffix | `product sales↑` |
| Index | black arrowhead at index point + `100%` label | reference = 100 |

> → in bino: these are **real, renderable features** — the `_dateLinkFormat` map in `ibcsRuleSet.ts`
> defines `ytd: "_[DATE]"`, `ytg: "[DATE]_"`, `mat: "~[DATE]"`, `avg: "Ø[DATE]"`, `interval`
> (`[DATE]..[DATE]`), `start`/`end` (`.[DATE]` / `[DATE].`), `cum` (`[DATE]_[DATE]`). When the question
> is a YTD / year-to-go / moving / average view, reach for them. Confirm the component's support via
> `describe_kind` before authoring.

### 2.6 Highlighting & scaling indicators (UN 5.1 / 5.2 / 5.3)

- **Assisting lines/areas**, **difference markers** (green/red/blue by impact), **trend arrows**
  (annotate method, e.g. `CAGR: 10.8%`), **highlighting ellipse** (usually **blue**), **reference
  arrowhead** (marks an index/benchmark line), **scaling line / scale band** (show two charts share a
  scale; **outlier indicators** for unimportant outliers instead of rescaling the whole chart).

> → in bino: shared scale across charts is the **`ScalingGroup`** kind (CH 4.1). Use it instead of
> rescaling, so comparable charts read on one scale.

### 2.7 General unification (UN 1 / UN 2)

Unify terms & abbreviations; numbers, units, dates; and the **position** of messages, titles,
legends, labels, comments, footnotes — consistently across the whole report family.

---

## 3. WG1 extension — more scenarios per scenario type

The base AC/PL/FC notation distinguishes three states. WG1 proposes orthogonal refinements so *many*
scenarios of the same type can coexist, each varied **within** the base notation:

| Refinement | Question | Visual mechanism | Increase direction |
|---|---|---|---|
| **Time gradation** | different *timeliness* | colour saturation in steps | later = darker |
| **Versions** | different *statuses* (FC 3+9 vs 6+6) | frame thickness | higher = thicker frame |
| **Variants** | different *calculation basis* | pattern density | more concrete = denser |

> → in bino: the numbered slots **`ac1…ac4`, `fc1…fc4`, `pl1…pl4`, `pp1…pp4`** are the practical
> realization — `ac1` and `ac2` are two scenarios of the *same* type. Label/title concept must carry
> the extra meaning. **Show at most 3–4 scenarios per visualization** for clarity.

---

## 4. SAY — convey a message (conceptual)

Storyline based on Minto's pyramid: *situation → complication → question → answer (message)*.

Key habits: a message is a **complete sentence** (not a topic label); the message goes in the
**title/headline**; say the message **first**; provide checkable evidence; name sources.

> → in bino: the brief's `primary_message` is the report's message — author it as a full declarative
> sentence in the title/headline and the narrative `Text`, and ground every number in the data
> (`get_rows`). Validation checks **message↔content coherence**: does the report actually say it?

---

## 5. STRUCTURE — organize content (conceptual)

MECE thinking (mutually exclusive, collectively exhaustive) + deductive/inductive reasoning. Use
consistent items, non-overlapping structures, exhaustive arguments; visualize structure in reports,
tables, and notes.

> → in bino: the breakdown a `Table` / `ChartStructure` / `Tree` shows must be **MECE** — categories
> don't overlap and together cover the whole. "thereof / partof" drill-downs in `Table` express
> hierarchy without double-counting.

---

## 6. EXPRESS — choose proper visualization (perceptual)

Time-series & structure analyses → mainly **column, bar, line, pin, waterfall** charts and IBCS
tables. **Avoid / replace:** pie & ring (EX 2.1), gauges & speedometers (EX 2.2), radar & funnel
(EX 2.3), spaghetti (EX 2.4), **traffic lights** (EX 2.5). Prefer quantitative representations; add
scenarios and variances to enrich the analysis.

> → in bino: the component set is **fixed and IBCS-native** (`Table`, `ChartTime`, `ChartStructure`,
> `Tree`, `Text`) — there is no pie / gauge / radar / funnel, so the EX 2.x bans are **satisfied by
> construction**. The author's EXPRESS decision is *which* of the allowed components answers the
> question (see `../SKILL.md` "Choosing a component").

---

## 7. CHECK — ensure visual integrity (perceptual)

The cardinal sins: truncated value axis, inconsistent scales for the same unit, area/3-D distortion.
Axes start at zero (CH 1.1); identical scale per unit (CH 4.1); size charts to the data; use scaling
& outlier indicators (CH 4.3 / 4.4).

> → in bino: the engine renders integrity-safe (axes are not truncatable, colour is not decorative).
> The one author lever is **`ScalingGroup`** for CH 4.1 — put comparable charts in one so they share a
> scale rather than each auto-scaling.

---

## 8. CONDENSE — increase information density (perceptual)

Small fonts / elements / objects; narrow margins; add data points and dimensions; overlay / multi-tier
charts; embed chart elements in tables; small multiples and related charts on one page. Aim:
everything needed to understand the content on **one page**.

> → in bino: pack a page with `LayoutCard` / `Grid`; prefer a few dense embeddables over many sparse
> pages.

---

## 9. SIMPLIFY — avoid clutter (perceptual)

Avoid cluttered layouts, filled/coloured backgrounds, animation; avoid frames, shades, pseudo-3D,
decorative colours & fonts; replace grid lines & value axes with **data labels**; right-align data;
avoid superfluous words and labels for tiny values.

> → in bino: **colour is reserved for semantic meaning** (scenario / variance) — never decoration.
> The engine already drops chart-junk; the author's job is to not *add* it (no decorative colours in
> `Text`, no redundant labels).

---

## 10. Compliance checklist (the IBCS self-audit)

Run against every page before declaring it IBCS-compliant. (The `bino-validation` agent runs the
author-owned items; engine-enforced ones are noted.)

**Message (SAY / STRUCTURE)**
- [ ] Page carries a message as a **full sentence** in the title, not a topic label.
- [ ] Structure is **MECE** — non-overlapping and exhaustive.

**Semantic notation (UNIFY)**
- [ ] Scenario codes correct & consistent (`ac`/`pp`/`fc`/`pl`), explained in title/legend.
- [ ] Variance `direction` = **impact** (`pos` good / `neg` bad / `neu`), not raw sign; measure
      polarity respected.
- [ ] Analysis notations (`_` YTD, `~` moving, `Ø` average, `..` span) applied where the question
      calls for them.

**Visualization (EXPRESS)**
- [ ] Component fits the question (Table / ChartTime / ChartStructure / Tree). *(Bans engine-enforced.)*

**Integrity / density / clarity (CHECK / CONDENSE / SIMPLIFY)**
- [ ] Comparable charts share a scale (`ScalingGroup`). *(Axes/colour integrity engine-enforced.)*
- [ ] Colour used only for semantics; no decoration. High density, one page where possible.

---

## 11. Rule-code quick index

Prefixes: **SA**=Say, **UN**=Unify, **CO**=Condense, **CH**=Check, **EX**=Express, **SI**=Simplify,
**ST**=Structure. (IBCS v1.2 numbers each headline `XX n.m`.)

- **SAY:** SA 1–5  ·  **UNIFY:** UN 1–5  ·  **CONDENSE:** CO 1–5  ·  **CHECK:** CH 1–5
- **EXPRESS:** EX 1–5  ·  **SIMPLIFY:** SI 1–5  ·  **STRUCTURE:** ST 1–5

*Sources: IBCS Standards v1.2 (2022-01-28); IBCS Work Group 1, "Semantic notation concept for more
scenarios per scenario type." IBCS® is a registered trademark of the IBCS Association; standard
published under CC BY-SA 4.0.*
