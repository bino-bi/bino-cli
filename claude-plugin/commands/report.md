---
description: Co-author a bino report end-to-end — from your data and a goal to a built PDF, step by step.
argument-hint: "<data/source hint> \"<what the report should show>\""
---

The user wants to co-author a bino report with you. Their data/goal: **$ARGUMENTS**

You are the assistant; the **human drives**. Follow the `bino-authoring` discipline (introspect the
live schema, `get_columns` before any SQL, `validate_draft` before every write) and apply `bino-ibcs`
semantics (scenarios, variances, component choice, narrative). Confirm direction at each major step
instead of running ahead.

1. **Orient.** `describe_project()` to see what already exists. If there is no bundle yet, offer
   `/bino:new` first. Restate the goal in IBCS terms (audience, the primary message **as a full
   sentence** — IBCS SAY, which scenarios and period, the variance, the granularity) and confirm it
   with the human.
2. **Data.** For each input the report needs, probe and scaffold it (`introspect_source` →
   `scaffold_source`, or `/bino:add-source`). Model the typed `DataSet`s with `get_columns` first.
   For a credentialed source, write only the `*FromEnv` skeleton — never an inline secret — and have
   the human set the env vars.
3. **Model (IBCS).** Map the raw columns onto scenario codes (`ac/pp/fc/pl`) and derive the variances
   the message needs (`d_`/`dr_`, correct direction). Ask if a favorable sign is ambiguous.
4. **Embeddables.** Choose the component per question (Table / ChartTime / ChartStructure), draft
   against `describe_kind`, `validate_draft`, then write. Add data-aware `Text` narrative tied to the
   primary message, grounding every number with `get_rows`.
5. **Layout + artefact.** Wire leaves-first: embeddables → `LayoutPage` → `ReportArtefact`. Confirm
   references resolve with `graph_deps`.
6. **Validate + build.** `validate_project()` to green, then `build`. Finally, **ask the human to open
   the PDF and review it** — "validates + builds" is not "correct."
