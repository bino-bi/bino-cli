package explorer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"bino.bi/bino/internal/runtimecfg"
)

// Handler returns an http.Handler that serves all explorer endpoints under
// the /__explorer/ prefix.
func Handler(session *Session) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__explorer/metadata", handleMetadata(session))
	mux.HandleFunc("POST /__explorer/query", handleQuery(session))
	mux.HandleFunc("POST /__explorer/summarize", handleSummarize(session))
	return mux
}

// metadataResponse is the JSON response for the metadata endpoint.
type metadataResponse struct {
	Sources  []sourceInfo  `json:"sources"`
	Datasets []datasetInfo `json:"datasets"`
}

type sourceInfo struct {
	Name    string       `json:"name"`
	Type    string       `json:"type"`
	Columns []columnInfo `json:"columns"`
}

type datasetInfo struct {
	Name       string   `json:"name"`
	DependsOn  []string `json:"dependsOn,omitempty"`
}

type columnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// queryRequest is the JSON body for the query endpoint.
type queryRequest struct {
	SQL    string `json:"sql"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// queryResponse is the JSON response for the query endpoint.
type queryResponse struct {
	Columns    []columnInfo    `json:"columns"`
	Rows       [][]any         `json:"rows"`
	TotalRows  any             `json:"totalRows"` // int or "unknown"
	DurationMs int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
}

// summarizeRequest is the JSON body for the summarize endpoint.
type summarizeRequest struct {
	SQL  string `json:"sql,omitempty"`
	Name string `json:"name,omitempty"`
}

func handleMetadata(session *Session) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		docs := session.Docs()
		resp := metadataResponse{
			Sources:  make([]sourceInfo, 0),
			Datasets: make([]datasetInfo, 0),
		}

		for _, doc := range docs {
			switch doc.Kind {
			case "DataSource":
				si := sourceInfo{Name: doc.Name, Type: extractSourceType(doc.Raw)}
				// Fetch column info from the registered view
				cols, err := fetchColumns(r.Context(), session, doc.Name)
				if err == nil {
					si.Columns = cols
				}
				resp.Sources = append(resp.Sources, si)
			case "DataSet":
				di := datasetInfo{Name: doc.Name}
				di.DependsOn = extractDependsOn(doc.Raw)
				resp.Datasets = append(resp.Datasets, di)
			}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleQuery(session *Session) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req queryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, queryResponse{Error: "invalid request body"})
			return
		}

		sqlText := strings.TrimSpace(req.SQL)
		if sqlText == "" {
			writeJSON(w, http.StatusBadRequest, queryResponse{Error: "sql is required"})
			return
		}

		if isWriteOperation(sqlText) {
			writeJSON(w, http.StatusBadRequest, queryResponse{Error: "write operations are not allowed"})
			return
		}

		limit := req.Limit
		if limit <= 0 || limit > 1000 {
			limit = 50
		}
		offset := max(req.Offset, 0)

		cfg := runtimecfg.Current()
		timeout := cfg.MaxQueryDuration
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		var resp queryResponse
		start := time.Now()

		err := session.WithDB(func(db *sql.DB) error {
			// Run paginated query and count in sequence (same connection)
			paginatedSQL := fmt.Sprintf("SELECT * FROM (%s) AS _q LIMIT %d OFFSET %d", sqlText, limit, offset)
			rows, err := db.QueryContext(ctx, paginatedSQL)
			if err != nil {
				return err
			}
			defer rows.Close()

			colTypes, err := rows.ColumnTypes()
			if err != nil {
				return err
			}

			resp.Columns = make([]columnInfo, len(colTypes))
			for i, ct := range colTypes {
				resp.Columns[i] = columnInfo{
					Name: ct.Name(),
					Type: ct.DatabaseTypeName(),
				}
			}

			resp.Rows = make([][]any, 0)
			for rows.Next() {
				vals := make([]any, len(colTypes))
				ptrs := make([]any, len(colTypes))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return err
				}
				normalized := make([]any, len(vals))
				for i, v := range vals {
					normalized[i] = normalizeValue(v)
				}
				resp.Rows = append(resp.Rows, normalized)
			}
			if err := rows.Err(); err != nil {
				return err
			}

			// Count total rows (5s timeout, falls back to "unknown")
			countCtx, countCancel := context.WithTimeout(ctx, 5*time.Second)
			defer countCancel()
			countSQL := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS _q", sqlText)
			var totalRows int64
			if err := db.QueryRowContext(countCtx, countSQL).Scan(&totalRows); err != nil {
				resp.TotalRows = "unknown"
			} else {
				resp.TotalRows = totalRows
			}

			return nil
		})

		resp.DurationMs = time.Since(start).Milliseconds()

		if err != nil {
			resp.Error = err.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handleSummarize(session *Session) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req summarizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, queryResponse{Error: "invalid request body"})
			return
		}

		// Build the summarize SQL
		var summarizeSQL string
		if req.Name != "" {
			summarizeSQL = fmt.Sprintf(`SUMMARIZE SELECT * FROM "%s"`, req.Name)
		} else if req.SQL != "" {
			sqlText := strings.TrimSpace(req.SQL)
			if isWriteOperation(sqlText) {
				writeJSON(w, http.StatusBadRequest, queryResponse{Error: "write operations are not allowed"})
				return
			}
			summarizeSQL = fmt.Sprintf("SUMMARIZE (%s)", sqlText)
		} else {
			writeJSON(w, http.StatusBadRequest, queryResponse{Error: "name or sql is required"})
			return
		}

		cfg := runtimecfg.Current()
		timeout := cfg.MaxQueryDuration
		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		var resp queryResponse
		start := time.Now()

		err := session.WithDB(func(db *sql.DB) error {
			rows, err := db.QueryContext(ctx, summarizeSQL)
			if err != nil {
				return err
			}
			defer rows.Close()

			colTypes, err := rows.ColumnTypes()
			if err != nil {
				return err
			}

			resp.Columns = make([]columnInfo, len(colTypes))
			for i, ct := range colTypes {
				resp.Columns[i] = columnInfo{
					Name: ct.Name(),
					Type: ct.DatabaseTypeName(),
				}
			}

			resp.Rows = make([][]any, 0)
			for rows.Next() {
				vals := make([]any, len(colTypes))
				ptrs := make([]any, len(colTypes))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return err
				}
				normalized := make([]any, len(vals))
				for i, v := range vals {
					normalized[i] = normalizeValue(v)
				}
				resp.Rows = append(resp.Rows, normalized)
			}
			return rows.Err()
		})

		resp.DurationMs = time.Since(start).Milliseconds()

		if err != nil {
			resp.Error = err.Error()
			writeJSON(w, http.StatusOK, resp)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

// fetchColumns queries column information for a view by selecting zero rows.
func fetchColumns(ctx context.Context, session *Session, viewName string) ([]columnInfo, error) {
	var cols []columnInfo
	err := session.WithDB(func(db *sql.DB) error {
		query := fmt.Sprintf(`SELECT * FROM "%s" LIMIT 0`, viewName)
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()

		colTypes, err := rows.ColumnTypes()
		if err != nil {
			return err
		}

		cols = make([]columnInfo, len(colTypes))
		for i, ct := range colTypes {
			cols[i] = columnInfo{
				Name: ct.Name(),
				Type: ct.DatabaseTypeName(),
			}
		}
		return nil
	})
	return cols, err
}

// writeOperationPattern detects SQL statements that modify data.
var writeOperationPattern = regexp.MustCompile(`(?i)^\s*(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|TRUNCATE|REPLACE|MERGE|COPY|ATTACH|DETACH|GRANT|REVOKE)\b`)

func isWriteOperation(sql string) bool {
	return writeOperationPattern.MatchString(sql)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// normalizeValue converts sql.DB scan results into JSON-friendly values.
func normalizeValue(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format(time.RFC3339)
	default:
		return val
	}
}

// extractSourceType extracts the "type" field from a DataSource raw JSON.
func extractSourceType(raw json.RawMessage) string {
	var spec struct {
		Spec struct {
			Type string `json:"type"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return ""
	}
	return spec.Spec.Type
}

// extractDependsOn extracts datasource dependencies from a DataSet raw JSON.
func extractDependsOn(raw json.RawMessage) []string {
	var spec struct {
		Spec struct {
			Source  string   `json:"source"`
			Sources []string `json:"sources"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil
	}
	var deps []string
	if spec.Spec.Source != "" {
		deps = append(deps, spec.Spec.Source)
	}
	deps = append(deps, spec.Spec.Sources...)
	return deps
}

