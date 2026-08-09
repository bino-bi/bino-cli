package graph

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// NodeByManifest replaces the two byte-identical findGraphNode copies in
// internal/cli and internal/daemon, deriving the component-kind set from the
// builder's own list instead of restating it (the copies had drifted: both
// omitted Tree and Grid).
func TestNodeByManifest(t *testing.T) {
	ctx := context.Background()

	docs := []config.Document{
		makeDoc("Grid", "kpi_grid", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "Grid",
			"metadata": {"name": "kpi_grid"},
			"spec": {"children": []}
		}`)),
		treeDoc(),
		makeDoc("LayoutPage", "main_page", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "LayoutPage",
			"metadata": {"name": "main_page"},
			"spec": {"children": []}
		}`)),
	}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	tests := []struct {
		kind, name string
		want       bool
	}{
		{"Grid", "kpi_grid", true},
		{"Tree", "org_tree", true},
		{"LayoutPage", "main_page", true},
		{"Grid", "missing", false},
		{"Table", "kpi_grid", false},
	}
	for _, tc := range tests {
		got := g.NodeByManifest(tc.kind, tc.name)
		if (got != nil) != tc.want {
			t.Errorf("NodeByManifest(%q, %q) = %v, want found=%v", tc.kind, tc.name, got, tc.want)
		}
	}
}
