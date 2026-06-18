---
name: bino-requirements
description: Elicit report requirements from the human and produce the REPORT BRIEF — the structured
  contract (audience, primary message, scenarios, variance, granularity, visualization intent,
  acceptance criteria) that the bino autopilot subagents consume. Use during /bino:autopilot phase 1
  to turn a free-text goal into a brief. Runs on the main thread (it asks the human).
---

# Requirements → the REPORT BRIEF

This runs **on the main thread**, because only the main thread can ask the human (subagents can't).
Your job: turn a vague goal into a precise brief written in bino/IBCS vocabulary, so the downstream
`bino-data` and `bino-author` subagents act without re-interpreting prose.

## Elicit (ask in IBCS terms)

Use `AskUserQuestion`. Don't ask everything at once — propose sensible defaults from the goal and the
data, and ask the human to confirm or correct. Cover:

- **Audience** — who reads this, and what decision do they make from it?
- **Primary message** — the *one* thing the report must say, phrased as a **full declarative
  sentence**, not a topic label (IBCS **SAY**: "Actuals beat plan by 6% in Q3", not "Q3 revenue").
  It becomes the title/headline and the spine the narrative and validation check against.
- **Questions** — the specific questions the report answers.
- **Scenarios + period** — which scenarios are in play (`ac`/`pp`/`fc`/`pl`) and over what period.
- **Variance type** — absolute (`d_`), relative (`dr_`), or both; and **which sign is favorable** (a
  business decision — never assume it for costs vs revenue).
- **Granularity** — the grain (e.g. by month, by region, by product).
- **Visualization intent** — table of numbers, a time trend, a structural breakdown, narrative — or a
  mix. (Final component choice is `bino-author`'s, guided by `bino-ibcs`; capture the *intent*.)
- **Acceptance criteria** — how the human will know it's right (e.g. "total revenue ties to
  €4.2M", "plan column present for every month"). These drive the validation spot-checks later.

## The REPORT BRIEF contract

Write it to `.bino/agent/brief.json` (a JSON object — machine-readable; downstream agents `Read` it):

```json
{
  "id": "<short run id>",
  "source_goal": "<the human's original goal, verbatim>",
  "audience": "...",
  "primary_message": "...",
  "questions": ["..."],
  "scenario_setup": { "scenarios": ["ac1", "pl1", "pp1"], "period": "FY2025 by month" },
  "variance_type": "absolute | relative | both",
  "granularity": "...",
  "visualization_intent": "...",
  "acceptance_criteria": ["..."],
  "assumptions": ["<every default you filled in for the human>"],
  "open_questions": ["<anything still unresolved>"]
}
```

## Rules

- **Write in bino/IBCS vocabulary**, not loose prose — the brief is a contract, not a chat log.
- **Record every assumption** you made on the human's behalf in `assumptions[]`, so the gate review
  surfaces them.
- **Don't fabricate certainty.** If something is genuinely open, leave it in `open_questions[]` and
  flag it at the brief-confirmation gate.
- The brief is **confirmed at a gate** before any data work begins (see `bino-orchestration`).
