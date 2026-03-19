package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/pkg/duckdb"
)

// State holds the shared daemon state with cached documents and diagnostics.
type State struct {
	mu          sync.RWMutex
	projectRoot string
	session     *duckdb.Session
	documents   []config.Document
	diagnostics []Diagnostic
	lastIndexAt time.Time
	logger      logx.Logger
	tempDir     string
}

// NewState creates a new daemon state.
func NewState(projectRoot string, session *duckdb.Session, logger logx.Logger) (*State, error) {
	tempDir, err := os.MkdirTemp("", "bino-daemon-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	return &State{
		projectRoot: projectRoot,
		session:     session,
		logger:      logger,
		tempDir:     tempDir,
	}, nil
}

// Close releases resources held by the state.
func (s *State) Close() {
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

// Refresh reloads documents from disk, registers views, and re-validates.
func (s *State) Refresh(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}

	// Load documents in lenient mode
	docs, err := config.LoadDirWithOptions(ctx, s.projectRoot, config.LoadOptions{Lenient: true})
	if err != nil {
		return fmt.Errorf("load documents: %w", err)
	}

	// Register views on the shared session
	_, err = datasource.RegisterViews(ctx, s.session, docs, &datasource.ViewsOptions{
		TempDir: s.tempDir,
	})
	if err != nil {
		s.logger.Warnf("register views: %v", err)
	}

	// Validate
	diagnostics := s.validateDocs(ctx)

	s.documents = docs
	s.diagnostics = diagnostics
	s.lastIndexAt = time.Now()

	return nil
}

// Documents returns a copy of the cached document list.
func (s *State) Documents() []config.Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.documents
}

// Diagnostics returns a copy of the cached diagnostics.
func (s *State) Diagnostics() []Diagnostic {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.diagnostics
}

// LastIndexAt returns when the last refresh completed.
func (s *State) LastIndexAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastIndexAt
}

// Session returns the shared DuckDB session.
func (s *State) Session() *duckdb.Session {
	return s.session
}

// ProjectRoot returns the project root directory.
func (s *State) ProjectRoot() string {
	return s.projectRoot
}

// validateDocs runs full validation and returns diagnostics.
// Must be called with s.mu held.
func (s *State) validateDocs(ctx context.Context) []Diagnostic {
	var diagnostics []Diagnostic
	dir := s.projectRoot

	// Load documents in strict mode to catch schema errors
	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: false})
	if err != nil {
		diag := parseValidationError(err)
		diagnostics = append(diagnostics, diag...)
		// Use the lenient docs already loaded (s.documents may not be set yet)
		docs, _ = config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: true})
	}

	// Check for missing environment variables
	paramNames := config.CollectLayoutPageParamNames(docs)
	missingVars := config.CollectMissingEnvVarsExcluding(docs, paramNames)
	for _, mv := range missingVars {
		diagnostics = append(diagnostics, Diagnostic{
			File:     mv.File,
			Severity: "warning",
			Message:  fmt.Sprintf("Unresolved environment variable: %s", mv.VarName),
			Code:     "missing-env-var",
		})
	}

	// Validate document uniqueness
	if err := config.ValidateDocuments(docs); err != nil {
		diag := parseValidationError(err)
		diagnostics = append(diagnostics, diag...)
	}

	// Run lint rules
	lintDocs := make([]lint.Document, 0, len(docs))
	for _, d := range docs {
		lintDocs = append(lintDocs, lint.Document{
			File:        d.File,
			Position:    d.Position,
			Kind:        d.Kind,
			Name:        d.Name,
			Labels:      d.Labels,
			Constraints: d.Constraints,
			Raw:         d.Raw,
		})
	}
	runner := lint.NewDefaultRunner()
	findings := runner.Run(ctx, lintDocs)
	for _, f := range findings {
		diagnostics = append(diagnostics, Diagnostic{
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

	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	return diagnostics
}

// ValidateWithQueries runs validation including query execution and returns diagnostics.
func (s *State) ValidateWithQueries(ctx context.Context) []Diagnostic {
	s.mu.RLock()
	docs := s.documents
	dir := s.projectRoot
	s.mu.RUnlock()

	diagnostics := s.Diagnostics()

	if len(docs) == 0 {
		return diagnostics
	}

	execOpts := &dataset.ExecuteOptions{
		DataValidation:           dataset.DataValidationWarn,
		DataValidationSampleSize: dataset.GetDataValidationSampleSize(),
	}
	_, warnings, err := dataset.Execute(ctx, dir, docs, execOpts)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: "warning",
			Message:  fmt.Sprintf("Query execution failed: %v", err),
			Code:     "data-validation-error",
		})
	}
	for _, w := range warnings {
		var file string
		var position int
		for _, doc := range docs {
			if doc.Kind == "DataSet" && doc.Name == w.DataSet {
				file = doc.File
				position = doc.Position
				break
			}
		}
		diagnostics = append(diagnostics, Diagnostic{
			File:     file,
			Position: position,
			Severity: "warning",
			Message:  w.Message,
			Code:     "data-validation",
			Field:    w.DataSet,
		})
	}

	return diagnostics
}

