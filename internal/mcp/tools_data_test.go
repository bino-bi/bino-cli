package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestQueryDataTool(t *testing.T) {
	cs := newTestClient(t)
	var out struct {
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"columns"`
		Rows      [][]any `json:"rows"`
		Truncated bool    `json:"truncated"`
		Error     string  `json:"error"`
	}
	callToolJSON(t, cs, "query_data", map[string]any{
		"sql": "SELECT count(*) AS n FROM revenue_by_region r JOIN revenue_data d USING (category)",
	}, &out)
	if out.Error != "" || len(out.Rows) != 1 || len(out.Columns) != 1 || out.Columns[0].Type != "BIGINT" {
		t.Fatalf("out = %+v", out)
	}

	out.Error = ""
	callToolJSON(t, cs, "query_data", map[string]any{"sql": "SELECT * FROM read_text('/etc/passwd')"}, &out)
	if !strings.Contains(out.Error, "read_text() is not allowed") {
		t.Fatalf("error = %q", out.Error)
	}
}

func TestProfileDataTool(t *testing.T) {
	cs := newTestClient(t)
	var out struct {
		RowCount int64 `json:"rowCount"`
		Columns  []struct {
			Name string `json:"name"`
		} `json:"columns"`
		Warnings []string `json:"warnings"`
		File     string   `json:"file"`
		Error    string   `json:"error"`
	}
	callToolJSON(t, cs, "profile_data", map[string]any{"name": "revenue_by_region"}, &out)
	if out.Error != "" || out.RowCount == 0 || len(out.Columns) == 0 || out.Warnings == nil || out.File == "" {
		t.Fatalf("out = %+v", out)
	}
}

// TestDataToolSchemas checks that a read-only server lists both data tools,
// logs their input schemas as tools/list serves them, and calls each once.
func TestDataToolSchemas(t *testing.T) {
	deps := fullDeps(t)
	deps.ReadOnly = true
	tools, err := ListTools(context.Background(), NewServer(deps))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, tool := range tools {
		if tool.Name != "query_data" && tool.Name != "profile_data" {
			continue
		}
		found++
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s input schema: %s", tool.Name, raw)
	}
	if found != 2 {
		t.Fatalf("found %d data tools", found)
	}

	ct, st := mcpsdk.NewInMemoryTransports()
	ss, err := NewServer(deps).Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close() //nolint:errcheck // in-memory session teardown
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close() //nolint:errcheck // in-memory session teardown
	var q struct {
		Rows  [][]any `json:"rows"`
		Error string  `json:"error"`
	}
	callToolJSON(t, cs, "query_data", map[string]any{"sql": "SELECT count(*) FROM revenue_data"}, &q)
	if q.Error != "" || len(q.Rows) != 1 {
		t.Fatalf("read-only query_data = %+v", q)
	}
	var p struct {
		RowCount int64  `json:"rowCount"`
		Error    string `json:"error"`
	}
	callToolJSON(t, cs, "profile_data", map[string]any{"name": "revenue_data"}, &p)
	if p.Error != "" || p.RowCount == 0 {
		t.Fatalf("read-only profile_data = %+v", p)
	}
}
