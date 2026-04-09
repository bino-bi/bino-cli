# bino

bino is a command-line tool for building pixel-perfect PDF reports from YAML manifests and SQL queries.

> **Status:** bino is under active development. Configuration and CLI APIs are not yet stable — expect breaking changes between releases.

For installation guides, usage documentation, and manifest reference, visit **[cli.bino.bi](https://cli.bino.bi)**.

## Building from Source

### Prerequisites

- Go 1.25 or later
- CGO enabled (required for DuckDB)
- Chrome headless shell (for PDF rendering — `bino setup` downloads it automatically)

### Build

```bash
goreleaser build --snapshot --clean --single-target
```

On macOS, copy the binary to your PATH and sign it:

```bash
cp ./dist/bino_darwin_arm64_v8.0/bino ~/go/bin/bino
codesign --force --sign - ~/go/bin/bino
```

## Architecture

### Processing Pipeline

```
Manifest Discovery → YAML Parsing → Validation → Datasource Collection → Dataset Execution → HTML Rendering → PDF Generation
```

Manifests are YAML documents typed by a `kind` field (DataSource, DataSet, LayoutPage, ReportArtefact, etc.). DuckDB serves as the embedded SQL engine for all data queries. Chrome headless shell converts rendered HTML into PDF output.

### Directory Structure

```
cmd/bino/main.go        Entry point with signal handling and context setup
internal/cli/            CLI commands (build, preview, serve, lint, graph, init, lsp, cache)
internal/report/         Core report processing engine
  config/                  YAML manifest loading and validation
  spec/                    Schema and constraint definitions
  datasource/              Data source collection (CSV, Excel, databases via DuckDB)
  dataset/                 SQL query execution
  pipeline/                Build orchestration
  render/                  HTML/PDF rendering
pkg/duckdb/              Exportable DuckDB session wrapper
vscode-bino/             VS Code extension (TypeScript)
docs/                    Documentation website (https://cli.bino.bi)
```

## Development

Run all tests:

```bash
go test -v -race ./...
```

Run the linter:

```bash
golangci-lint run ./...
```

Run a specific test:

```bash
go test -run TestName ./...
```

Test with coverage:

```bash
go test -v -race -coverprofile=coverage.out ./...
```

### Runtime Limit Overrides

These environment variables are useful during development:

| Variable | Default | Description |
|---|---|---|
| `BNR_MAX_MANIFEST_FILES` | 500 | Max manifest files to scan |
| `BNR_MAX_QUERY_ROWS` | 100,000 | Max rows returned per query |
| `BNR_MAX_QUERY_DURATION_MS` | 60,000 | Query timeout in milliseconds |
| `CI` | — | Set to `1` to disable update check |

## VS Code Extension

The `vscode-bino/` directory contains a VS Code extension that provides YAML validation and auto-completion based on bino's JSON Schemas.

```bash
cd vscode-bino
npm ci
npm run compile
```

Package a `.vsix` for distribution:

```bash
npx vsce package
```

## License

This project is licensed under the **GNU Affero General Public License v3.0 (AGPLv3)** — see the [LICENCE](LICENCE) file for details.

The VS Code extension in `vscode-bino/` is licensed separately under the **MIT License** (see `vscode-bino/LICENSE`).

## Third-Party Dependencies

Run `bino about` to list all direct dependencies with their licenses and upstream URLs.
