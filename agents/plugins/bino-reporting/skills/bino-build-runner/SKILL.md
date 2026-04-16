---
name: bino-build-runner
description: Drive the bino build / preview / serve lifecycle. Use when the user wants to render PDFs, start the live preview server, serve a LiveReportArtefact for production, or set up CI-style runs of bino. Triggers on phrases like "build the report", "render the PDF", "start preview", "run bino in CI", "serve the dashboard", or any time the discussion is about *running* bino rather than authoring or debugging.
---

# Running bino end-to-end

Four commands actually do work: `bino build`, `bino preview`,
`bino serve`, and `bino lint`. Pick the right one for the user's intent.

| Goal | Command |
|---|---|
| Produce PDF/PNG files into `dist/` (one-shot) | `bino build` |
| Author interactively with hot-reload | `bino preview` |
| Serve a `LiveReportArtefact` as a web app | `bino serve` |
| Validate without running queries (or before running) | `bino lint` |

Full flag matrix in
`../bino-author/references/commands-reference.md` — this skill covers
the *why* and the common workflows.

## `bino build` — produce artefacts

```bash
bino build                              # everything in current project
bino build --work-dir ./reports         # different project root
bino build --artefact weekly            # only one artefact
bino build --artefact weekly --artefact monthly --exclude-artefact draft
bino build --out-dir dist
```

Notable flags:

- `-w, --work-dir <dir>` — project root (default `.`).
- `--out-dir <dir>` — where outputs land (default `dist`).
- `--artefact <name>` (repeatable) — only build matching artefacts
  (`ReportArtefact`, `ScreenshotArtefact`, `DocumentArtefact`,
  `LiveReportArtefact`).
- `--exclude-artefact <name>` (repeatable) — skip these.
- `--chrome-path <path>` — override the Chrome binary.
- `--log-sql` — log every SQL query DuckDB runs.
- `--log-format text|json` — switch to JSON for machine consumption.
- `--detailed-execution-plan` — write a step-by-step JSON plan into `dist/`.
- `--data-validation warn|fail|off` — how to surface data-validation findings.
- `--no-graph` — skip writing `.dot` dependency graph.
- `--no-lint` — skip lint rules during build.
- CSV embedding (for shareable PDFs / audit logs):
  - `--embed-data-csv` — embed per-dataset CSV previews in JSON log.
  - `--embed-data-max-rows <N>` (default 10).
  - `--embed-data-max-bytes <N>` (default 65 536).
  - `--embed-data-base64` — base64-encode (default `true`).
  - `--embed-data-redact` — redact sensitive columns (default `true`).
    Masks columns matching `password | passwd | secret | token | key | api_key | private | credential | auth` (case-insensitive).

`bino build` **fails** on unresolved env vars. This is intentional — it
prevents shipping a PDF with blank SQL.

In CI, default to:

```bash
CI=1 bino build --log-format json
```

`CI=1` (or `BINO_DISABLE_UPDATE_CHECK=1`) suppresses the auto-update
check so the run is hermetic.

## `bino preview` — author with hot reload

```bash
bino preview                            # http://127.0.0.1:45678
bino preview --port 9000
bino preview --work-dir ./reports --lint
```

Watches every YAML + `.sql` / `.prql` file in the project and pushes
updates to the browser via SSE.

**Run it in the background** when invoking from a Claude Code session
(so you stay free to edit files):

```text
Bash(command="bino preview --work-dir ./my-report", run_in_background=true)
```

Then read the captured stdout to confirm the listen URL. To stop, kill
the background shell.

Useful flags: `--log-sql`, `--lint` (run lint on every refresh),
`--data-validation warn|fail|off`.

`bino preview` **warns** on unresolved env vars and keeps going — let
the user iterate on layout before wiring up credentials.

In Cowork, prefer `bino build` over `bino preview`: there's no browser
to view the SSE-driven preview, so the dev server is purely overhead.

## `bino serve` — production live reports

```bash
bino serve --live my-dashboard          # http://127.0.0.1:8080
bino serve --live my-dashboard --port 8080
bino serve --live my-dashboard --addr 0.0.0.0:8080
```

