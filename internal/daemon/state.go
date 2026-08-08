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
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/diagnostics"
	"bino.bi/bino/internal/report/graph"
	"bino.bi/bino/internal/report/lint"
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
	engineCompat  func(dir string) (Diagnostic, bool)
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
	diags := s.validateDocs(ctx)

	s.documents = docs
	s.diagnostics = diags
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

// SetEngineCompat sets the engine-version compatibility check consulted during
// validation. The check lives in the CLI layer (it knows the engine cache), so
// it is injected here like the kind provider.
func (s *State) SetEngineCompat(check func(dir string) (Diagnostic, bool)) {
	s.engineCompat = check
}

// validateDocs runs full validation and returns diagnostics.
// Must be called with s.mu held.
func (s *State) validateDocs(ctx context.Context) []Diagnostic {
	return diagnostics.Collect(ctx, s.projectRoot, diagnostics.Options{
		KindProvider:  s.kindProvider,
		SkipForeign:   true,
		PluginLinters: s.pluginLinters,
		EngineCompat:  s.engineCompat,
	})
}

// ValidateWithQueries runs validation including query execution and returns diagnostics.
func (s *State) ValidateWithQueries(ctx context.Context) []Diagnostic {
	s.mu.RLock()
	docs := s.documents
	dir := s.projectRoot
	s.mu.RUnlock()

	diags := s.Diagnostics()

	if len(docs) == 0 {
		return diags
	}

	return append(diags, diagnostics.RunQueryValidation(ctx, dir, docs)...)
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
	_, _ = config.LoadDirWithOptions(ctx, tmpDir, config.LoadOptions{ //nolint:errcheck // issues arrive via CollectErrors; the return would duplicate them
		Lenient:       false,
		KindProvider:  s.kindProvider,
		CollectErrors: &loadErrs,
	})

	diags := make([]Diagnostic, 0, len(loadErrs))
	for _, loadErr := range loadErrs {
		for _, d := range diagnostics.FromLoadError(loadErr) {
			d.File = "<draft>" // the temp path is meaningless to callers
			// Defense in depth for error shapes the parsers miss: never leak the
			// temp path into an editor-visible message.
			d.Message = strings.ReplaceAll(d.Message, draftPath, "<draft>")
			d.Message = strings.ReplaceAll(d.Message, tmpDir, "<draft>")
			diags = append(diags, d)
		}
	}
	return diags, nil
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
