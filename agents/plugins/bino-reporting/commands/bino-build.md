---
description: Build PDF reports from bino manifests (bino build wrapper)
argument-hint: "[--artefact name]... [--exclude-artefact name]... [--out-dir dist] [--log-sql] [--log-format text|json]"
allowed-tools: [Bash, Read, Glob]
---

# /bino-build

Build PDF artefacts from the current bino project. Wraps `bino build`.

```bash
bino build $ARGUMENTS
```

## Common patterns

- `/bino-build` — build every `ReportArtefact` in the project.
- `/bino-build --artefact sales-dashboard` — only one.
- `/bino-build --artefact weekly --artefact monthly` — multiple.
- `/bino-build --log-sql --detailed-execution-plan --log-format json`
  — full diagnostic output (for debugging or CI logs).

## Output

PDFs land in `dist/<filename>.pdf` (filename comes from
`ReportArtefact.spec.filename`). The build also writes:

- `dist/build-log.<text|json>` — per-step log.
- `dist/execution-plan.json` (with `--detailed-execution-plan`).
- Optionally embedded CSV attachments (with the `--embed-data-csv`
  family of flags).

## After the build

Read the generated PDF location to confirm the build succeeded. If you
have a viewer available locally, mention the path so the user can open
it. In Cowork, the artefact path is the deliverable.

## When it fails

Hand off to the **bino-debug** skill. Common causes:

- Lint errors not yet fixed (run `/bino-lint --execute-queries` first).
- Missing Chrome (`bino setup` once per machine).
- `*FromEnv` env vars unset on a `ConnectionSecret` (build fails fast;
  preview would have warned and kept going).
- Cache staleness (`bino cache clean` if a recent change isn't reflected).
