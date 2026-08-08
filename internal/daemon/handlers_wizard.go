package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/sqlgen"
	"bino.bi/bino/internal/runtimecfg"
	"bino.bi/bino/internal/version"
	"bino.bi/bino/pkg/duckdb"
)

// daemonCapabilities lists the optional endpoints this daemon build serves, so a
// client can detect a stale daemon (spawned from an older binary) and restart it.
var daemonCapabilities = []string{
	"introspect-draft", "typed-select", "preview-dataset", "dataset-schema",
	"registry-packages", "registry-search", "registry-info", "registry-events",
	"validate", "validate-draft",
}

// introspectDraftResponse mirrors datasource.ProbeResult plus the daemon version
// (for the client staleness handshake) and a non-fatal error string.
type introspectDraftResponse struct {
	Version     string                   `json:"version"`
	Columns     []datasource.ProbeColumn `json:"columns"`
	Sheets      []string                 `json:"sheets,omitempty"`
	SampleRows  []map[string]any         `json:"sampleRows"`
	Truncated   bool                     `json:"truncated"`
	DetectedCSV *datasource.DetectedCSV  `json:"detectedCsv,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

// handleIntrospectDraft introspects a not-yet-registered data source described
// by a partial spec. It runs on a fresh ephemeral DuckDB session so it never
// pollutes the shared session that serves /columns, /rows, validation and preview.
func (s *Server) handleIntrospectDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Spec    json.RawMessage `json:"spec"`
		BaseDir string          `json:"baseDir"`
		Sheet   string          `json:"sheet"`
		Limit   int             `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, introspectDraftResponse{Version: version.Version, Columns: []datasource.ProbeColumn{}, SampleRows: []map[string]any{}, Error: "invalid request: " + err.Error()})
		return
	}

	cfg := runtimecfg.Current()
	limit := req.Limit
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
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	baseDir := req.BaseDir
	if baseDir == "" {
		baseDir = s.state.ProjectRoot()
	}

	resp := introspectDraftResponse{Version: version.Version, Columns: []datasource.ProbeColumn{}, SampleRows: []map[string]any{}}

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	res, err := datasource.Probe(ctx, session, datasource.ProbeRequest{
		SpecJSON: req.Spec,
		BaseDir:  baseDir,
		Docs:     s.state.Documents(),
		Sheet:    req.Sheet,
		Limit:    limit,
	})
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}

	resp.Columns = res.Columns
	resp.Sheets = res.Sheets
	resp.SampleRows = res.SampleRows
	resp.Truncated = res.Truncated
	resp.DetectedCSV = res.DetectedCSV
	s.writeJSON(w, resp)
}

// handleTypedSelect generates a column-aware SELECT statement server-side so the
// wizard's live preview matches exactly what gets written to the DataSet manifest.
func (s *Server) handleTypedSelect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source   string          `json:"source"`
		Columns  []sqlgen.Column `json:"columns"`
		Pretty   bool            `json:"pretty"`
		CastMode string          `json:"castMode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, map[string]any{"version": version.Version, "error": "invalid request: " + err.Error()})
		return
	}
	sql, aliases := sqlgen.TypedSelect(req.Source, req.Columns, sqlgen.Options{
		Pretty:   req.Pretty,
		CastMode: sqlgen.ParseCastMode(req.CastMode),
	})
	s.writeJSON(w, map[string]any{"version": version.Version, "sql": sql, "aliases": aliases})
}

// previewDataSetResponse mirrors datasource.PreviewResult plus the daemon version
// and a non-fatal error string.
type previewDataSetResponse struct {
	Version   string                   `json:"version"`
	Columns   []datasource.ProbeColumn `json:"columns"`
	Rows      []map[string]any         `json:"rows"`
	Truncated bool                     `json:"truncated"`
	Error     string                   `json:"error,omitempty"`
}

// handlePreviewDataSet registers a draft DataSource on a fresh ephemeral session
// and runs the wizard's (possibly hand-edited) DataSet SQL against it, returning a
// sample of the result. Like introspect-draft it never touches the shared session.
func (s *Server) handlePreviewDataSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Spec       json.RawMessage `json:"spec"`
		SourceName string          `json:"sourceName"`
		SQL        string          `json:"sql"`
		BaseDir    string          `json:"baseDir"`
		Sheet      string          `json:"sheet"`
		Limit      int             `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSON(w, previewDataSetResponse{Version: version.Version, Columns: []datasource.ProbeColumn{}, Rows: []map[string]any{}, Error: "invalid request: " + err.Error()})
		return
	}

	cfg := runtimecfg.Current()
	limit := req.Limit
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
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	baseDir := req.BaseDir
	if baseDir == "" {
		baseDir = s.state.ProjectRoot()
	}

	resp := previewDataSetResponse{Version: version.Version, Columns: []datasource.ProbeColumn{}, Rows: []map[string]any{}}

	opts, err := duckdb.DefaultOptions()
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	res, err := datasource.PreviewDataSet(ctx, session, datasource.PreviewRequest{
		SpecJSON:   req.Spec,
		SourceName: req.SourceName,
		SQL:        req.SQL,
		BaseDir:    baseDir,
		Docs:       s.state.Documents(),
		Sheet:      req.Sheet,
		Limit:      limit,
	})
	if err != nil {
		resp.Error = err.Error()
		s.writeJSON(w, resp)
		return
	}
	resp.Columns = res.Columns
	resp.Rows = res.Rows
	resp.Truncated = res.Truncated
	s.writeJSON(w, resp)
}

// handleDatasetSchema returns the canonical dataset schema (standard columns) so
// the wizard's mapper renders the same set the CLI validates against.
func (s *Server) handleDatasetSchema(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, map[string]any{"version": version.Version, "columns": dataset.StandardColumns()})
}
