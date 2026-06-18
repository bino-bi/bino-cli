---
description: Scaffold a fresh bino report bundle (bino.toml + sample manifests) ready to build or preview.
argument-hint: "[directory] [\"report title\"]"
---

Bootstrap a new bino report bundle. Arguments (all optional): **$ARGUMENTS**

1. Parse a target directory and/or a report title from the arguments. If none are given, ask the
   human for a directory name and title (the default directory is `./rainbow-report`); pick a language
   (`en` or `de`) if relevant.
2. Call `init_bundle(directory?, name?, title?, language?)`. Do **not** pass `force:true` unless the
   human explicitly wants to overwrite an existing bundle.
3. Report the files it created, then `describe_project()` to show what's in the new bundle.
4. Suggest next steps: `/bino:add-source` to bring in data, or `/bino:report` to author the report.
