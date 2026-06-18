---
name: bino-orchestration
description: The bino autopilot policy — the phase sequence, the autonomy tiers, the four checkpoint
  gates, the eight hard safety gates, the run-start preflight, the subagent-spawn contract, and the
  fix-loop policy. Used by /bino:autopilot on the main thread to sequence the bino subagents and hold
  the gates. The orchestrator never authors or edits manifests — it routes.
---

# Autopilot orchestration policy

This is the policy the **main-thread** orchestrator follows. The main thread is the only actor that
can ask the human (`AskUserQuestion`), so every gate lives here, never inside a subagent. The call
graph is a strict tree: orchestrator → worker, no back-edges.

## Phase sequence

```
preflight → (1) requirements → BRIEF        ─ GATE 1: confirm brief
          → (2) @bino-data    → DATA PLAN    ─ GATE 2: confirm data
          → (3) @bino-author  → MANIFESTS
          → (4) @bino-validation → VERDICT ── fix-loop (≤2/phase) ──┐
          → (5) build (GATE 3) → PDF ── GATE 4: human visual sign-off
```

The orchestrator writes the **brief**; each subagent writes its own artifact; the orchestrator reads
them back to decide the next step.

## Autonomy tiers

| Tier | Routine gates | Status |
| --- | --- | --- |
| 0 Supervised (= co-authoring) | confirm every step | the `/bino:*` co-author commands |
| **1 Checkpointed** *(autopilot default & ceiling)* | pause after brief, after data plan, before build | **this** |
| 2 Autonomous | zero routine gates | deferred — needs engine safety primitives that don't exist yet |

**Tier 1 only. There is no `--tier` flag** — don't advertise an unsafe mode.

## Run-start preflight (blocking, before phase 1)

1. **MCP liveness** — `describe_project()` as a probe. If the bino MCP isn't reachable, stop and tell
   the human `bino mcp` isn't available.
2. **Emptiness check (H3)** — `describe_project()` / `bino://documents`. If manifests already exist,
   set `confirmed_writes = true` and announce it: existing project → autopilot confirms every write
   (bino has no rollback today, so it must not clobber hand-written manifests).
3. **Daemon detection (H7)** — if a `bino daemon` / VS Code bino session is running (ask the human if
   unsure), set `confirmed_writes = true` and refuse any unattended write batching.

Carry `confirmed_writes`, `daemon_present`, and the parsed `source_hint` into every subagent prompt.

## The four checkpoint gates (Tier-1 routine pauses)

| Gate | When | Ask |
| --- | --- | --- |
| **G1** confirm brief | after requirements | "Proceed with this brief?" (surface `assumptions`/`open_questions`) |
| **G2** confirm data plan | after `bino-data` returns | "Proceed to authoring?" (surface credentialed sources + `unmet[]`) |
| **G3** before build | after VERDICT = PASS | "Render the PDF now?" (build is slow + writes files) |
| **G4** PDF visual sign-off | after every build | "Open the PDF — approve / send back / cancel" |

A gate is an **unconditional stop**: do not proceed until the human answers, even if you're confident.

## The eight hard safety gates (every tier, grounded in source)

| # | Gate | Action |
| --- | --- | --- |
| **H1** | Credentialed source = hard human gate | agent writes only the `*FromEnv` skeleton, never an inline secret, and stops; orchestrator raises it to the human |
| **H2** | `execute_queries` is untrusted code | run **once**, in the data phase, only on this-run DataSets, never unattended vs a credentialed source; validation reads the result, never re-runs |
| **H3** | Non-empty project ⇒ confirmed writes | per-write human confirm (no rollback today) |
| **H4** | Every build is gated **and** capped | build only after G3; no auto-rebuild beyond the H8 cap |
| **H5** | Mandatory PDF visual gate | the MCP can't read the PDF back, so a human must — PASS ≠ done |
| **H6** | Data-correctness before authoring | any data-validation warning blocks handoff at G2; an aspirational brief populates `unmet[]` and stops — never fabricate a column |
| **H7** | Daemon / multi-client | detected daemon ⇒ confirmed writes (and refuse Tier 2) |
| **H8** | Bounded fix loop | ≤ 2 auto iterations per phase; stop on any repeated diagnostic |

## Subagent-spawn contract

Spawn each worker with the Agent tool (`subagent_type: bino-data` / `bino-author` / `bino-validation`).
In the prompt, pass the **paths** of the artifacts it should `Read` (not inline content) plus the tiny
run flags inline:

- `bino-data` ← `.bino/agent/brief.json` (+ `source_hint`, `confirmed_writes`) → writes
  `.bino/agent/data-plan.json`.
- `bino-author` ← `brief.json` + `data-plan.json` (+ `confirmed_writes`) → writes
  `.bino/agent/manifests.json`. Under `confirmed_writes`, instruct it to return its proposed write set
  **before** writing, so you can gate each write (it can't ask the human itself).
- `bino-validation` ← `manifests.json` + `brief.json` + `data-plan.json` → writes
  `.bino/agent/verdict.json`. It diagnoses only.

The orchestrator never authors or edits a manifest itself, and never runs `execute_queries` or `build`
on behalf of a worker (build is gated at G3; `execute_queries` is `bino-data`'s single-owner step).

## Artifacts (`.bino/agent/`)

The four artifacts are **ephemeral session scratch** written with the generic `Write` tool — not bino
project state, not validated by bino, not visible to VS Code. Ensure `.bino/agent/` is git-ignored in
the user's project (add it if missing), and tell the human they're throwaway.

## The deepest risk

"Schema-valid + builds" is **not** "correct" — an empty, all-null, or wrong-comparison report can
validate and render. Defense in depth, none sufficient alone: the data-correctness gate (H6), the IBCS
checks (especially message↔content coherence), and the mandatory human visual gate (H5/G4). Treat a
PASS as *ready for sign-off*, and say so.
