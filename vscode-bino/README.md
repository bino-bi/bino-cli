# Bino Reports VS Code Extension

Enhanced YAML editing for Bino Reports report manifests with intelligent autocompletion, navigation, and project overview.

## Features

- **Language Server**: Completion, hover, diagnostics, go-to-definition, find-references, rename, and quick-fixes are all served by the real `bino lsp` Language Server (see the [LSP docs](https://cli.bino.bi/cli/lsp/))
- **Bino Explorer Tree View**: Browse all Bino documents grouped by kind (DataSource, DataSet, ReportArtefact, etc.) in the Explorer sidebar
- **Go to Definition / Find References / Rename**: Ctrl+Click (Cmd+Click on macOS) on references like `dataset:`, `source:`, `secret:`, `signingProfile:`, `selectedStyle:`, or items in `dependencies:` to jump to their definitions; Shift+F12/F2 work project-wide
- **Dataset Autocompletion**: When typing `dataset:`, suggests all DataSet names and `$`-prefixed DataSource names from your project
- **Column Introspection**: When editing `scenarios:`/`variances:` arrays or a `query:`/`prql:` block, suggests real column names from the referenced dataset via DuckDB
- **Reference Completions**: Smart completions for `signingProfile`, `selectedStyle`, and other cross-document references
- **Kind Completions**: Suggests all valid document kinds when typing `kind:`
- **Designer**: A schema-driven property-form editor for Table/Chart/Text/Tree/Grid manifests (**Bino: Open Designer**)
- **DataSource Wizard**: Scaffolds a DataSource + DataSet pair from a file or database connection (**Bino: New DataSource from File/Database…**)
- **Tree/Table Editor & Rows Preview**: A grid editor for `Tree`-kind manifests, plus a live 100-row data sample for any DataSource/DataSet
- **PRQL Integration**: When editing `spec.prql` blocks in DataSet manifests, right-click to open a dedicated PRQL editor or SQL preview (requires the [PRQL extension](https://marketplace.visualstudio.com/items?itemName=PRQL-lang.prql-vscode))

The extension talks to `bino lsp` for all live editor intelligence, and to an optional
persistent `bino daemon` (see `bino.daemon.enabled`) shared by the LSP, preview, build, and
data-introspection features so they stay warm across requests. With no daemon running, the
extension falls back to discrete `bino lsp-helper` CLI calls.

## Requirements

- [RedHat YAML Extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml) (automatically installed as dependency; provides generic YAML folding/formatting)
- `bino` CLI must be installed and available in PATH (or configured via `bino.binPath`)
- [PRQL Extension](https://marketplace.visualstudio.com/items?itemName=PRQL-lang.prql-vscode) (optional, for enhanced PRQL editing)

## Extension Settings

- `bino.binPath`: Path to the bino CLI executable. If not set, uses 'bino' from PATH.
- `bino.columnCacheTTL`: Time in milliseconds to cache column introspection results (default: 60 seconds).
- `bino.validateOnSave`: Fallback validation on save when the daemon isn't running (default: enabled).
- `bino.previewPort`: Port number for the `bino preview` server (default: 3000).
- `bino.daemon.enabled`: Enable the persistent background daemon for faster indexing, validation, and data introspection (default: enabled).
- `bino.wizard.sampleRowLimit`: Max sample rows the DataSource wizard fetches when introspecting a source (default: 100).

## Commands

- **Bino: Refresh Index** - Manually refresh the workspace index (also available via refresh button in Bino Explorer)
- **Bino: Open PRQL Editor** - Extract `spec.prql` from the current DataSet and open it in a PRQL editor with syntax highlighting
- **Bino: Open PRQL SQL Preview** - Open the PRQL SQL Preview panel to see the compiled SQL (requires PRQL extension)

## PRQL Support

Bino supports [PRQL](https://prql-lang.org) (Pipelined Relational Query Language) as an alternative to SQL for DataSet queries. When you have a DataSet with `spec.prql`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_summary
spec:
  prql: |
    from sales_csv
    filter amount > 0
    group {region} (
      aggregate {total = sum amount}
    )
    sort {-total}
  dependencies:
    - sales_csv
```

You can:

1. **Edit with PRQL tooling**: Place your cursor inside the `prql:` block and run "Bino: Open PRQL Editor" (or right-click → "Open PRQL Editor"). This opens the PRQL in a dedicated editor with syntax highlighting and diagnostics.

2. **Preview compiled SQL**: Run "Bino: Open PRQL SQL Preview" to see the SQL that DuckDB will execute. This uses the official PRQL VS Code extension's SQL Preview feature.

3. **Execute via bino build**: When you run `bino build`, PRQL queries are compiled and executed directly by DuckDB using the [prql community extension](https://duckdb.org/community_extensions/extensions/prql).

For the best PRQL editing experience, install the [PRQL extension](https://marketplace.visualstudio.com/items?itemName=PRQL-lang.prql-vscode). Bino will prompt you to install it when it detects PRQL usage in your workspace.

## How It Works

1. On activation, the extension spawns `bino lsp` (the Language Server) and indexes all YAML files in the workspace that contain `apiVersion: bino.bi`
2. The **Bino Explorer** in the sidebar shows all indexed documents grouped by kind
3. When you request completions, the language server provides context-aware suggestions:
   - `dataset:`/`source:` → Lists matching DataSet/DataSource names
   - `scenarios:`/`variances:` → Executes the referenced dataset query via DuckDB and returns column names
   - `signingProfile:`/`selectedStyle:` → Lists all matching SigningProfile/ComponentStyle names
4. **Go to Definition** / **Find All References** / **Rename** work on reference fields - Ctrl+Click, Shift+F12, or F2 to navigate/refactor
5. File changes trigger cache invalidation, re-indexing, and a fresh project-wide validation pass (lint findings, missing `${VAR}`, engine-compat warnings)
