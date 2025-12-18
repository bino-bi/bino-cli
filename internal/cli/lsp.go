package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
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
type LSPDiagnostic struct {
	File     string `json:"file"`
	Position int    `json:"position"` // 1-based document index within multi-doc YAML
	Line     int    `json:"line"`     // 1-based line number (0 if unknown)
	Column   int    `json:"column"`   // 1-based column number (0 if unknown)
	Severity string `json:"severity"` // "error", "warning", "info", "hint"
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"` // optional error code
	Field    string `json:"field,omitempty"`
}

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
	}

	cmd.AddCommand(newLSPIndexCommand())
	cmd.AddCommand(newLSPColumnsCommand())
	cmd.AddCommand(newLSPValidateCommand())
	cmd.AddCommand(newLSPGraphDepsCommand())
	cmd.AddCommand(newLSPRowsCommand())

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

	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve path: %v", err)
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

	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve path: %v", err)
		return outputJSON(out, result)
	}

	// Use lenient mode to skip non-bino YAML files and continue on errors
	docs, err := config.LoadDirWithOptions(ctx, absDir, config.LoadOptions{Lenient: true})
	if err != nil {
		result.Error = fmt.Sprintf("load documents: %v", err)
		return outputJSON(out, result)
	}

	// Use shared introspection from datasource package
	columns, err := datasource.IntrospectColumns(ctx, docs, name)
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

func newLSPValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <directory>",
		Short: "Validate all bino documents in a directory",
		Long:  "Scans the directory for YAML manifests, validates them against the schema, and returns structured diagnostics.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			return runLSPValidate(cmd.Context(), dir, cmd.OutOrStdout())
		},
	}
	return cmd
}

func runLSPValidate(ctx context.Context, dir string, out io.Writer) error {
	result := LSPValidateResult{
		Valid:       true,
		Diagnostics: []LSPDiagnostic{},
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve path: %v", err)
		result.Valid = false
		return outputJSON(out, result)
	}

	// First pass: use lenient mode to gather documents but track validation errors
	diagnostics, err := validateDirectory(ctx, absDir)
	if err != nil {
		result.Error = fmt.Sprintf("validation failed: %v", err)
		result.Valid = false
		return outputJSON(out, result)
	}

	result.Diagnostics = diagnostics
	result.Valid = len(diagnostics) == 0

	return outputJSON(out, result)
}

// validateDirectory performs validation on all bino documents in a directory
// and returns structured diagnostics for any issues found.
func validateDirectory(ctx context.Context, dir string) ([]LSPDiagnostic, error) {
	var diagnostics []LSPDiagnostic

	// Load documents in strict mode first to catch schema errors
	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: false})
	if err != nil {
		// Parse the error to extract diagnostic info
		diag := parseValidationError(err, dir)
		diagnostics = append(diagnostics, diag...)

		// If strict loading failed, also try lenient to get the document list
		// for additional checks
		docs, _ = config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: true})
	}

	// Check for missing environment variables
	missingVars := config.CollectMissingEnvVars(docs)
	for _, mv := range missingVars {
		diagnostics = append(diagnostics, LSPDiagnostic{
			File:     mv.File,
			Severity: "warning",
			Message:  fmt.Sprintf("Unresolved environment variable: %s", mv.VarName),
			Code:     "missing-env-var",
		})
	}

	// Validate document uniqueness (ReportArtefact names)
	if err := config.ValidateDocuments(docs); err != nil {
		diag := parseValidationError(err, dir)
		diagnostics = append(diagnostics, diag...)
	}

	// Run lint rules and add findings as warnings
	lintDocs := configDocsToLintDocs(docs)
	runner := lint.NewDefaultRunner()
	findings := runner.Run(ctx, lintDocs)
	for _, f := range findings {
		diagnostics = append(diagnostics, LSPDiagnostic{
			File:     f.File,
			Position: f.DocIdx,
			Line:     f.Line,
			Column:   f.Column,
			Severity: "warning",
			Message:  f.Message,
			Code:     f.RuleID,
			Field:    f.Path,
		})
	}

	return diagnostics, nil
}

// parseValidationError converts a validation error into LSPDiagnostic entries.
func parseValidationError(err error, baseDir string) []LSPDiagnostic {
	var diagnostics []LSPDiagnostic

	errStr := err.Error()

	// Check for schema validation errors
	var schemaErr *spec.SchemaValidationError
	if errors.As(err, &schemaErr) {
		for _, se := range schemaErr.Errors {
			diag := LSPDiagnostic{
				Severity: "error",
				Message:  se.Description,
				Field:    se.Field,
				Code:     "schema-validation",
			}
			diagnostics = append(diagnostics, diag)
		}
		return diagnostics
	}

	// Parse file path from error message patterns like "path/file.yaml document N: ..."
	// or "path/file.yaml #N (Kind) ..."
	file, position, message := parseFileError(errStr)
	if file != "" {
		diagnostics = append(diagnostics, LSPDiagnostic{
			File:     file,
			Position: position,
			Severity: "error",
			Message:  message,
			Code:     "validation-error",
		})
	} else {
		// Generic error
		diagnostics = append(diagnostics, LSPDiagnostic{
			Severity: "error",
			Message:  errStr,
			Code:     "validation-error",
		})
	}

	return diagnostics
}

