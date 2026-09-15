---
description: Probe a CSV / Excel / database source and scaffold a typed DataSource (and optional DataSet).
argument-hint: "<path or connection> [\"what you want from it\"]"
---

Add a data source to the current bino report. Source: **$ARGUMENTS**

1. **Probe first.** Build the bare `DataSource` spec for the source and call
   `introspect_source(spec, sheet?, limit?)` to learn its real columns, sample rows, Excel sheet
   names, and (for CSV) the detected options. Show the human what came back.
   - **Credentialed sources** (any database / S3 / WebDAV / `connection` block): this is a hard human
     gate. Write only the `*FromEnv` skeleton — **never an inline secret** — and ask the human to set
     the env vars. Don't introspect a live connection on their behalf without confirmation.
2. **Scaffold.** Call `scaffold_source(dataSource, dataSet?)` to write the typed `DataSource` and,
   when it's useful, a starter typed `DataSet` that selects from it. It validates before writing.
3. **Confirm.** `get_columns` on the new dataset/source to verify the columns resolved, and
   `validate_project()` to confirm the project is still clean.
4. Suggest modeling the data into IBCS scenarios next (`/bino:report`, applying `bino-concepts` and
   `bino-ibcs`).
