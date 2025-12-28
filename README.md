# bino – Declarative report bundles

bino is a command-line tool for building pixel-perfect PDF reports from YAML and SQL.

> **Status:** bino is under active development and its configuration and CLI APIs are not yet considered stable. Expect breaking changes between releases.

You describe your reports as “report bundles”: YAML manifests that define data sources, datasets, layouts, styles, and translations. bino executes your queries against an embedded analytical engine and renders high-quality PDFs via an automated browser.

This README is written for technical BI/report developers who are comfortable with YAML and SQL. You do not need any Go knowledge to use bino.

---

## Installation

### Supported platforms

Pre-built binaries are published for:

- macOS (Intel, Apple Silicon)
- Linux x86_64
- Windows x86_64

### Install using the bundled installer script (recommended)

We publish a small installer script as a release asset named `install.sh`. It detects your OS/architecture, downloads the matching pre-built archive, verifies the SHA-256 checksum (if available), and installs the `bino` binary for you.

Direct, single-command install (runs the installer immediately):

```sh
curl -sL https://github.com/bino-bi/bino-cli/releases/latest/download/install.sh | sh
```

Safer: download, inspect, then run:

```sh
curl -sL https://github.com/bino-bi/bino-cli/releases/latest/download/install.sh -o install.sh
less install.sh   # inspect before running
sh install.sh
```

Installer options:

- `--repo owner/repo` — install from a different repo (default: `bino-bi/bino-cli-releases`)
- `--tag <tag|latest>` — install a specific tag instead of `latest`
- `--install-dir <dir>` — destination directory for the binary (default: `$HOME/.local/bin`)
- `--dry-run` — show actions without performing them
- `--yes` — non-interactive; accept prompts

Use the safer two-step download-and-inspect flow if you prefer to review the script before execution.

### Install from GitHub Releases

1. Open the bino GitHub repository in your browser and go to the **Releases** page.
2. Download the archive that matches your platform:

   - macOS (Intel): `bino-cli_Darwin_x86_64.tar.gz`
   - macOS (Apple Silicon): `bino-cli_Darwin_arm64.tar.gz`
   - Linux x86_64: `bino-cli_Linux_x86_64.tar.gz`
   - Windows x86_64: `bino-cli_Windows_x86_64.zip`

3. Unpack the archive:

   - macOS/Linux:

     ```bash
     tar -xzvf bino-cli_Darwin_x86_64.tar.gz
     # or
     tar -xzvf bino-cli_Linux_x86_64.tar.gz
     ```

   - Windows:

     Extract the `.zip` using the file explorer or a tool like 7-Zip.

4. Put the `bino` (or `bino.exe`) binary on your `PATH`:

   - macOS/Linux (example):

     ```bash
     mv bino /usr/local/bin/
     ```

   - Windows:

     - Move `bino.exe` to a folder that is on your `%PATH%`, or
     - Add the folder containing `bino.exe` to the system/user `PATH` environment variable.

5. Verify the installation:

   ```bash
   bino version
   ```

---

## Quickstart: Your First Report Bundle

This section walks you through creating, previewing, and building a simple report in a few minutes.

### 1. Create a new bundle

Create a new directory and run the init command:

```bash
mkdir my-report
cd my-report
bino init
```

What this does:

- Asks a few questions (target folder, report name, title, language) unless you run with `--yes`.
- Creates a small set of YAML manifests that together form a **report bundle**:
  - A `ReportArtefact` describing the report.
  - One or more `DataSource` and `DataSet` documents for the sample data.
  - A `LayoutPage` describing how the report is laid out.
  - `ComponentStyle` and `Internationalization` documents for styling and translations.
  - A `.gitignore` tuned for bino’s cache and build output.

You now have a working report bundle.

### 2. Preview the report in your browser

From inside the workdir:

```bash
bino preview
```

What happens:

- bino scans the current directory for YAML manifests that belong to a bino bundle.
- It starts a local HTTP server (by default on `http://127.0.0.1:45678/`).
- It opens your default browser to show a live preview of your report.
- As you edit YAML files or data files, bino detects changes and refreshes the preview.

Use this while iterating on layouts, data, and translations.

