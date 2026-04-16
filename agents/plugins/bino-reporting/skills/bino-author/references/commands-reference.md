# bino CLI reference

Every command accepts the global flags `--verbose` / `-v` (show run ID + detailed logs) and `--no-color` (also `NO_COLOR=1`).

## Table of contents

- [bino init](#bino-init)
- [bino add dataset](#bino-add-dataset)
- [bino add datasource](#bino-add-datasource)
- [bino preview](#bino-preview)
- [bino build](#bino-build)
- [bino lint](#bino-lint)
- [bino graph](#bino-graph)
- [bino setup](#bino-setup)
- [bino serve](#bino-serve)
- [bino cache clean](#bino-cache-clean)
- [bino update](#bino-update)
- [bino version / about](#bino-version--about)
- [bino lsp-helper](#bino-lsp-helper)
- [bino.toml](#binotoml)
- [Runtime environment variables](#runtime-environment-variables)

---

## bino init

Scaffold a new report bundle with `bino.toml` + starter manifests (`data.yaml`, `pages.yaml`, `report.yaml`) and a sample CSV.

```
bino init [flags]
```

| Flag | Description |
|---|---|
| `--directory`, `-d` | Target directory (default: current dir) |
| `--name` | Internal report name |
| `--title` | Human-readable report title |
| `--language` | Language code (e.g. `en`, `de`) |
| `-y`, `--yes` | Non-interactive, accept defaults |
| `--force` | Overwrite existing files |

```bash
bino init
bino init --directory monthly-report --title "Monthly Sales" --language en
bino init -d my-report -y --force
```

Generates a unique `report-id` UUID in `bino.toml`. The file layout preference (separate files vs. multi-document YAML) is remembered in `.bino/config.json` for subsequent `bino add` wizards.

---

## bino add dataset

Interactive wizard to create a `DataSet` manifest. Supports SQL, PRQL, external file refs, or pass-through.

```
bino add dataset [name] [flags]
```

Query source (one required in non-interactive mode):

| Flag | Description |
|---|---|
| `--sql` | Inline SQL query string |
| `--sql-file` | Path to external `.sql` file (becomes `$file` ref) |
| `--prql` | Inline PRQL query string |
| `--prql-file` | Path to external `.prql` file |
| `--source` | Pass-through from another DataSet or DataSource |

Configuration:

| Flag | Description |
|---|---|
| `--deps` | Dependency name (repeatable) |
| `--constraint` | `field:operator:value` (repeatable) |
| `--description` | Free-text description |

Output:

| Flag | Description |
|---|---|
| `--output`, `-o` | Write to new file |
| `--append-to` | Append as new `---` document to existing multi-doc YAML |

Behavior:

| Flag | Description |
|---|---|
| `--no-prompt` | Fail if required values missing instead of prompting |
| `--open-editor` | Open `$VISUAL` / `$EDITOR` for query input (falls back to vim/nano) |

```bash
bino add dataset
bino add dataset monthly_sales \
  --sql "SELECT month, SUM(amount) FROM orders GROUP BY month" \
  --deps orders --output datasets/monthly_sales.yaml --no-prompt
bino add dataset quarterly --sql-file queries/quarterly.sql \
  --deps sales_csv --output datasets/quarterly.yaml --no-prompt
```

Dataset name rules: lowercase-start, `[a-z0-9_]` only for DataSource; looser for other kinds. See `manifests-data.md` — "Naming rules".

---

## bino add datasource

Interactive wizard to create a `DataSource` manifest.

```
bino add datasource [name] [flags]
```

Type (required non-interactively):

| Flag | Description |
|---|---|
| `--type` | `postgres` / `mysql` / `csv` / `parquet` / `excel` / `json` |

Database connection (for `postgres` / `mysql`):

| Flag | Description |
|---|---|
| `--db-host`, `--db-port`, `--db-database`, `--db-schema`, `--db-user` | Connection details |
| `--db-secret` | `ConnectionSecret` name (for password) |
| `--db-query` | SQL query to execute |

File source:

| Flag | Description |
|---|---|
| `--file` | Path to source file |
| `--csv-delimiter` | Field delimiter (default `,`) |
| `--csv-header` | File has header row (default `true`) |
| `--csv-skip-rows` | Rows to skip before data |

Output: `--output` / `--append-to` / `--no-prompt` (same semantics as `bino add dataset`).

```bash
bino add datasource sales_db --type postgres \
  --db-host localhost --db-port 5432 --db-database analytics \
  --db-user reporting --db-secret postgresCredentials \
  --db-query "SELECT * FROM fact_sales" \
  --output datasources/sales_db.yaml --no-prompt

bino add datasource sales_csv --type csv --file data/sales.csv \
  --csv-delimiter ";" --output datasources/sales.yaml --no-prompt
```

DataSource names **must** match `^[a-z_][a-z0-9_]*$` (SQL identifier rule).

Other `bino add` subcommands exist for every Kind — `bino add reportartefact`, `bino add layoutpage`, `bino add chartstructure`, `bino add connectionsecret`, etc. — each with its own wizard. Run `bino add --help` for the full list.

---

## bino preview

Live HTTP server with file watching, hot reload on manifest/data changes.

```
bino preview [flags]
```

| Flag | Description |
|---|---|
| `--work-dir` | Report bundle dir (default `.`) |
| `--port` | HTTP port (default `45678`) |
| `--log-sql` | Log executed SQL queries |
| `--lint` | Run lint on each refresh (off by default) |
| `--data-validation` | `warn` (default) / `fail` / `off` |

```bash
bino preview
bino preview --port 8080 --log-sql --lint
```

Browser opens automatically; if not, visit `http://127.0.0.1:45678/`. Unresolved env vars → warning + empty string (lets you iterate without setting everything).

`bino.toml` overrides: `[preview.args]`, `[preview.env]`.

---

## bino build

Validate, execute datasets, render HTML, produce PDFs + build logs.

```
bino build [flags]
```

Common:

| Flag | Description |
|---|---|
| `--work-dir` | Report bundle dir (default `.`) |
| `--out-dir` | Output dir relative to workdir (default `dist`) |
| `--artefact` | Build only named artefact(s); repeatable |
| `--exclude-artefact` | Skip named artefact(s); repeatable |
| `--chrome-path` | Chrome/Chromium binary (or `CHROME_PATH` env) |
| `--no-graph` | Skip writing `.dot` dependency graph |
| `--no-lint` | Skip lint (runs by default) |
| `--log-sql` | Log SQL queries |
| `--data-validation` | `warn` (default) / `fail` / `off` |

Build log options:

| Flag | Description |
|---|---|
| `--log-format` | `text` (default) / `json` |
| `--embed-data-csv` | Embed CSV previews in JSON log |
| `--embed-data-max-rows` | Max rows per query (default `10`) |
| `--embed-data-max-bytes` | Max bytes per embedded CSV (default `65536`) |
| `--embed-data-base64` | Base64-encode embedded CSV (default `true`) |
| `--embed-data-redact` | Redact sensitive columns (default `true`) |
| `--detailed-execution-plan` | Include per-step timing in JSON log |

```bash
bino build
bino build --artefact monthly_sales --out-dir dist/reports
bino build --log-format=json --embed-data-csv --detailed-execution-plan
bino build --no-lint --artefact draft
```

Sensitive data redaction masks columns matching: `password`, `passwd`, `secret`, `token`, `key`, `api_key`, `private`, `credential`, `auth` (case-insensitive).

**Fails** on unresolved env vars. Build log path: `dist/bino-build-<runID>.log` (text) and `dist/bino-build-<runID>.json` (if `--log-format=json` or `--embed-data-csv`).

`bino.toml` overrides: `[build.args]`, `[build.env]`.

---

## bino lint

Validate manifest schema and lint rules without rendering.

```
bino lint [flags]
```

| Flag | Description |
|---|---|
| `--work-dir` | Report bundle dir |
| `--out-dir` | Output dir for lint logs (default `dist`) |
| `--log-format` | `text` (default) / `json` |
| `--execute-queries` | Run queries and validate data (slower; catches data issues) |
| `--fail-on-warnings` | Exit 1 on any warning (for CI) |

```bash
bino lint
bino lint --execute-queries --fail-on-warnings
bino lint --log-format json
```

By default exits 0 unless there's a fatal manifest load error. Lint runs automatically inside `bino build` unless `--no-lint`.

---

## bino graph

Inspect dependency graph.

```
bino graph [flags]
```

| Flag | Description |
|---|---|
| `--work-dir` | Report bundle dir |
| `--artefact` | Focus on named artefact(s); repeatable |
| `--exclude-artefact` | Exclude named artefact(s); repeatable |
| `--view` | `tree` (default) or `flat` |

```bash
bino graph --view tree
bino graph --artefact monthly_report --view tree
```

---

## bino setup

Download Chrome headless shell and template engine to `~/.bino/`. Required one-time before `build`/`preview`/`serve` work.

```
bino setup [flags]
```

| Flag | Description |
|---|---|
| `--template-engine` | Also download/update `bn-template-engine` |
| `--engine-version` | Pin a specific engine version (e.g. `v0.37.0`) |
| `--dry-run` | Show what would be installed, skip download |
| `--quiet` | Suppress verbose installer output |

```bash
bino setup
bino setup --template-engine
bino setup --template-engine --engine-version v0.37.0
```

Chrome location: `~/.bino/chrome-headless-shell/`. Override with `CHROME_PATH` env var to use a system Chrome/Chromium.

---

## bino serve

Production-style HTTP server for `LiveReportArtefact` — per-request rendering with query-param substitution. No file watching.

```
bino serve --live <name> [flags]
```

Required: `--live <name>` (name of the `LiveReportArtefact` manifest).

| Flag | Description |
|---|---|
| `--work-dir` | Report bundle dir |
| `--port` | HTTP port (default `8080`) |
| `--addr` | Full listen address (overrides `--port`, e.g. `0.0.0.0:8080`) |
| `--log-sql` | Log SQL queries |

```bash
bino serve --live sales-dashboard
bino serve --live sales-dashboard --addr 0.0.0.0:8080 --log-sql
```

Missing required query params → HTTP 400 with JSON listing missing fields.

`bino.toml` overrides: `[serve.args]`, `[serve.env]`.

---

## bino cache clean

Clear caches.

```
bino cache clean [flags]
```

| Flag | Description |
|---|---|
| `--work-dir`, `-w` | Project whose `.bino/cache/` to clear (default `.`) |
| `--global` | Also remove `~/.bino/` |

```bash
bino cache clean                 # just this project
bino cache clean --global        # plus Chrome shell + engine + extensions
```

Clearing caches slows the next build (Chrome and extensions will re-download).

---

## bino update

Self-update the CLI and template engine from GitHub Releases.

```
bino update
```

Downloads the right binary for your OS/arch, verifies SHA-256, replaces the current binary. Template engine gets a new entry under `~/.bino/cdn/bn-template-engine/` — multiple versions coexist; the newest is used unless `bino.toml` pins `engine-version`.

Automatic update checks run once per 24h in the background. Disabled when `CI=1` or `BINO_DISABLE_UPDATE_CHECK=1`.

---

## bino version / about

```bash
bino version    # just the version string
bino about      # version + direct dependencies with license identifiers
```

Use `bino about` for compliance / dependency disclosure.

---

## bino lsp-helper

Hidden command that emits structured JSON for editor/agent integration. Full details in the `bino-data-explorer` skill. Subcommands:

| Subcommand | Purpose |
|---|---|
| `bino lsp-helper index <dir>` | JSON index of every manifest (kind, name, file, position) |
| `bino lsp-helper columns <dir> <name>` | Column names of a DataSource/DataSet |
| `bino lsp-helper rows <dir> <name> [--limit N]` | Sample rows from a DataSource/DataSet |
| `bino lsp-helper validate <dir>` | Structured diagnostics (file, line, column, severity, code) |
| `bino lsp-helper graph-deps <dir> <kind>/<name> [--direction in\|out\|both]` | Dependency graph for a single document |

Every subcommand emits JSON on stdout. Pipe through `jq` for filtering.

---

## bino.toml

Lives at the project root. Marks the workdir (searched upward from cwd like `.git/`), stores project identity, and provides per-command defaults.

```toml
report-id = "4b8f...uuid..."
engine-version = "v0.37.0"       # optional pin

[build.args]
out-dir = "dist"
log-sql = false
no-lint = false
log-format = "json"
embed-data-csv = true
detailed-execution-plan = true
artefact = ["monthly", "quarterly"]
exclude-artefact = ["draft"]

[build.env]
BNR_MAX_QUERY_ROWS = "100000"
BNR_MAX_QUERY_DURATION_MS = "120000"
DATABASE_URL = "postgres://prod/reports"

[preview.args]
port = 9000
log-sql = true
lint = true
data-validation = "warn"

[preview.env]
BNR_MAX_QUERY_ROWS = "10000"    # smaller for dev
BNR_CDN_MAX_BYTES = "10485760"

[serve.args]
port = 8080
live = "sales-dashboard"

[serve.env]
BNR_MAX_QUERY_ROWS = "100000"
```

Override priority (highest → lowest):

- **Env vars:** process env → `[<cmd>.env]` → defaults.
- **Flags:** explicit CLI flag → `[<cmd>.args]` → defaults.

The CLI logs an override message whenever a TOML value is applied.

---

## Runtime environment variables

| Variable | Default | Purpose |
|---|---|---|
| `BNR_MAX_QUERY_ROWS` | — | Max rows per query (safety cap) |
| `BNR_MAX_QUERY_DURATION_MS` | — | Query timeout, milliseconds |
| `BNR_CDN_MAX_BYTES` | — | CDN cache ceiling (preview) |
| `BNR_DATA_VALIDATION_SAMPLE_SIZE` | `10` | Rows sampled for data validation |
| `BNR_MAX_MANIFEST_FILES` | `500` | Max YAML files scanned |
| `CHROME_PATH` | — | Use a custom Chrome/Chromium instead of cached shell |
| `CI` | unset | Any value disables auto-update check |
| `BINO_DISABLE_UPDATE_CHECK` | unset | Any value disables auto-update check |
| `NO_COLOR` | unset | Any value disables ANSI color |

Any of these can also be set per-command via `[<cmd>.env]` in `bino.toml`.
