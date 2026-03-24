package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

// QueryDuckDB executes a SQL query against the host's DuckDB engine.
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
	defer session.Close()

	db := session.DB()
	rows, err := db.QueryContext(ctx, req.GetSql())
	if err != nil {
		return &pluginv1.QueryResponse{
			Diagnostics: []*pluginv1.Diagnostic{{
				Source:   "host",
				Stage:    "query",
				Message:  err.Error(),
				Severity: pluginv1.Severity_ERROR,
			}},
		}, nil
	}
	defer rows.Close()

	jsonRows, columns, err := rowsToJSON(rows)
	if err != nil {
		return nil, fmt.Errorf("serialize query results: %w", err)
	}

	return &pluginv1.QueryResponse{
		JsonRows: jsonRows,
		Columns:  columns,
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
