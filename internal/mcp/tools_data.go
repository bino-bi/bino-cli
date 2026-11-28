package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
)

type queryDataInput struct {
	SQL   string `json:"sql" jsonschema:"one DuckDB SELECT statement; it reads the project's DataSources and DataSets by name, e.g. SELECT region, sum(ac1) FROM sales GROUP BY region"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum rows to return (default 100, at most 1000)"`
}

type profileDataInput struct {
	Name       string `json:"name" jsonschema:"DataSet or DataSource name; prefix with '$' to force a DataSource"`
	SampleRows int    `json:"sample_rows,omitempty" jsonschema:"above this many rows the column statistics use a random sample of this size (default 100000)"`
}

// registerDataTools adds query_data and profile_data. They are read-only:
// each call runs on a DuckDB session of its own that can read only the
// project's data, so they are served in read-only mode too.
func (h *handlers) registerDataTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "query_data",
		Title:       "Query project data",
		Annotations: readOnlyTool("Query project data"),
		Description: "Run one DuckDB SELECT over the project's DataSources and DataSets, referenced by name (a DataSet whose name a DataSource also uses is not offered: the name means the DataSource). Returns columns with DuckDB types, rows as arrays, and truncated when more rows exist than limit or the result passes 2 MiB; cells over 4 KiB are cut (see notes). Table functions (read_csv, glob, ...), qualified names, DESCRIBE/SUMMARIZE/SHOW and every non-SELECT statement are refused. A failing query returns DuckDB's own message in error.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in queryDataInput) (*mcpsdk.CallToolResult, daemon.QueryDataResult, error) {
		st, err := h.state(ctx)
		if err != nil {
			return nil, daemon.QueryDataResult{}, err
		}
		return nil, st.QueryData(ctx, in.SQL, in.Limit), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "profile_data",
		Title:       "Profile a dataset",
		Annotations: readOnlyTool("Profile a dataset"),
		Description: "Profile a DataSet or DataSource: row count; per column the DuckDB type, min, max, approximate distinct count and null count (from a random sample when the data has more than sample_rows rows); the top 5 values of columns with few distinct values; for DATE and TIMESTAMP columns the range, the inferred step (day, weekday, week, month, quarter, year) and the missing periods. warnings lists things worth checking, such as a VARCHAR column whose values are all numbers. For a DataSource that reads a local CSV file, or a DataSet whose only DataSource does, csv holds the delimiter, header and date format DuckDB detects in the file, and warnings says when the declared options do not fit it: a missing or misread header row (columns named column0, column1, ...), another delimiter, dates the dateFormat does not parse. Fix those before writing a DataSet on the columns.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in profileDataInput) (*mcpsdk.CallToolResult, daemon.ProfileDataResult, error) {
		st, err := h.state(ctx)
		if err != nil {
			return nil, daemon.ProfileDataResult{}, err
		}
		return nil, st.ProfileData(ctx, in.Name, in.SampleRows), nil
	})
}
