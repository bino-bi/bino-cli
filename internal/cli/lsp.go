package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/diagnostics"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/version"
	"bino.bi/bino/pkg/duckdb"
)

// LSPDocument represents a document entry for the LSP index output.
type LSPDocument struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Position int    `json:"position"`
}

// LSPIndexResult is the JSON output for the index command.
type LSPIndexResult struct {
	Documents []LSPDocument `json:"documents"`
	Error     string        `json:"error,omitempty"`
}

// LSPColumnsResult is the JSON output for the columns command.
type LSPColumnsResult struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Error   string   `json:"error,omitempty"`
}

// LSPDiagnostic represents a single diagnostic message for a file/document.
// It is the shared pipeline's type, so lsp-helper and the daemon cannot
// drift; the alias keeps the established name in this package.
type LSPDiagnostic = diagnostics.Diagnostic

// LSPValidateResult is the JSON output for the validate command.
type LSPValidateResult struct {
	Valid       bool            `json:"valid"`
	Diagnostics []LSPDiagnostic `json:"diagnostics"`
	Error       string          `json:"error,omitempty"`
}

// LSPGraphNode represents a node in the dependency graph.
type LSPGraphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Hash string `json:"hash,omitempty"`
}

// LSPGraphEdge represents a directed edge in the dependency graph.
type LSPGraphEdge struct {
	FromID    string `json:"fromId"`
	ToID      string `json:"toId"`
	Direction string `json:"direction"` // "in" = dependent->root, "out" = root->dependency
}

// LSPGraphDepsResult is the JSON output for the graph-deps command.
type LSPGraphDepsResult struct {
	RootID    string         `json:"rootId"`
	Direction string         `json:"direction"` // "in", "out", or "both"
	Nodes     []LSPGraphNode `json:"nodes"`
	Edges     []LSPGraphEdge `json:"edges"`
	Error     string         `json:"error,omitempty"`
}

// LSPRowsResult is the JSON output for the rows command.
type LSPRowsResult struct {
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Limit     int              `json:"limit"`
	Truncated bool             `json:"truncated"`
	Error     string           `json:"error,omitempty"`
}

func newLSPCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "lsp-helper",
		Short:  "Helper commands for LSP/IDE integration",
		Long:   "Provides workspace indexing and schema introspection for IDE autocompletion features.",
		Hidden: true,
		// Every subcommand prints machine-consumed JSON on stdout. The root
		// PersistentPreRunE routes all logging to stderr for commands carrying
		// this annotation, mirroring `bino lsp` and `bino mcp` — a stray Info
		// line on stdout breaks the extension's JSON.parse.
		Annotations: map[string]string{annotationStdoutIsData: "true"},
	}

	cmd.AddCommand(newLSPIndexCommand())
	cmd.AddCommand(newLSPColumnsCommand())
	cmd.AddCommand(newLSPValidateCommand())
	cmd.AddCommand(newLSPGraphDepsCommand())
	cmd.AddCommand(newLSPRowsCommand())
	cmd.AddCommand(newLSPIntrospectDraftCommand())
	cmd.AddCommand(newLSPPreviewDataSetCommand())
	cmd.AddCommand(newLSPDatasetSchemaCommand())
	cmd.AddCommand(newLSPKindsCommand())
	cmd.AddCommand(newLSPScaffoldCommand())
	cmd.AddCommand(newLSPTypedSelectCommand())
	cmd.AddCommand(newLSPEditCommand())
	cmd.AddCommand(newLSPCreateCommand())

	return cmd
}

func newLSPIndexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index <directory>",
		Short: "Index all bino documents in a directory",
		Long:  "Scans the directory for YAML manifests and outputs a JSON index of all document kinds and names.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			return runLSPIndex(cmd.Context(), dir, cmd.OutOrStdout())
		},
	}
	return cmd
}

func newLSPColumnsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "columns <directory> <datasource-or-dataset-name>",
		Short: "Get column names from a datasource or dataset",
		Long:  "Executes the datasource/dataset query and returns the available column names for autocompletion.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			name := args[1]
			return runLSPColumns(cmd.Context(), dir, name, cmd.OutOrStdout())
		},
	}
	return cmd
}

func runLSPIndex(ctx context.Context, dir string, out io.Writer) error {
	result := LSPIndexResult{
		Documents: []LSPDocument{},
	}

	// Find project root (directory containing bino.toml)
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}

	// Use lenient mode to skip non-bino YAML files and continue on errors
	docs, err := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})
	if err != nil {
		result.Error = fmt.Sprintf("load documents: %v", err)
		return outputJSON(out, result)
	}

	for _, doc := range docs {
		result.Documents = append(result.Documents, LSPDocument{
			Kind:     doc.Kind,
			Name:     doc.Name,
			File:     doc.File,
			Position: doc.Position,
		})
	}

	return outputJSON(out, result)
}