### 3. Build a PDF

Once you're happy with the preview, build a PDF:

```bash
bino build
```

What happens:

- bino validates your manifests against its schemas.
- It runs your datasets in DuckDB (using your defined datasources).
- It renders HTML for each report artefact and uses a headless browser to export PDFs.
- It writes PDFs (and, optionally, dependency graph summaries and logs) into an output directory, by default something like `dist/` under your workdir.

You now have a PDF report you can share, attach in emails, or publish via CI.

---

### Minimal illustrative example

The following YAML shows how a few core kinds relate to each other. It is intentionally small; your editor, backed by bino’s schemas, will guide you through all available fields and options.

```yaml
# Minimal example (illustrative only – your editor shows full schema details)
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_csv
spec:
  type: csv
  path: data/sales.csv

---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_summary
spec:
  query: |
    SELECT
      region,
      SUM(amount) AS total_amount
    FROM sales_csv
    GROUP BY region
  dependencies:
    - sales_csv

---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: sales_layout
spec:
  pageLayout: full
  children:
    - kind: Text
      spec:
        value: "Sales Overview Report"
    - kind: Table
      spec:
        dataset: sales_summary

---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: sales_report
spec:
  title: Sales Overview
  filename: sales-report.pdf
  language: en
```

This example demonstrates how datasources, datasets, layouts, and report artefacts fit together. Your editor's auto-completion will guide you through all available fields and options.

---

## Core Concepts

### Workdir

A **workdir** is the root directory of a report bundle. It typically contains:

- YAML manifests defining reports, data, layouts, etc.
- Data files such as CSV or Excel.
- The build output directory (e.g. `dist/`).
- Bino’s local cache (e.g. `.bncache`).

Most commands accept `--work-dir` to point bino at the correct directory. If omitted, bino uses the current directory.

### Manifests

Bino’s configuration is spread across one or more YAML files. Each file may contain one or more **YAML documents**, separated by `---`.

Bino looks for documents with:

- An `apiVersion` that belongs to bino.
- A `kind` that identifies what the document describes.

Each `(apiVersion, kind)` combination is backed by a JSON Schema bundled with bino and used by the VS Code extension for validation and auto-completion.

### Naming Convention for `metadata.name`

All `metadata.name` values must use **camelCase** (e.g., `salesSummary`, `monthlyReport`) or **snake_case** (e.g., `sales_summary`, `monthly_report`). Hyphens (`-`) are **not allowed** in names because names are used as SQL table identifiers in queries.

Examples of valid names:

- `salesCsv`, `salesSummary`, `monthlyReport` (camelCase)
- `sales_csv`, `sales_summary`, `monthly_report` (snake_case)

Examples of **invalid** names:

- `sales-csv`, `sales-summary`, `monthly-report` (hyphens not allowed)

### Main manifest kinds

The most important kinds you’ll work with are:

- **`ReportArtefact`**  
  Describes a single report (usually resulting in one PDF file). Contains its internal name, human title, file name, language, and references to layouts and optional signing profiles.

- **`DataSource`**  
  Describes where raw data comes from (e.g. CSV, Excel, database queries, inline JSON). You configure location, format options, and connection details (if needed).

- **`DataSet`**  
  Describes a derived table built using SQL. Datasets select from datasources or other datasets and declare their dependencies so bino can keep them updated.

- **`LayoutPage`**  
  Describes how a report page is structured: page size/orientation, what components go where, and how those components bind to datasets and text.

- **`ComponentStyle`**  
  Describes reusable visual styles: typography, colors, spacing, etc. Layout components refer to styles by name to keep your report consistent.

- **`Internationalization` (i18n)**  
  Defines localized text. It maps keys to messages in a given language. Layouts and components use keys so you can switch languages without touching layouts.

- **`SigningProfile`**  
  Describes how PDFs should be digitally signed (for example, which certificate to use). A `ReportArtefact` can reference a signing profile.

- **`ConnectionSecret`**  
  Holds credentials or connection settings for databases and other external systems. Datasources reference secrets by name instead of embedding raw credentials.

For full field lists and detailed constraints, rely on the schemas your editor uses. The HOWTOs below explain how to apply these kinds to common tasks.

