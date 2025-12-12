package buildlog

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"bino.bi/bino/pkg/duckdb"
)

// JSONBuildLog represents the complete JSON build log structure.
type JSONBuildLog struct {
	RunID         string          `json:"run_id"`
	Started       time.Time       `json:"started"`
	Completed     time.Time       `json:"completed"`
	DurationMs    int64           `json:"duration_ms"`
	Workdir       string          `json:"workdir"`
	Documents     []DocumentEntry `json:"documents"`
	Artefacts     []ArtefactEntry `json:"artefacts"`
	Queries       []QueryEntry    `json:"queries"`
	ExecutionPlan []ExecutionStep `json:"execution_plan,omitempty"`
}

// DocumentEntry represents a document in the build log.
type DocumentEntry struct {
	File     string `json:"file"`
	Position int    `json:"position"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
}

// ArtefactEntry represents a generated artefact in the build log.
type ArtefactEntry struct {
	Name  string `json:"name"`
	PDF   string `json:"pdf"`
	Graph string `json:"graph,omitempty"`
}

// QueryEntry represents a query execution in the build log.
type QueryEntry struct {
	ID         string     `json:"id"`
	Query      string     `json:"query"`
	QueryType  string     `json:"query_type"`
	Dataset    string     `json:"dataset,omitempty"`
	Datasource string     `json:"datasource,omitempty"`
	StartTime  time.Time  `json:"start_time"`
	DurationMs int64      `json:"duration_ms"`
	RowCount   int        `json:"row_count"`
	Columns    []string   `json:"columns,omitempty"`
	CSV        *CSVResult `json:"csv,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// queryIDCounter is used to generate unique query IDs.
var queryIDCounter int

// resetQueryIDCounter resets the query ID counter (for testing).
func resetQueryIDCounter() {
	queryIDCounter = 0
}

// BuildQueryEntry converts a QueryExecMeta to a QueryEntry.
// If embedOpts.Enable is true and rows are available, CSV data is embedded.
func BuildQueryEntry(meta duckdb.QueryExecMeta, embedOpts EmbedOptions) QueryEntry {
	queryIDCounter++
	id := fmt.Sprintf("query-%03d", queryIDCounter)

	entry := QueryEntry{
		ID:         id,
		Query:      meta.Query,
		QueryType:  meta.QueryType,
		Dataset:    meta.Dataset,
		Datasource: meta.Datasource,
		StartTime:  meta.StartTime,
		DurationMs: meta.DurationMs,
		RowCount:   meta.RowCount,
		Columns:    meta.Columns,
		Error:      meta.Error,
	}

	// Build CSV if embedding is enabled and rows are available
	if embedOpts.Enable && len(meta.Columns) > 0 && len(meta.Rows) > 0 {
		entry.CSV = BuildCSV(meta.Columns, meta.Rows, embedOpts)
	}

	return entry
}

// WriteJSONBuildLog marshals the log to indented JSON and writes it to the specified path.
func WriteJSONBuildLog(path string, log *JSONBuildLog) error {
	if log == nil {
		return fmt.Errorf("log is nil")
	}

	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON build log: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write JSON build log to %s: %w", path, err)
	}

	return nil
}