func runLSPColumns(ctx context.Context, dir, name string, out io.Writer) error {
	result := LSPColumnsResult{
		Name:    name,
		Columns: []string{},
	}

	// Find project root (directory containing bino.toml)
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}

	// Use lenient mode to skip non-bino YAML files and continue on errors
	docs, err := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})
	if err != nil {
		result.Error = fmt.Sprintf("load documents: %v", err)
		return outputJSON(out, result)
	}

	// Use shared introspection from datasource package
	columns, err := dataset.IntrospectColumns(ctx, docs, name)
	if err != nil {
		result.Error = fmt.Sprintf("extract columns: %v", err)
		return outputJSON(out, result)
	}

	result.Columns = columns
	return outputJSON(out, result)
}

func outputJSON(out io.Writer, v any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}

// resolveProjectRootForLSP finds the project root from the given directory.
// It searches for bino.toml walking up the directory hierarchy.
func resolveProjectRootForLSP(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return pathutil.FindProjectRoot(absDir)
}

func newLSPValidateCommand() *cobra.Command {
	var executeQueries bool

	cmd := &cobra.Command{
		Use:   "validate <directory>",
		Short: "Validate all bino documents in a directory",
		Long:  "Scans the directory for YAML manifests, validates them against the schema, and returns structured diagnostics.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			return runLSPValidate(cmd.Context(), dir, executeQueries, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&executeQueries, "execute-queries", false,
		"Execute dataset queries and validate data (slower but catches data issues)")

	return cmd
}

func runLSPValidate(ctx context.Context, dir string, executeQueries bool, out io.Writer) error {
	result := LSPValidateResult{
		Valid:       true,
		Diagnostics: []LSPDiagnostic{},
	}

	// Find project root (directory containing bino.toml)
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		result.Valid = false
		return outputJSON(out, result)
	}

	// First pass: use lenient mode to gather documents but track validation errors
	diags := validateDirectory(ctx, absDir, executeQueries)

	result.Diagnostics = diags
	result.Valid = len(diags) == 0

	return outputJSON(out, result)
}

// validateDirectory performs validation on all bino documents in a directory
// and returns structured diagnostics for any issues found. It runs the same
// diagnostics.Collect pipeline as the daemon: plugins are loaded best-effort
// (like `bino lint`), foreign YAML is skipped, and the engine-compat check is
// included. If executeQueries is true, datasets are executed and data is
// validated. The result is never nil, so the JSON output is [] rather than
// null when the bundle is clean.
func validateDirectory(ctx context.Context, dir string, executeQueries bool) []LSPDiagnostic {
	opts := diagnostics.Options{
		SkipForeign:    true,
		EngineCompat:   engineCompatDiagnostic,
		ExecuteQueries: executeQueries,
	}

	// Load plugins if declared, best-effort. lsp-helper emits pure JSON on
	// stdout, so the plugin manager gets a stderr-only logger — its Infof
	// lines must not corrupt the stream.
	projectCfg, cfgErr := pathutil.LoadProjectConfig(dir)
	if cfgErr == nil && len(projectCfg.Plugins) > 0 {
		logger := logx.NewTerminal(io.Discard, os.Stderr, false)
		mgr := plugin.NewManager(logger.Channel("plugin"))
		mgr.SetVerbose(logx.DebugEnabled(ctx))
		if err := mgr.LoadAll(ctx, projectCfg, dir, version.Version); err != nil {
			logger.Warnf("Failed to load plugins: %v", err)
		} else {
			defer mgr.ShutdownAll(ctx)
			opts.KindProvider = mgr.Registry()
			opts.PluginLinters = plugin.NewLinterRegistry(mgr.Registry())
		}
	}

	return diagnostics.Collect(ctx, dir, opts)
}

func newLSPRowsCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "rows <directory> <name>",
		Short: "Preview rows from a DataSource or DataSet",
		Long:  "Executes the datasource/dataset query and returns the first N rows for preview.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			name := args[1]
			return runLSPRows(cmd.Context(), dir, name, limit, cmd.OutOrStdout())
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of rows to return")

	return cmd
}