---

## Task-Oriented HOWTOs

This section focuses on everyday tasks, explaining which kind to use and how the pieces fit together. Field names and possible values are guided by the schemas; your editor can show you exact properties and descriptions.

### Task: Describe a report you want to build

**Goal:** Define a report that bino can build into a PDF.

**Kind:** `ReportArtefact`

Conceptually, a `ReportArtefact` answers:

- What is this report called (internally and for users)?
- Which layout(s) should be used?
- Which language and output file name should be used?
- Should this report be digitally signed?

Key ideas:

- Give the report a unique name (for referencing and filtering).
- Provide a human-readable title and description.
- Specify the output filename (without path; bino writes it to the output directory).
- Point to the layout(s) by name so bino knows how to render pages.
- Optionally reference a `SigningProfile` if PDFs should be signed.

The schema for `ReportArtefact` in your editor describes all available fields and which ones are required.

---

### Task: Use CSV or Excel as a data source

**Goal:** Read structured data from local CSV or Excel files.

**Kind:** `DataSource` (with a `type` for CSV or Excel)

Conceptually, a `DataSource` answers:

- Where is the raw data stored (file path or glob pattern)?
- What is the file format (CSV, Excel, etc.)?
- How should the file be interpreted (delimiter, header row, sheet name, etc.)?

Key ideas:

- Paths are usually relative to the workdir, not absolute.
- For CSV:
  - Configure delimiter, quote character, and whether the first row is a header.
  - Optionally provide type hints if auto-detection is not enough.
- For Excel:
  - Configure which sheet(s) and ranges to read.
  - Optionally define type conversions.

The `DataSource` schema in your editor lists which options are available per file type and which are required.

---

### Task: Use a database query as a data source

**Goal:** Fetch data via SQL from a database (e.g. Postgres).

**Kinds:** `ConnectionSecret` + `DataSource`

Conceptually:

- A `ConnectionSecret` describes how to connect: host, port, database, user, password, and other options. It keeps credentials separate from datasources.
- A `DataSource` of the appropriate type (e.g. a Postgres query type) references the secret and contains the actual SQL query.

Key ideas:

- Define a `ConnectionSecret` once per database and reference it by name in your datasource.
- Put connection details in the secret, not inline in the datasource.
- Write your SQL query in the datasource; bino passes it to DuckDB’s external connector or to the appropriate driver based on the type.

Your editor’s schemas will show the exact fields for `ConnectionSecret` and database-style `DataSource` kinds.

---

### Task: Define a dataset with SQL

**Goal:** Create a reusable derived table using SQL.

**Kind:** `DataSet`

Conceptually, a `DataSet` answers:

- What is this dataset called?
- How is it computed (SQL)?
- Which datasources and/or datasets does it depend on?

Key ideas:

- Give the dataset a unique name; you’ll reference it from layouts and other datasets.
- Write the SQL in the `DataSet` using DuckDB syntax.
- Declare dependencies on datasources/datasets by name so bino knows which inputs it should make available and how to order evaluations.

Datasets allow you to centralize logic, such as aggregations or joins, and then reuse the results across multiple layouts and reports.

---

### Task: Design a simple layout page

**Goal:** Control the structure and content of the pages in your report.

**Kind:** `LayoutPage`

Conceptually, a `LayoutPage` answers:

- What is the page size and orientation?
- Which components appear on the page (e.g. title, text blocks, tables, charts)?
- Which data and text keys feed those components?

Key ideas:

- Define one or more pages with fixed size (e.g. A4, US Letter) and orientation (portrait/landscape).
- For each component on a page:
  - Choose a component type (e.g. heading, paragraph, table).
  - Connect it to a dataset (for tables or data-bound elements).
  - Connect any text fields to i18n keys.
  - Optionally assign a style from `ComponentStyle`.

The layout schema in your editor lists supported component types and the fields required for each.

---

### Task: Style components consistently

**Goal:** Reuse consistent visual styles across components and pages.

**Kind:** `ComponentStyle`

Conceptually, a `ComponentStyle` answers:

- What should headings, paragraphs, tables, etc. look like?
- Which fonts, colors, margins, and alignments should be applied?

