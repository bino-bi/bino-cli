---
description: Render the bino report artefacts to PDF.
argument-hint: "[artefact names…] [--out-dir dist]"
---

Build the current bino report. Optional artefact names / output dir: **$ARGUMENTS**

1. **Validate first.** Run `validate_project()` and only build if it's clean — building a broken
   project wastes a slow render.
2. **Build.** Call `build(artefacts?, out_dir?)` (default: all artefacts → `dist`). This shells out to
   `bino build`, rendering PDFs via headless Chrome — it's slow and writes files. Stream the progress
   back to the human.
3. **Report** the exit code, the produced artefact paths, and any build-log tail on failure.
4. **Hand off to a human review.** The build can't read its own PDF back, so a clean build is **not**
   a finished report. Ask the human to open each PDF and check it visually — layout, the right
   comparison, no empty/all-null surprises.