// parseFileError attempts to extract file path and position from error messages.
func parseFileError(errStr string) (file string, position int, message string) {
	// Pattern: "file.yaml document N: message" or "file.yaml #N (Kind) message"
	// Try to find patterns like "/path/to/file.yaml document 2:"
	parts := strings.SplitN(errStr, " document ", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		// Remove any prefix like "decode " or "validate "
		for _, prefix := range []string{"decode ", "read ", "validate ", "marshal ", "header "} {
			file = strings.TrimPrefix(file, prefix)
		}

		rest := parts[1]
		// Extract position number
		var posStr string
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				posStr += string(c)
			} else {
				message = strings.TrimPrefix(rest[i:], ": ")
				break
			}
		}
		if posStr != "" {
			fmt.Sscanf(posStr, "%d", &position)
		}
		return file, position, message
	}

	// Pattern: "file.yaml #N (Kind) message"
	parts = strings.SplitN(errStr, " #", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		rest := parts[1]

		// Extract position number
		var posStr string
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				posStr += string(c)
			} else {
				message = strings.TrimSpace(rest[i:])
				// Remove leading " (Kind)" pattern
				if idx := strings.Index(message, ")"); idx > 0 {
					message = strings.TrimSpace(message[idx+1:])
				}
				break
			}
		}
		if posStr != "" {
			fmt.Sscanf(posStr, "%d", &position)
		}
		return file, position, message
	}

	return "", 0, errStr
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

	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve path: %v", err)
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
func executeRowsPreview(ctx context.Context, doc *config.Document, allDocs []config.Document, limit int) ([]string, []map[string]any, bool, error) {
	// Open a DuckDB session
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, nil, false, fmt.Errorf("duckdb options: %w", err)
	}

	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		return nil, nil, false, fmt.Errorf("duckdb open: %w", err)
	}
	defer session.Close()

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
		query = fmt.Sprintf("SELECT * FROM \"%s\" LIMIT %d", doc.Name, limit+1)

	case "DataSet":
		// DataSet has a custom query (SQL or PRQL)
		var payload struct {
			Spec struct {
				Query string `json:"query"`
				Prql  string `json:"prql"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, nil, false, fmt.Errorf("parse dataset spec: %w", err)
		}

		if payload.Spec.Prql != "" {
			// Load PRQL extension for PRQL queries
			if err := session.InstallAndLoadCommunityExtensions(ctx, duckdb.CommunityExtensions()); err != nil {
				return nil, nil, false, fmt.Errorf("load prql extension: %w", err)
			}
			// For PRQL, wrap the query with a LIMIT
			query = fmt.Sprintf("SELECT * FROM (%s) AS _preview LIMIT %d", payload.Spec.Prql, limit+1)
		} else if payload.Spec.Query != "" {
			// Strip trailing semicolons to avoid syntax errors when wrapping
			sqlQuery := strings.TrimSuffix(strings.TrimSpace(payload.Spec.Query), ";")
			query = fmt.Sprintf("SELECT * FROM (%s) AS _preview LIMIT %d", sqlQuery, limit+1)
		} else {
			return nil, nil, false, fmt.Errorf("dataset has no query or prql")
		}

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

	absDir, err := filepath.Abs(dir)
	if err != nil {
		result.Error = fmt.Sprintf("resolve path: %v", err)
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
	rootNode := findGraphNode(g, kind, name)
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

// findGraphNode locates a node in the graph by kind and name.
func findGraphNode(g *graph.Graph, kind, name string) *graph.Node {
	// Special handling for ReportArtefact - use the dedicated index
	if kind == "ReportArtefact" {
		if node, ok := g.ReportArtefactByName(name); ok {
			return node
		}
		return nil
	}

	// Check if this is a component kind (Text, Asset, ChartTime, etc.)
	// Components are stored with Kind=NodeComponent and componentKind in attributes
	componentKinds := map[string]bool{
		"Text": true, "Table": true, "ChartStructure": true,
		"ChartTime": true, "Image": true, "Asset": true,
	}
	if componentKinds[kind] {
		for _, node := range g.Nodes {
			if node.Kind == graph.NodeComponent &&
				node.Attributes["componentKind"] == kind &&
				node.Name == name {
				return node
			}
		}
		return nil
	}

	// For other kinds, scan all nodes
	targetKind := graph.NodeKind(kind)
	for _, node := range g.Nodes {
		if node.Kind == targetKind && node.Name == name {
			return node
		}
	}

	return nil
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