Key ideas:

- Define named styles that capture typography (font, size, weight), colors, spacing, and other layout options.
- Reference styles by name from components in `LayoutPage` so that changes to styles propagate everywhere.

Schemas for `ComponentStyle` describe exactly which style properties you can set.

---

### Task: Translate report text

**Goal:** Manage report text in one or more languages.

**Kind:** `Internationalization`

Conceptually, an `Internationalization` document answers:

- Which language is this for?
- What are the translated messages for each key?

Key ideas:

- Define one i18n document per language or per logical area, depending on how you want to organize translations.
- Each entry maps a key (used by layouts/components) to a message string.
- `ReportArtefact` and layouts refer to language and text keys so that bino can pick the right messages.

Schemas explain the structure of keys and messages, but the concept is straightforward: keys in layouts, messages in i18n docs.

---

### Task: Digitally sign PDFs

**Goal:** Apply digital signatures to PDFs generated by bino.

**Kinds:** `SigningProfile` + `ReportArtefact`

Conceptually:

- A `SigningProfile` describes where to find the certificate/key and how to apply the signature.
- A `ReportArtefact` references a signing profile so that bino applies it when building PDFs.

Key ideas:

- Configure signing profile details once and reuse them across multiple reports.
- Keep secure material (certificates, keys) in a secure location and reference them in the profile.

The signing-related schemas clarify which fields must be provided and which options are available.

---

## CLI: Commands for Everyday Use

This section summarizes the key commands BI/report developers will use most often.

### `bino init`

Scaffold a new report bundle.

- Common flags:
  - `--directory, -d` – target directory (default: current directory).
  - `--name` – internal report name.
  - `--title` – human-readable report title.
  - `--language` – language code (e.g. `en`, `de`).
  - `-y, --yes` – run non-interactively with defaults.
  - `--force` – overwrite existing files.
- Example:

  ```bash
  bino init --directory my-report --title "Sales Overview" --language en
  ```

### `bino preview`

Run a live preview server for a bundle.

- Common flags:
  - `--work-dir` – report bundle directory (default: `.`).
  - `--port` – port for the HTTP server (default: `45678`).
  - `--log-sql` – log executed SQL queries to the terminal.
- Example:

  ```bash
  bino preview --work-dir my-report --log-sql
  ```

### `bino build`

Validate manifests, run datasets, and build PDFs.

- Common flags:
  - `--work-dir` – report bundle directory (default: `.`).
  - `--out-dir` – output directory relative to work-dir (default: `dist`).
  - `--include` – build only specified report names (can be repeated).
  - `--exclude` – skip specified report names (can be repeated).
  - `--browser` – browser engine to use for PDF rendering (e.g. `chromium`, `firefox`, `webkit`).
  - `--no-graph` – skip writing dependency graph files (`.bngraph`).
  - `--log-sql` – log executed SQL to terminal and build log.
- Example:

  ```bash
  bino build --work-dir my-report --out-dir dist --include monthly-report --log-sql
  ```

### `bino graph`

Inspect dependencies between artefacts, datasets, and datasources.

- Common flags:
  - `--work-dir` – report bundle directory.
  - `--include` – show graph only for certain report names.
  - `--exclude` – exclude certain report names.
  - `--mode` – `tree` (hierarchical view) or `flat` (tabular view).
- Example:

  ```bash
  bino graph --work-dir my-report --mode tree
  ```

### `bino setup`

Install or update the headless browser runtimes used for PDF rendering.

- Common flags:
  - `--browser` – browser(s) to install (can be repeated).
  - `--driver-dir` – custom directory for browser driver/cache.
  - `--dry-run` – show what would be installed without downloading.
  - `--quiet` – reduce output noise.
- Example:

  ```bash
  bino setup --browser chromium
  ```

### `bino cache clean`

Clean bino's caches.

- Common flags:
  - `--work-dir` – workdir whose local cache (`.bncache`) should be removed.
  - `--all` – remove both the local cache and the global cache (e.g. in your home directory).
- Example:

  ```bash
  bino cache clean --work-dir my-report --all
  ```

### `bino version` and `bino about`

