package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/runtimecfg"
	"bino.bi/bino/pkg/duckdb"
)

// State holds the shared daemon state with cached documents and diagnostics.
type State struct {
	mu            sync.RWMutex
	projectRoot   string
	session       *duckdb.Session
	documents     []config.Document
	diagnostics   []Diagnostic
	lastIndexAt   time.Time
	logger        logx.Logger
	tempDir       string
	kindProvider  config.KindProvider
	pluginLinters lint.PluginLinterRegistry
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
	docs, err := config.LoadDirWithOptions(ctx, s.projectRoot, config.LoadOptions{Lenient: true, KindProvider: s.kindProvider})
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

// SetKindProvider sets the KindProvider for plugin kind validation.
func (s *State) SetKindProvider(kp config.KindProvider) {
	s.kindProvider = kp
}

// SetPluginLinters sets the plugin linter registry for validation.
func (s *State) SetPluginLinters(linters lint.PluginLinterRegistry) {
	s.pluginLinters = linters
}

// validateDocs runs full validation and returns diagnostics.
// Must be called with s.mu held.
func (s *State) validateDocs(ctx context.Context) []Diagnostic {
	dir := s.projectRoot

	// Load documents in strict mode to catch schema errors. Errors are
	// collected per document so every schema issue is reported, not just
	// the first one. ValidateDocuments runs inside the loader and its
	// failures land in loadErrs as well.
	var loadErrs []error
	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: false, KindProvider: s.kindProvider, CollectErrors: &loadErrs, SkipForeign: true})
	if err != nil {
		loadErrs = append(loadErrs, err)
	}
	diagnostics := make([]Diagnostic, 0, len(loadErrs))
	for _, loadErr := range loadErrs {
		diagnostics = append(diagnostics, parseValidationError(loadErr)...)
	}
	if len(loadErrs) > 0 {
		// Use lenient docs so downstream checks still see schema-invalid documents
		docs, _ = config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: true, KindProvider: s.kindProvider})
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

	// Run lint rules. DocumentsFromConfig carries metadata.params too — the
	// ref-params rule is inert without the declarations.
	lintDocs := lint.DocumentsFromConfig(docs)
	runner := lint.NewDefaultRunner()
	findings := runner.Run(ctx, lintDocs)
	if s.pluginLinters != nil {
		pluginFindings := lint.RunPluginLinters(ctx, lintDocs, s.pluginLinters)
		findings = append(findings, pluginFindings...)
	}
	for _, f := range findings {
		sev := f.Severity
		if sev == "" {
			sev = "warning"
		}
		diagnostics = append(diagnostics, Diagnostic{
			File:     f.File,
			Position: f.DocIdx,
			Line:     f.Line,
			Column:   f.Column,
			Severity: sev,
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
		ContinueOnQueryError:     true,
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

// ValidateDraft validates in-memory manifest YAML (one or more documents)
// against the schema and constraints, without touching the project on disk. It
// reports schema-validation and constraint diagnostics for the draft only; it
// does not run project-wide lint rules (which assume the full document set and
// would produce false positives against a single draft). This is the agent's
// pre-write guardrail.
func (s *State) ValidateDraft(ctx context.Context, yamlBytes []byte) ([]Diagnostic, error) {
	// Foreign buffers (docker-compose etc. — the editor attaches to every YAML
	// file) validate to nothing, and skip the per-keystroke strict load
	// entirely. Empty buffers are foreign too: a brand-new file must not open
	// with a wall of missing-property errors.
	if !s.draftIsBino(yamlBytes) {
		return []Diagnostic{}, nil
	}

	tmpDir, err := os.MkdirTemp("", "bino-draft-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	draftPath := filepath.Join(tmpDir, "draft.yaml")
	if err := os.WriteFile(draftPath, yamlBytes, 0o600); err != nil {
		return nil, fmt.Errorf("write draft: %w", err)
	}

	// Strict load collects every schema/constraint error per document. The temp
	// dir is outside the project, so this neither touches project files nor
	// triggers the watcher.
	var loadErrs []error
	_, _ = config.LoadDirWithOptions(ctx, tmpDir, config.LoadOptions{
		Lenient:       false,
		KindProvider:  s.kindProvider,
		CollectErrors: &loadErrs,
	})

	diagnostics := make([]Diagnostic, 0, len(loadErrs))
	for _, loadErr := range loadErrs {
		for _, d := range parseValidationError(loadErr) {
			d.File = "<draft>" // the temp path is meaningless to callers
			// Defense in depth for error shapes the parsers miss: never leak the
			// temp path into an editor-visible message.
			d.Message = strings.ReplaceAll(d.Message, draftPath, "<draft>")
			d.Message = strings.ReplaceAll(d.Message, tmpDir, "<draft>")
			diagnostics = append(diagnostics, d)
		}
	}
	return diagnostics, nil
}

// draftIsBino reports whether any document in a draft buffer identifies as a
// bino manifest (apiVersion prefix, or a known kind while the header is still
// being typed). On a syntax-broken buffer it falls back to a substring sniff,
// so a broken bino manifest still gets its yaml-syntax diagnostic while a
// broken docker-compose stays silent.
func (s *State) draftIsBino(yamlBytes []byte) bool {
	dec := yaml.NewDecoder(bytes.NewReader(yamlBytes))
	for {
		var head struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
		}
		err := dec.Decode(&head)
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			if bytes.Contains(yamlBytes, []byte("bino.bi/")) {
				return true
			}
			return sniffKnownKind(yamlBytes, s.kindProvider)
		}
		if config.IsBinoHeader(head.APIVersion, head.Kind, s.kindProvider) {
			return true
		}
	}
}

// sniffKnownKind scans a syntax-broken buffer line-wise for a `kind:` whose
// value bino recognizes — a half-typed bino manifest must keep its yaml-syntax
// diagnostic even before apiVersion exists.
func sniffKnownKind(yamlBytes []byte, kp config.KindProvider) bool {
	for line := range strings.SplitSeq(string(yamlBytes), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "kind:")
		if !ok {
			continue
		}
		kind := strings.Trim(strings.TrimSpace(rest), `"'`)
		if kind != "" && config.IsBinoHeader("", kind, kp) {
			return true
		}
	}
	return false
}

// IntrospectSource probes a not-yet-registered data source described by specJSON
// (a bare DataSource spec object) and returns its columns, a sample of rows,
// Excel sheet names, and detected CSV options. It opens a fresh ephemeral
// session so introspection never pollutes the shared one (matching the daemon's
// introspect-draft handler and lsp-helper).
func (s *State) IntrospectSource(ctx context.Context, specJSON json.RawMessage, sheet string, limit int) (*datasource.ProbeResult, error) {
	cfg := runtimecfg.Current()
	if limit <= 0 {
		limit = 100
	}
	if cfg.MaxQueryRows > 0 && limit > cfg.MaxQueryRows {
		limit = cfg.MaxQueryRows
	}
	timeout := cfg.MaxQueryDuration
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		return nil, err
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	return datasource.Probe(ctx, session, datasource.ProbeRequest{
		SpecJSON: specJSON,
		BaseDir:  s.ProjectRoot(),
		Docs:     s.Documents(),
		Sheet:    sheet,
		Limit:    limit,
	})
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
				Hint:     spec.Hint(se),
			})
		}
		return diagnostics
	}

	if d, ok := parseYAMLSyntaxError(errStr); ok {
		return []Diagnostic{d}
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

// parseYAMLSyntaxError destructures the loader's decode failure
// ("decode <path>: yaml: line N: <msg>", or without a line) into a positioned
// diagnostic. Splitting on ": yaml: " keeps Windows drive colons in the path
// intact; a yaml.TypeError embeds several "line N:" fragments — the first one
// anchors the diagnostic and the full message is kept.
func parseYAMLSyntaxError(errStr string) (Diagnostic, bool) {
	rest, ok := strings.CutPrefix(errStr, "decode ")
	if !ok {
		return Diagnostic{}, false
	}
	path, tail, found := strings.Cut(rest, ": yaml: ")
	if !found {
		return Diagnostic{}, false
	}
	msg := tail
	line := 0
	if idx := strings.Index(tail, "line "); idx >= 0 {
		digits := tail[idx+len("line "):]
		n := 0
		for n < len(digits) && digits[n] >= '0' && digits[n] <= '9' {
			n++
		}
		if n > 0 {
			line, _ = strconv.Atoi(digits[:n])
			if idx == 0 {
				msg = strings.TrimPrefix(digits[n:], ": ")
			}
		}
	}
	return Diagnostic{
		File:     path,
		Line:     line,
		Severity: "error",
		Message:  "YAML syntax error: " + msg,
		Code:     "yaml-syntax",
	}, true
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
