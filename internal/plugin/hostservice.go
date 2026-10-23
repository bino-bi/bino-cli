package plugin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	pluginv1 "github.com/bino-bi/bino-plugin-sdk/proto/v1"

	"bino.bi/bino/pkg/duckdb"
)

// BinoHostServer implements the BinoHost gRPC service that plugins can call
// back to for querying DuckDB, accessing documents, and retrieving dataset results.
//
// State is set via setter methods as the pipeline progresses. All methods are
// safe for concurrent access.
type BinoHostServer struct {
	pluginv1.UnimplementedBinoHostServer

	mu       sync.RWMutex
	docs     []DocumentPayload
	datasets []DatasetPayload

	// viewDir and views hold the dataset files QueryDuckDB reads. They are
	// written by the first QueryDuckDB after SetDatasets, so nothing touches
	// the disk when no plugin queries.
	viewDir string
	views   []datasetView
	// viewUsers counts the running queries per dataset dir. A dir dropped
	// while a query reads it is removed by the last of those queries.
	viewUsers map[string]int

	// duckDBOpener creates a new DuckDB session for plugin queries.
	// The caller is responsible for closing the session.
	duckDBOpener func(ctx context.Context) (*duckdb.Session, error)
}

// NewBinoHostServer creates a host service with no initial state.
func NewBinoHostServer() *BinoHostServer {
	return &BinoHostServer{}
}

// SetDocuments updates the current set of loaded documents.
func (h *BinoHostServer) SetDocuments(docs []DocumentPayload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.docs = docs
}

// SetDatasets updates the current set of executed dataset results.
func (h *BinoHostServer) SetDatasets(datasets []DatasetPayload) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.datasets = datasets
	h.dropViewsLocked()
}

// Close removes the dataset files written for QueryDuckDB. The host stays
// usable: the next QueryDuckDB writes them again.
func (h *BinoHostServer) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropViewsLocked()
}

func (h *BinoHostServer) dropViewsLocked() {
	if h.viewDir != "" && h.viewUsers[h.viewDir] == 0 {
		_ = os.RemoveAll(h.viewDir)
	}
	h.viewDir = ""
	h.views = nil
}

// datasetView is one dataset as QueryDuckDB exposes it. file is empty for a
// dataset without rows.
type datasetView struct {
	name string
	file string
}

// datasetViews returns the views for the current datasets and writes their
// files on first use. The caller must pass the returned dir to
// releaseViews once the query is done.
func (h *BinoHostServer) datasetViews() ([]datasetView, string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.views == nil && len(h.datasets) > 0 {
		if err := h.writeViewsLocked(); err != nil {
			return nil, "", err
		}
	}
	if h.viewDir != "" {
		if h.viewUsers == nil {
			h.viewUsers = make(map[string]int)
		}
		h.viewUsers[h.viewDir]++
	}
	return h.views, h.viewDir, nil
}

func (h *BinoHostServer) releaseViews(dir string) {
	if dir == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.viewUsers[dir]--
	if h.viewUsers[dir] > 0 {
		return
	}
	delete(h.viewUsers, dir)
	if dir != h.viewDir {
		_ = os.RemoveAll(dir)
	}
}

func (h *BinoHostServer) writeViewsLocked() error {
	dir, err := os.MkdirTemp("", "bino-plugin-datasets-")
	if err != nil {
		return err
	}
	views := make([]datasetView, 0, len(h.datasets))
	for i, ds := range h.datasets {
		v := datasetView{name: ds.Name}
		if rows := bytes.TrimSpace(ds.JSONRows); len(rows) > 0 && !bytes.Equal(rows, []byte("[]")) {
			// Files are named by index so any dataset name is safe on disk.
			v.file = filepath.Join(dir, fmt.Sprintf("%d.json", i))
			if err := os.WriteFile(v.file, rows, 0o600); err != nil {
				_ = os.RemoveAll(dir)
				return err
			}
		}
		views = append(views, v)
	}
	h.viewDir = dir
	h.views = views
	return nil
}

func (v datasetView) createSQL() string {
	ident := `"` + strings.ReplaceAll(v.name, `"`, `""`) + `"`
	if v.file == "" {
		return "CREATE VIEW " + ident + " AS SELECT 1 WHERE 1=0"
	}
	return "CREATE VIEW " + ident + " AS SELECT * FROM read_json_auto('" + strings.ReplaceAll(v.file, "'", "''") + "')"
}