- `bino version` prints the CLI version.
- `bino about` prints product/about information.

---

## Browser Runtimes & PDF Rendering

bino uses a real browser to render HTML into high-quality PDFs. This ensures your reports look consistent across platforms, including fonts, layout, and complex components.

### Installing browsers

Typically you run this once per machine or environment:

```bash
bino setup --browser chromium
```

You can install multiple browsers if you want to test across engines.

### Choosing the browser for builds

When building reports, you can choose the browser engine with a flag (depending on your configuration). Using a single engine (e.g. `chromium`) can make troubleshooting easier.

If bino reports that a browser is missing, re-run `bino setup` and check that your environment has necessary network and disk access.

---

## VS Code Integration

A VS Code extension is available to make authoring bino bundles smoother.

### What it provides

- YAML validation based on the same JSON Schemas bino uses.
- Auto-completion for:
  - `apiVersion` and `kind`.
  - Names of `ReportArtefact`, `DataSource`, `DataSet`, `LayoutPage`, etc.
  - References between documents (e.g. layouts referencing datasets by name).
- Awareness of dataset columns in certain fields, making it easier to configure scenarios, variances, and bindings.

### Requirements

- The `bino` binary must be on your `PATH` (or configured via a dedicated setting in VS Code).
- The extension uses the schemas shipped with bino; you do not need to manage JSON Schema files manually.

Once installed, open your workdir in VS Code and edit YAML manifests; validation and completion should activate automatically.

---

### Installing the VS Code extension (current workflow)

Until the extension is published on the Marketplace, you can install it from the packaged `.vsix` file in this repository:

1. Download the latest release artifact or build the extension locally (see `INTERNAL.md` for packaging details).
2. Install the VSIX in VS Code using the CLI:

```bash
code --install-extension /path/to/vscode-bino.vsix
```

3. Reload VS Code (for example via `Developer: Reload Window`).

To uninstall the extension again:

```bash
code --uninstall-extension bino.vscode-bino
```

To update to a newer packaged version, uninstall the old one (optional but recommended), then install the new `.vsix` file with the same `code --install-extension` command.

---

## Runtime Limits & Environment Variables

bino applies sensible limits to keep builds predictable and safe. You can override these using environment variables when needed.

### Environment Variable Substitution in YAML

You can use environment variables in any YAML configuration value using the following syntax:

- `${VAR}` – replaced with the value of environment variable `VAR`
- `${VAR:default}` – replaced with the value of `VAR`, or `default` if `VAR` is not set
- `\${VAR}` – escape sequence, produces the literal text `${VAR}` (no substitution)

**Example:**

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_data
spec:
  type: csv
  path: "${DATA_DIR:./data}/sales.csv"
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: production_db
spec:
  type: postgres_query
  connection:
    host: ${DB_HOST:localhost}
    port: ${DB_PORT:5432}
    database: ${DB_NAME}
    user: ${DB_USER}
    secret: dbCredentials
  query: |
    SELECT * FROM sales