func runLSPRows(ctx context.Context, dir, name string, limit int, out io.Writer) error {
	result := LSPRowsResult{
		Name:    name,
		Columns: []string{},
		Rows:    []map[string]any{},
		Limit:   limit,
	}

	// Find project root (directory containing bino.toml)
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}

	// Use lenient mode to skip non-bino YAML files and continue on errors
	docs, err := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})
	if err != nil {
		result.Error = fmt.Sprintf("load documents: %v", err)
		return outputJSON(out, result)
	}

	// Find the target document (DataSource or DataSet)
	var targetDoc *config.Document
	isDataSource := strings.HasPrefix(name, "$")
	lookupName := strings.TrimPrefix(name, "$")

	for i := range docs {
		doc := &docs[i]
		if doc.Name != lookupName {
			continue
		}
		if isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
			break
		}
		if !isDataSource && doc.Kind == "DataSet" {
			targetDoc = doc
			break
		}
		// Also accept DataSource without $ prefix as fallback
		if !isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
			// Don't break, prefer DataSet if both exist
		}
	}

	if targetDoc == nil {
		result.Error = fmt.Sprintf("document not found: %s", name)
		return outputJSON(out, result)
	}

	result.Kind = targetDoc.Kind

	// Execute the query and get rows
	columns, rows, truncated, err := executeRowsPreview(ctx, targetDoc, docs, limit)
	if err != nil {
		result.Error = fmt.Sprintf("execute query: %v", err)
		return outputJSON(out, result)
	}

	result.Columns = columns
	result.Rows = rows
	result.Truncated = truncated

	return outputJSON(out, result)
}

// executeRowsPreview runs a query against a DataSource or DataSet and returns limited rows.
func executeRowsPreview(ctx context.Context, doc *config.Document, allDocs []config.Document, limit int) (cols []string, rowData []map[string]any, isTruncated bool, err error) {
	// Open a DuckDB session
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, nil, false, fmt.Errorf("duckdb options: %w", err)
	}

	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		return nil, nil, false, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	// Create temp directory for inline datasource CSV files
	tempDir, err := os.MkdirTemp("", "bino-rows-preview-")
	if err != nil {
		return nil, nil, false, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Register all DataSources as views so DataSets can reference them
	_, err = datasource.RegisterViews(ctx, session, allDocs, &datasource.ViewsOptions{
		TempDir: tempDir,
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("register views: %w", err)
	}

	// Build the query based on document type
	var query string
	switch doc.Kind {
	case "DataSource":
		// DataSource is already a view, just select from it
		query = fmt.Sprintf("SELECT * FROM %q LIMIT %d", doc.Name, limit+1)

	case "DataSet":
		// DataSet: same resolution as the build (source / prql / query, $file,
		// @inline, derive/assert) so the preview shows what the build produces.
		compiled, err := dataset.Compile(*doc)
		if err != nil {
			return nil, nil, false, err
		}
		if compiled.Prql {
			if err := session.InstallAndLoadCommunityExtensions(ctx, []string{"prql"}); err != nil {
				return nil, nil, false, fmt.Errorf("load prql extension: %w", err)
			}
		}
		if err := dataset.RunSetup(ctx, session, compiled); err != nil {
			return nil, nil, false, err
		}
		query = dataset.LimitQuery(compiled.Query, limit+1)

	default:
		return nil, nil, false, fmt.Errorf("unsupported kind: %s", doc.Kind)
	}

	// Execute the query
	rows, err := session.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, nil, false, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	// Get columns
	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, false, fmt.Errorf("get columns: %w", err)
	}

	// Scan rows
	var results []map[string]any
	values := make([]any, len(columns))
	valuePtrs := make([]any, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		// Stop after limit rows (we fetch limit+1 to detect truncation)
		if rowCount > limit {
			break
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, false, fmt.Errorf("scan row: %w", err)
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}

	if results == nil {
		results = []map[string]any{}
	}

	// Truncated if we had more rows than the limit
	truncated := rowCount > limit

	return columns, results, truncated, nil
}

// normalizeValue converts database values to JSON-serializable types.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case []byte:
		// Try to parse as JSON first
		var jsonVal any
		if err := json.Unmarshal(val, &jsonVal); err == nil {
			return jsonVal
		}
		// Otherwise return as string
		return string(val)
	default:
		return val
	}
}