Required: `--live <name>` — the `metadata.name` of a `LiveReportArtefact`.

Differences from `preview`:

- Stateless — no file watcher.
- Renders on demand per HTTP request (caches results internally).
- Substitutes `${VAR}` from query parameters (`?REGION=DACH&YEAR=2025`).
- Fails fast on unresolved env vars (same as `build`).
- Designed to sit behind a reverse proxy.

## `bino lint` — validate, don't build

```bash
bino lint
bino lint --execute-queries             # also runs DataSource/DataSet queries
bino lint --fail-on-warnings            # exit non-zero on any finding (CI use)
bino lint --log-format json
```

Default exit code is **0 unless there's a fatal error** loading
manifests. Pass `--fail-on-warnings` for CI gates.
`--execute-queries` adds runtime schema validation (catches
column-not-found before build).

## `bino graph` — dependency tree

```bash
bino graph                              # tree view of one artefact
bino graph --view tree
bino graph --view flat                  # flat table with hashes
bino graph --artefact sales-dashboard
```

Useful right before a build to confirm the artefact you expect actually
references the pages and datasets you think it does.

For machine-readable per-document graphs, prefer
`bino lsp-helper graph-deps <dir> <kind>/<name>` (JSON output, see the
`bino-data-explorer` skill).

## `bino cache clean` — manage cache

```bash
bino cache clean                      # local project cache
bino cache clean --global             # plus ~/.bino/ (Chrome, engine, extensions)
```

If the build doesn't reflect a change, suspect cache before suspecting
the code. `--global` forces a Chrome + template engine re-download
(~30s+ per run) — avoid unless needed.

## `bino setup` — one-time install

```bash
bino setup                            # Chrome headless shell
bino setup --template-engine          # also pull bn-template-engine
bino setup --template-engine --engine-version v0.37.0
```

Required once per machine before `build` / `preview` / `serve` work.
Downloads to `~/.bino/`. Override with `CHROME_PATH` env var to use a
system Chrome.

## `bino.toml` — project-scoped defaults

Lives at the project root. Pre-set args and env vars per command so the
user doesn't have to repeat them:

```toml
report-id = "…uuid…"

[build.args]
out-dir = "dist"
log-format = "json"
artefact = ["monthly", "quarterly"]

[build.env]
BNR_MAX_QUERY_ROWS = "100000"
DB_HOST = "reporting-db.internal"

[preview.args]
port = 9000
lint = true

[preview.env]
BNR_MAX_QUERY_ROWS = "10000"         # smaller for dev
```

Priority (highest wins): CLI flag → `[cmd.args]` → default; process env
→ `[cmd.env]` → default.

## Recommended workflows

### Authoring (interactive Claude Code)

```text
1. /bino-init my-report                    # scaffold
2. cd my-report && /bino-preview           # background preview server
3. (Edit YAML; preview hot-reloads.)
4. /bino-lint --execute-queries            # before committing
5. /bino-build                             # produce the PDF
```

### CI / automation (Cowork, GitHub Actions, etc.)

```text
1. CI=1 bino setup                                           # install Chrome
2. CI=1 bino lint --fail-on-warnings --log-format json
3. CI=1 bino build --log-format json
4. (Inspect dist/<artefact>.pdf and dist/bino-build-*.json.)
```

See `../bino-author/references/workflows.md` for the full GitHub
Actions recipe.

### Iterating on a single artefact

```text
1. bino build --artefact <name> --log-sql
2. (Inspect failures.)
3. Edit; repeat.
```

`--artefact` filtering is a big win when the project has many slow
artefacts and you only care about one.

## Don't

- Don't run `bino build --no-lint --no-graph` to "make it faster"
  without understanding what you're skipping. Lint catches schema
  mistakes; graph writes the `.dot` file that downstream tooling may
  expect.
- Don't pass `BNR_*` limits to silence errors you should investigate
  (see `bino-debug`).
- Don't chain bino commands with `;` in CI; use `&&` so a lint failure
  prevents a (likely-failing) build.
- Don't use `bino cache clean --global` in CI unless you really need it
  — re-downloading Chrome wastes ~30s per run. Cache `~/.bino/`
  between runs instead.
