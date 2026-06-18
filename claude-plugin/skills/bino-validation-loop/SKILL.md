---
name: bino-validation-loop
description: The four-layer VERDICT check (schema, IBCS, build-readiness, acceptance) and the bounded
  fix-loop policy for bino autopilot — diagnostic routing, the 2-iteration cap, and the stop-on-repeat
  rule. Used by the bino-validation subagent (to produce the VERDICT) and by the orchestrator (to
  drive the fix loop). Diagnose, never edit; PASS means "ready for sign-off," never "done."
---

# The validation loop

Two readers use this skill in different contexts: the **`bino-validation` subagent** runs the four
layers and writes the VERDICT; the **main-thread orchestrator** drives the bounded fix loop from it.

## The four layers

Run all four; a single FAIL in any layer makes the overall FAIL.

1. **Schema** — `validate_project()` (**without** `execute_queries`: the data correctness pass is
   `bino-data`'s single-owner step; you read its result, you never re-run the SQL). Structural and
   cross-document/reference errors.
2. **IBCS** — apply the **SUCCESS** rules from `bino-ibcs` (see its self-audit and
   `references/ibcs-standard.md`): **SAY** message↔content coherence (does the report actually say its
   primary message?), **UNIFY** correct scenario codes and variance favorable-direction, **EXPRESS**
   the component fits the question, **STRUCTURE** the breakdown is MECE. CHECK/CONDENSE/SIMPLIFY are
   engine-enforced.
3. **Build-readiness** — structurally confirm the report would build **without building it**: the
   `ReportArtefact` wires to real pages/embeddables (`graph_deps`), and nothing trips the
   engine-compatibility surface. Don't call `build` — that's gated and the orchestrator's to run.
4. **Acceptance** — spot-check each `brief.acceptance_criteria` against the data with `get_rows`. A
   criterion you can't check mechanically (a subjective "looks right") is reported as
   `routeTo:"human"`, not silently passed.

## The VERDICT contract

Write `.bino/agent/verdict.json`:

```json
{
  "overall": "PASS | FAIL | ESCALATE",
  "layers": { "schema": "pass|fail", "ibcs": "pass|fail", "build": "pass|fail", "acceptance": "pass|fail" },
  "diagnostics": [
    { "code": "...", "file": "...", "kind": "...", "message": "...", "routeTo": "author|data|human" }
  ],
  "next": "<one line: what should happen next>"
}
```

`PASS` = mechanically correct **and** build-ready — *ready for human sign-off*, **never "done."**
`ESCALATE` = something the loop shouldn't auto-handle (see human routes).

## Diagnostic → owner routing (heuristic)

bino's `Diagnostic` has no `category` field, so route by file/kind/message — **conservatively, human
classes first**:

| Signal | routeTo |
| --- | --- |
| IBCS-semantic (UNIFY notation / scenario meaning, SAY message↔content) | **human** |
| Ambiguous variance / favorable-direction (UNIFY, UN 4.1) | **human** |
| Engine `CompatibilityError` (version range) | **human** — no agent may widen it |
| Acceptance criterion not mechanically checkable | **human** |
| File is a `DataSource`/`DataSet`/`ConnectionSecret`, or message mentions column/SQL/type/null | **data** |
| File is an embeddable/`LayoutPage`/`ReportArtefact`/`Text`, or message mentions reference/wiring/component/notation | **author** |
| Unclassifiable, or spans both lanes | **human** |

The validator *proposes* `routeTo`; the **orchestrator re-derives** it and treats human classes as
authoritative (never overridden into an auto-fix).

## Who edits (the orchestrator enforces)

- `bino-author` is the **sole** editor of report-structure manifests (embeddables, layout, artefact,
  narrative `Text`).
- `bino-data` is the **sole** editor of `DataSource` / `DataSet` / `ConnectionSecret`.
- `bino-validation` **never edits**. A judge that wields the pen is not a guardrail.

## The bounded fix loop (orchestrator)

- Route each FAIL diagnostic to its owner; re-spawn that subagent with **only** its diagnostics and
  "fix only these." Then re-spawn `bino-validation`.
- **Cap: at most 2 automatic iterations per phase.**
- **Stop on repeat:** the same `code`+`file` appearing twice → escalate to the human (the loop isn't
  converging). Keep a seen-set across iterations.
- Any `human`-routed diagnostic or `ESCALATE` → **stop and ask the human** immediately; never auto-fix.
- The loop is pessimistic by design — when routing is unclear, it asks rather than guesses.
