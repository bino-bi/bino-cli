---
description: Validate the bino project and walk the diagnostics to green.
argument-hint: "[--execute-queries] [hint at what's wrong]"
---

Get the current bino project validating cleanly. Context: **$ARGUMENTS**

1. **Validate.** Call `validate_project()`. If the user explicitly asked to also check the data, pass
   `execute_queries:true` — but note this **runs the DataSet SQL** (DuckDB SQL is not read-only), so
   only do it on a project whose queries you trust, and never expect it on a credentialed source
   unattended.
2. **Triage.** Group the diagnostics by file and kind. Fix the upstream ones first (a `DataSet` error
   often cascades into the embeddables that bind to it).
3. **Fix surgically.** Prefer `edit_manifest(file, position?, patch)` — dotted-path edits that
   preserve comments and key order and validate before writing. Read the live schema
   (`describe_kind`) when a field is in question; don't guess.
4. **Re-validate after each fix** and confirm the specific diagnostic is gone — don't assume. Repeat
   until `validate_project()` is clean.
5. If a diagnostic is an IBCS-semantic or ambiguous-direction judgment (which scenario, which sign is
   favorable), **ask the human** rather than guessing.