// IntrospectColumns returns column names for a DataSource or DataSet using the shared session.
func (s *State) IntrospectColumns(ctx context.Context, name string) ([]string, error) {
	s.mu.RLock()
	docs := s.documents
	s.mu.RUnlock()

	// Find the target document
	isDataSource := strings.HasPrefix(name, "$")
	lookupName := strings.TrimPrefix(name, "$")

	var targetDoc *config.Document
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
		if !isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
		}
	}

	if targetDoc == nil {
		return nil, fmt.Errorf("document not found: %s", name)
	}

	// Build query (views already registered during refresh)
	var query string
	switch targetDoc.Kind {
	case "DataSource":
		query = fmt.Sprintf("SELECT * FROM %q", targetDoc.Name)
	case "DataSet":
		var payload struct {
			Spec struct {
				Query string `json:"query"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(targetDoc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("parse dataset spec: %w", err)
		}
		if payload.Spec.Query == "" {
			return nil, fmt.Errorf("dataset missing query")
		}
		query = strings.TrimSuffix(strings.TrimSpace(payload.Spec.Query), ";")
	default:
		return nil, fmt.Errorf("unsupported kind: %s", targetDoc.Kind)
	}

	schemaQuery := fmt.Sprintf("SELECT * FROM (%s) AS _schema LIMIT 0", query)
	rows, err := s.session.DB().QueryContext(ctx, schemaQuery)
	if err != nil {
		return nil, fmt.Errorf("query schema: %w", err)
	}
	defer rows.Close()

	return rows.Columns()
}

// QueryRows returns preview rows for a DataSource or DataSet using the shared session.
func (s *State) QueryRows(ctx context.Context, name string, limit int) (columns []string, rowData []map[string]any, truncated bool, kind string, err error) {
	s.mu.RLock()
	docs := s.documents
	s.mu.RUnlock()

	isDataSource := strings.HasPrefix(name, "$")
	lookupName := strings.TrimPrefix(name, "$")

	var targetDoc *config.Document
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
		if !isDataSource && doc.Kind == "DataSource" {
			targetDoc = doc
		}
	}

	if targetDoc == nil {
		return nil, nil, false, "", fmt.Errorf("document not found: %s", name)
	}

	var query string
	switch targetDoc.Kind {
	case "DataSource":
		query = fmt.Sprintf("SELECT * FROM %q LIMIT %d", targetDoc.Name, limit+1)
	case "DataSet":
		var payload struct {
			Spec struct {
				Query string `json:"query"`
				Prql  string `json:"prql"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(targetDoc.Raw, &payload); err != nil {
			return nil, nil, false, "", fmt.Errorf("parse dataset spec: %w", err)
		}
		switch {
		case payload.Spec.Prql != "":
			query = fmt.Sprintf("SELECT * FROM (%s) AS _preview LIMIT %d", payload.Spec.Prql, limit+1)
		case payload.Spec.Query != "":
			sqlQuery := strings.TrimSuffix(strings.TrimSpace(payload.Spec.Query), ";")
			query = fmt.Sprintf("SELECT * FROM (%s) AS _preview LIMIT %d", sqlQuery, limit+1)
		default:
			return nil, nil, false, "", fmt.Errorf("dataset has no query or prql")
		}
	default:
		return nil, nil, false, "", fmt.Errorf("unsupported kind: %s", targetDoc.Kind)
	}

	rows, err := s.session.DB().QueryContext(ctx, query)
	if err != nil {
		return nil, nil, false, "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, false, "", fmt.Errorf("get columns: %w", err)
	}

	var results []map[string]any
	values := make([]any, len(cols))
	valuePtrs := make([]any, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	rowCount := 0
	for rows.Next() {
		rowCount++
		if rowCount > limit {
			break
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, nil, false, "", fmt.Errorf("scan row: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = normalizeValue(values[i])
		}
		results = append(results, row)
	}

	if results == nil {
		results = []map[string]any{}
	}

	return cols, results, rowCount > limit, targetDoc.Kind, nil
}

// BuildGraph computes the dependency graph from cached documents.
func (s *State) BuildGraph(ctx context.Context) (*graph.Graph, error) {
	s.mu.RLock()
	docs := s.documents
	s.mu.RUnlock()

	return graph.Build(ctx, docs)
}

// normalizeValue converts database values to JSON-serializable types.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		var jsonVal any
		if err := json.Unmarshal(val, &jsonVal); err == nil {
			return jsonVal
		}
		return string(val)
	default:
		return val
	}
}

// parseValidationError converts a validation error into Diagnostic entries.
func parseValidationError(err error) []Diagnostic {
	var diagnostics []Diagnostic
	errStr := err.Error()

	var schemaErr *spec.SchemaValidationError
	if errors.As(err, &schemaErr) {
		for _, se := range schemaErr.Errors {
			diagnostics = append(diagnostics, Diagnostic{
				File:     schemaErr.File,
				Position: schemaErr.DocPosition,
				Line:     se.Line,
				Column:   se.Column,
				Severity: "error",
				Message:  se.Description,
				Field:    se.Field,
				Code:     "schema-validation",
			})
		}
		return diagnostics
	}

	file, position, message := parseFileError(errStr)
	if file != "" {
		diagnostics = append(diagnostics, Diagnostic{
			File:     file,
			Position: position,
			Severity: "error",
			Message:  message,
			Code:     "validation-error",
		})
	} else {
		diagnostics = append(diagnostics, Diagnostic{
			Severity: "error",
			Message:  errStr,
			Code:     "validation-error",
		})
	}
	return diagnostics
}

// parseFileError attempts to extract file path and position from error messages.
func parseFileError(errStr string) (file string, position int, message string) {
	parts := strings.SplitN(errStr, " document ", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		for _, prefix := range []string{"decode ", "read ", "validate ", "marshal ", "header "} {
			file = strings.TrimPrefix(file, prefix)
		}
		rest := parts[1]
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
			_, _ = fmt.Sscanf(posStr, "%d", &position)
		}
		return file, position, message
	}

	parts = strings.SplitN(errStr, " #", 2)
	if len(parts) == 2 {
		file = strings.TrimSpace(parts[0])
		rest := parts[1]
		var posStr string
		for i, c := range rest {
			if c >= '0' && c <= '9' {
				posStr += string(c)
			} else {
				message = strings.TrimSpace(rest[i:])
				if idx := strings.Index(message, ")"); idx > 0 {
					message = strings.TrimSpace(message[idx+1:])
				}
				break
			}
		}
		if posStr != "" {
			_, _ = fmt.Sscanf(posStr, "%d", &position)
		}
		return file, position, message
	}

	return "", 0, errStr
}