// SetDuckDBOpener sets the function used to create DuckDB sessions for plugin queries.
func (h *BinoHostServer) SetDuckDBOpener(opener func(ctx context.Context) (*duckdb.Session, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.duckDBOpener = opener
}

// SetDefaultDuckDBOpener configures the host service to create fresh DuckDB sessions
// using default options. This is the standard setup for build, preview, and serve commands.
func (h *BinoHostServer) SetDefaultDuckDBOpener() {
	h.SetDuckDBOpener(func(ctx context.Context) (*duckdb.Session, error) {
		opts, err := duckdb.DefaultOptions()
		if err != nil {
			return nil, err
		}
		return duckdb.OpenSession(ctx, opts)
	})
}

// QueryDuckDB executes a SQL query in a throwaway DuckDB session.
//
// The session holds one view per executed DataSet, named by metadata.name.
// DataSets are set from the post-dataset-execute hook onward, so they are
// visible in RenderComponent and later hooks. Before that hook (for example
// in CollectDataSource) build shows none, but preview and serve can still
// hold the DataSets of the previous render. In preview, an embed or
// presentation render running at the same time can replace them at any point.
// DataSources are never visible. A dataset whose view cannot be created is
// skipped with one WARNING diagnostic; the query still runs. Nothing the
// query changes outlives the call.
//
// Error contract (deliberate, load-bearing): a failing query does NOT return
// a Go error. It returns a *QueryResponse whose Diagnostics carry a single
// ERROR entry (source "host", stage "query") with err == nil, so the failure
// crosses the plugin boundary as data the plugin can inspect and report.
// A non-nil error is reserved for host-side faults: no DuckDB opener
// configured, a session that fails to open, or result serialization failure.
// Callers must therefore check Diagnostics, not just err.
func (h *BinoHostServer) QueryDuckDB(ctx context.Context, req *pluginv1.QueryRequest) (*pluginv1.QueryResponse, error) {
	h.mu.RLock()
	opener := h.duckDBOpener
	h.mu.RUnlock()

	if opener == nil {
		return nil, fmt.Errorf("DuckDB is not available")
	}

	session, err := opener(ctx)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB session: %w", err)
	}
	defer session.Close() //nolint:errcheck // teardown of an ephemeral in-memory session

	views, viewDir, err := h.datasetViews()
	if err != nil {
		return nil, fmt.Errorf("write dataset files: %w", err)
	}
	defer h.releaseViews(viewDir)

	db := session.DB()
	var diags []*pluginv1.Diagnostic
	for _, v := range views {
		if _, err := db.ExecContext(ctx, v.createSQL()); err != nil {
			diags = append(diags, &pluginv1.Diagnostic{
				Source:   "host",
				Stage:    "query",
				Message:  fmt.Sprintf("dataset %q is not queryable: %v", v.name, err),
				Severity: pluginv1.Severity_WARNING,
			})
		}
	}

	rows, err := db.QueryContext(ctx, req.GetSql())
	if err != nil {
		return &pluginv1.QueryResponse{
			Diagnostics: append(diags, &pluginv1.Diagnostic{
				Source:   "host",
				Stage:    "query",
				Message:  err.Error(),
				Severity: pluginv1.Severity_ERROR,
			}),
		}, nil
	}
	defer rows.Close()

	jsonRows, columns, err := rowsToJSON(rows)
	if err != nil {
		return nil, fmt.Errorf("serialize query results: %w", err)
	}

	return &pluginv1.QueryResponse{
		JsonRows:    jsonRows,
		Columns:     columns,
		Diagnostics: diags,
	}, nil
}

// GetDocument returns a single document by kind and name.
func (h *BinoHostServer) GetDocument(_ context.Context, req *pluginv1.GetDocumentRequest) (*pluginv1.GetDocumentResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, d := range h.docs {
		if d.Name == req.GetName() && (req.GetKind() == "" || d.Kind == req.GetKind()) {
			return &pluginv1.GetDocumentResponse{
				Found: true,
				Document: &pluginv1.DocumentPayload{
					File:     d.File,
					Position: int32(d.Position), //nolint:gosec // G115: document position is always small
					Kind:     d.Kind,
					Name:     d.Name,
					Raw:      d.Raw,
				},
			}, nil
		}
	}

	return &pluginv1.GetDocumentResponse{Found: false}, nil
}

// GetDatasetResult returns a dataset result by name.
func (h *BinoHostServer) GetDatasetResult(_ context.Context, req *pluginv1.GetDatasetResultRequest) (*pluginv1.GetDatasetResultResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, ds := range h.datasets {
		if ds.Name == req.GetName() {
			return &pluginv1.GetDatasetResultResponse{
				Found: true,
				Dataset: &pluginv1.DatasetPayload{
					Name:     ds.Name,
					JsonRows: ds.JSONRows,
					Columns:  ds.Columns,
				},
			}, nil
		}
	}

	return &pluginv1.GetDatasetResultResponse{Found: false}, nil
}

// ListDocuments returns all documents, optionally filtered by kind.
func (h *BinoHostServer) ListDocuments(_ context.Context, req *pluginv1.ListDocumentsRequest) (*pluginv1.ListDocumentsResponse, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	kindFilter := req.GetKindFilter()
	resp := &pluginv1.ListDocumentsResponse{}
	for _, d := range h.docs {
		if kindFilter != "" && d.Kind != kindFilter {
			continue
		}
		resp.Documents = append(resp.Documents, &pluginv1.DocumentPayload{
			File:     d.File,
			Position: int32(d.Position), //nolint:gosec // G115: document position is always small
			Kind:     d.Kind,
			Name:     d.Name,
			Raw:      d.Raw,
		})
	}
	return resp, nil
}

// rowsToJSON converts sql.Rows to a JSON array and returns column names.
func rowsToJSON(rows *sql.Rows) (jsonData []byte, columns []string, err error) {
	columns, err = rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]any, len(columns))
		for i, col := range columns {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	if results == nil {
		results = []map[string]any{}
	}
	data, err := json.Marshal(results)
	if err != nil {
		return nil, nil, err
	}
	return data, columns, nil
}
