package datasource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/pkg/duckdb"
)

// PreviewRequest describes a not-yet-registered DataSource plus a DataSet SQL
// query to run against it. It is used by the wizard's "Preview dataset" action.
type PreviewRequest struct {
	// SpecJSON is the draft DataSource spec object, e.g. {"type":"csv","path":"data.csv"}.
	SpecJSON json.RawMessage
	// SourceName is the name the SQL's FROM clause references; the draft source is
	// registered as a view under this name before the query runs.
	SourceName string
	// SQL is the DataSet SELECT to execute (typically the wizard's generated, then
	// possibly hand-edited, query).
	SQL string
	// BaseDir resolves relative file paths in the spec.
	BaseDir string
	// Docs are sibling documents used to resolve ConnectionSecret references.
	Docs []config.Document
	// Sheet optionally overrides the Excel sheet to read.
	Sheet string
	// Limit caps the number of result rows returned (default 100 when <= 0).
	Limit int
}

// PreviewResult is a sample of the rows the DataSet SQL produces.
type PreviewResult struct {
	Columns   []ProbeColumn    `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated"`
}

// PreviewDataSet registers a draft DataSource as a view named req.SourceName on
// the session, then runs req.SQL against it and returns a sample of the result.
// Like Probe, it never touches the shared session — the caller passes a fresh,
// ephemeral session so the preview cannot pollute live state.
func PreviewDataSet(ctx context.Context, session *duckdb.Session, req PreviewRequest) (*PreviewResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	query := strings.TrimRight(strings.TrimSpace(req.SQL), "; \t\r\n")
	if query == "" {
		return nil, fmt.Errorf("sql is required")
	}
	name := strings.TrimSpace(req.SourceName)
	if name == "" {
		return nil, fmt.Errorf("sourceName is required")
	}

	spec, err := parseProbeSpec(req.SpecJSON)
	if err != nil {
		return nil, err
	}
	spec.Name = name
	spec.BaseDir = req.BaseDir
	if req.Sheet != "" {
		spec.Sheet = req.Sheet
	}
	if spec.Type == sourceTypeInline {
		return nil, fmt.Errorf("inline sources cannot be previewed")
	}

	db := session.DB()

	if ext := extensionForSource(spec.Type); ext != "" {
		if err := session.InstallAndLoadExtensions(ctx, []string{ext}); err != nil {
			return nil, fmt.Errorf("load %s extension: %w", ext, err)
		}
	}

	// Database sources need credentials + an ATTACH before the view can resolve.
	if attachName, attachSQL := buildAttachSQL(spec); attachSQL != "" {
		if _, _, err := LoadSecrets(ctx, db, req.Docs); err != nil {
			return nil, fmt.Errorf("load secrets: %w", err)
		}
		if _, err := db.ExecContext(ctx, attachSQL); err != nil {
			return nil, fmt.Errorf("attach %s: %w", attachName, err)
		}
	}

	// Register the draft source under its name so the DataSet SQL's FROM resolves.
	if err := createView(ctx, db, session, viewDef{name: name, spec: spec}); err != nil {
		return nil, fmt.Errorf("register datasource view: %w", err)
	}

	cols, rows, truncated, err := sampleAndDescribe(ctx, db, query, limit)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{Columns: cols, Rows: rows, Truncated: truncated}, nil
}
