# Bino Reports VS Code Extension

Enhanced YAML editing for Bino Reports report manifests with intelligent autocompletion, navigation, and project overview.

## Features

- **Schema Validation**: Automatic JSON Schema validation via RedHat YAML extension
- **Bino Explorer Tree View**: Browse all Bino documents grouped by kind (DataSource, DataSet, ReportArtefact, etc.) in the Explorer sidebar
- **Go to Definition**: Ctrl+Click (Cmd+Click on macOS) on references like `dataset:`, `signingProfile:`, or items in `dependencies:` to jump to their definitions
- **Dataset Autocompletion**: When typing `dataset:`, suggests all DataSet names and `$`-prefixed DataSource names from your project
- **Column Introspection**: When editing `scenarios:` or `variances:` arrays, suggests column names from the referenced dataset by executing DuckDB queries
- **Reference Completions**: Smart completions for `signingProfile` and other cross-document references
- **Kind Completions**: Suggests all valid document kinds when typing `kind:`
- **PRQL Integration**: When editing `spec.prql` blocks in DataSet manifests, right-click to open a dedicated PRQL editor or SQL preview (requires the [PRQL extension](https://marketplace.visualstudio.com/items?itemName=PRQL-lang.prql-vscode))

## Requirements

- [RedHat YAML Extension](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml) (automatically installed as dependency)
- `bino` CLI must be installed and available in PATH (or configured via `bino.binPath`)
- [PRQL Extension](https://marketplace.visualstudio.com/items?itemName=PRQL-lang.prql-vscode) (optional, for enhanced PRQL editing)

## Extension Settings

- `bino.binPath`: Path to the bino CLI executable. If not set, uses 'bino' from PATH.
- `bino.enableCompletion`: Enable intelligent autocompletion for dataset references, scenarios, etc.
- `bino.columnCacheTTL`: Time in milliseconds to cache column introspection results (default: 60 seconds).

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

1. On activation, the extension indexes all YAML files in the workspace that contain `apiVersion: bino.bi`
2. The **Bino Explorer** in the sidebar shows all indexed documents grouped by kind
3. When you request completions, it provides context-aware suggestions:
   - `dataset:` → Lists all DataSet and DataSource names
   - `scenarios:`/`variances:` → Executes the referenced dataset query via DuckDB and returns column names
   - `signingProfile:` → Lists all SigningProfile names
4. **Go to Definition** works on reference fields - Ctrl+Click to navigate
5. File changes trigger cache invalidation and re-indexing