func newLSPGraphDepsCommand() *cobra.Command {
	var (
		kind      string
		name      string
		direction string
		maxDepth  int
	)

	cmd := &cobra.Command{
		Use:   "graph-deps <directory>",
		Short: "Get dependency graph for a node",
		Long:  "Returns dependencies (outgoing) and/or dependents (incoming) for a specified node in the manifest graph.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			return runLSPGraphDeps(cmd.Context(), dir, kind, name, direction, maxDepth, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "Node kind: ReportArtefact, DataSet, DataSource, LayoutPage, LayoutCard, Component")
	cmd.Flags().StringVar(&name, "name", "", "Node name (e.g., dataset name, artefact name)")
	cmd.Flags().StringVar(&direction, "direction", "both", "Traversal direction: in (dependents), out (dependencies), both")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "Maximum traversal depth (0 = unlimited)")

	return cmd
}

func runLSPGraphDeps(ctx context.Context, dir, kind, name, direction string, maxDepth int, out io.Writer) error {
	result := LSPGraphDepsResult{
		Direction: direction,
		Nodes:     []LSPGraphNode{},
		Edges:     []LSPGraphEdge{},
	}

	if kind == "" || name == "" {
		result.Error = "both --kind and --name flags are required"
		return outputJSON(out, result)
	}

	if direction != "in" && direction != "out" && direction != "both" {
		result.Error = "direction must be 'in', 'out', or 'both'"
		return outputJSON(out, result)
	}

	// Find project root (directory containing bino.toml)
	absDir, err := resolveProjectRootForLSP(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve project root: %v", err)
		return outputJSON(out, result)
	}

	// Load documents in lenient mode
	docs, err := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})
	if err != nil {
		result.Error = fmt.Sprintf("load documents: %v", err)
		return outputJSON(out, result)
	}

	// Build the dependency graph
	g, err := graph.Build(ctx, docs)
	if err != nil {
		result.Error = fmt.Sprintf("build graph: %v", err)
		return outputJSON(out, result)
	}

	// Resolve the root node
	rootNode := g.NodeByManifest(kind, name)
	if rootNode == nil {
		result.Error = fmt.Sprintf("node not found: %s:%s", kind, name)
		return outputJSON(out, result)
	}

	result.RootID = rootNode.ID

	// Build reverse adjacency map for incoming edges (dependents)
	reverseAdj := make(map[string][]string)
	for _, node := range g.Nodes {
		for _, depID := range node.DependsOn {
			reverseAdj[depID] = append(reverseAdj[depID], node.ID)
		}
	}

	// Track visited nodes and collected edges
	visitedNodes := make(map[string]bool)
	var edges []LSPGraphEdge

	// Traverse outgoing edges (dependencies)
	if direction == "out" || direction == "both" {
		traverseGraph(g, rootNode.ID, "out", maxDepth, visitedNodes, &edges, nil)
	}

	// Traverse incoming edges (dependents)
	if direction == "in" || direction == "both" {
		traverseGraph(g, rootNode.ID, "in", maxDepth, visitedNodes, &edges, reverseAdj)
	}

	// Build node list from visited nodes
	for nodeID := range visitedNodes {
		node, ok := g.NodeByID(nodeID)
		if !ok {
			continue
		}
		result.Nodes = append(result.Nodes, LSPGraphNode{
			ID:   node.ID,
			Kind: string(node.Kind),
			Name: node.Name,
			File: node.File,
			Hash: node.Hash,
		})
	}

	result.Edges = edges

	return outputJSON(out, result)
}

// traverseGraph performs BFS traversal in the specified direction.
func traverseGraph(
	g *graph.Graph,
	rootID string,
	dir string,
	maxDepth int,
	visited map[string]bool,
	edges *[]LSPGraphEdge,
	reverseAdj map[string][]string,
) {
	type queueItem struct {
		id    string
		depth int
	}

	queue := []queueItem{{id: rootID, depth: 0}}
	visited[rootID] = true

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		// Check depth limit
		if maxDepth > 0 && item.depth >= maxDepth {
			continue
		}

		var neighbors []string
		if dir == "out" {
			// Outgoing: follow DependsOn
			if node, ok := g.NodeByID(item.id); ok {
				neighbors = node.DependsOn
			}
		} else {
			// Incoming: follow reverse adjacency
			neighbors = reverseAdj[item.id]
		}

		for _, neighborID := range neighbors {
			// Record the edge
			if dir == "out" {
				*edges = append(*edges, LSPGraphEdge{
					FromID:    item.id,
					ToID:      neighborID,
					Direction: "out",
				})
			} else {
				*edges = append(*edges, LSPGraphEdge{
					FromID:    neighborID,
					ToID:      item.id,
					Direction: "in",
				})
			}

			// Continue traversal if not visited
			if !visited[neighborID] {
				visited[neighborID] = true
				queue = append(queue, queueItem{id: neighborID, depth: item.depth + 1})
			}
		}
	}
}