```

**Behavior for missing variables:**

- **Preview (`bino preview`):** Warns about unresolved variables and continues with empty values. This allows iterating on reports even when not all environment variables are configured.
- **Build (`bino build`):** Fails with an error listing all unresolved variables. This ensures production builds have all required configuration.

Variables with a default value (e.g., `${VAR:default}`) never trigger warnings or errors—the default is used when the variable is not set.

### Runtime Limits

Examples of limits (exact variable names and defaults may evolve):

- **Manifest scanning limits**

  - Maximum number of manifest files.
  - Maximum number of YAML documents per file.
  - Maximum total size of all manifests.
  - If exceeded, bino fails fast with a clear error message.

- **Query limits**

  - Maximum number of rows returned by a single query.
  - Maximum execution time per query.
  - If exceeded, bino aborts the query and reports an error.

- **External asset limits**
  - Maximum size and timeout for downloading external assets (if used by your bundle).

Typical usage:

- For development, defaults are usually fine.
- For large monthly/quarterly reports or heavy CI runs, you may raise limits by exporting environment variables before running `bino build` or `bino preview`.

Consult the CLI help or dedicated documentation for the current list of environment variables and their meanings.

---

## Troubleshooting

This section maps common symptoms to likely causes and fixes.

### "bino can't find any reports to build"

- Check that you're running the command in the correct directory or passing the right `--work-dir`.
- Verify that you have at least one `ReportArtefact` manifest with the correct `apiVersion` and `kind`.
- Ensure your YAML file extensions and document separators (`---`) are correct.

### “Schema validation errors in YAML”

- Open the YAML file in VS Code and check inline diagnostics; they come directly from bino’s schemas.
- Compare your document to the relevant HOWTO and confirm:
  - `apiVersion` and `kind` are correct.
  - Required fields are present and have the right type (string, number, array, etc.).
- Use completion to discover valid field names and values.

### "Browser not found"

- Run:

  ```bash
  bino setup --browser chromium
  ```

- Ensure that the setup command has network access and can write to its cache directory.
- If you set a custom driver directory, verify that the same directory is used during builds.

### “Queries are slow or fail with timeouts”

- Check your SQL queries:
  - Avoid unnecessary cross joins or unbounded scans.
  - Add filters and aggregations where appropriate.
- Consider raising query limits via environment variables if you know your workloads are large and controlled.
- Use `--log-sql` with `bino preview` or `bino build` to inspect executed SQL.

### “Tables or charts are empty / data looks wrong”

- Verify that your `DataSource` paths or connection details are correct.
- Check that your `DataSet` dependencies refer to the right datasources/datasets.
- Use `bino graph` to understand which documents depend on which.
- Log SQL and run queries directly in DuckDB (if necessary) to confirm the results.

---

## Glossary

**Workdir**  
The root directory of a report bundle. Contains YAML manifests, data files, build outputs, and caches. Most commands accept `--work-dir` to point to it.

**Manifest**  
A YAML document with an `apiVersion` and `kind` that describes part of your report bundle, for example a report, a datasource, or a layout.

**`apiVersion`**  
A string that identifies the API group and version for a manifest. Bino uses it to pick the right schema for validation.

**`kind`**  
The type of document a manifest describes (e.g. `ReportArtefact`, `DataSource`, `DataSet`, `LayoutPage`). Together with `apiVersion` it determines the shape of the document.

**Report artefact**  
A manifest describing a single report. It links together layouts, datasets, styles, translations, and optional signing into a PDF output.

**Data source**  
A manifest describing where raw data comes from (CSV, Excel, databases, inline JSON, etc.) and how it should be interpreted.

**Dataset**  
A manifest describing a derived table built using SQL over datasources and/or other datasets. Datasets are what layouts typically bind to.

**Layout**  
A manifest (usually `LayoutPage`) describing how report pages are structured: components, positions, bindings to datasets, and text.

**Style**  
A manifest (`ComponentStyle`) describing reusable visual settings (fonts, colors, spacing) to keep your report consistent.

**Internationalization (i18n)**  
Manifests that map keys to localized messages in a specific language. Layouts and components use keys; bino selects messages by language.

**Signing profile**  
A manifest describing how to digitally sign generated PDFs. A `ReportArtefact` can reference a signing profile.

**Cache**  
Directories used by bino to cache intermediate results (e.g. query results) and other artefacts. Typically found within the workdir and in a global location in your home directory.

---

## License

The `bino` CLI (Go backend and report engine) is licensed under the **BinoBi – Commercial Source Available License, Version 1.0 – 2025**.

- Full license text: see `LICENCE` in this repository.
- In short: source is available for inspection and **internal use** within a single organization is free of charge; **any commercial or external use requires a separate commercial license** from the author.

The `vscode-bino` VS Code extension found in `vscode-bino/` is licensed separately under the **MIT License** (see `vscode-bino/LICENSE`).

For commercial licensing of the `bino` CLI, contact **Sven Herrmann** (`sven@bino.bi`).

## Third‑party components

`bino` depends on several third‑party libraries and tools. A concise, up‑to‑date list of direct dependencies, including license identifiers and upstream URLs, is available via:

```bash
bino about
```

This command prints, for each direct dependency, its module path, version, license, and a link to the upstream repository or project homepage. Please refer to those upstream projects for their complete license terms.
