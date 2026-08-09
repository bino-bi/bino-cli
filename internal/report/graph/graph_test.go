package graph

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// End-to-end graph construction over an inline bundle: artefact → page →
// component → dataset → datasource. This replaces a permanently skipped test
// that depended on an examples/ directory absent from the repo.
func TestBuildGraphFromBundle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	docs := []config.Document{
		makeDoc("DataSource", "ppl", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "DataSource",
			"metadata": {"name": "ppl"},
			"spec": {"type": "inline", "content": [{"region": "EMEA", "amount": 12}]}
		}`)),
		makeDoc("DataSet", "ppl_ds", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "DataSet",
			"metadata": {"name": "ppl_ds"},
			"spec": {"query": "SELECT region, amount FROM ppl", "dependencies": ["ppl"]}
		}`)),
		makeDoc("Table", "ppl_table", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "Table",
			"metadata": {"name": "ppl_table"},
			"spec": {"dataset": "ppl_ds"}
		}`)),
		makeDoc("LayoutPage", "main_page", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "LayoutPage",
			"metadata": {"name": "main_page"},
			"spec": {"children": [{"kind": "Table", "ref": "ppl_table"}]}
		}`)),
		makeDoc("ReportArtefact", "minimal_report", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "ReportArtefact",
			"metadata": {"name": "minimal_report"},
			"spec": {"filename": "out.pdf", "title": "Minimal", "layoutPages": ["main_page"]}
		}`)),
	}

	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if _, ok := g.ReportArtefactByName("minimal_report"); !ok {
		t.Fatalf("expected artefact minimal_report to exist")
	}

	// Dataset depends on the datasource its query references.
	datasetNode, ok := g.NodeByID(makeNodeID(NodeDataSet, "ppl_ds"))
	if !ok {
		t.Fatalf("dataset node not found")
	}
	dsID := makeNodeID(NodeDataSource, "ppl")
	foundDep := false
	for _, dep := range datasetNode.DependsOn {
		if dep == dsID {
			foundDep = true
			break
		}
	}
	if !foundDep {
		t.Fatalf("dataset should depend on datasource %s, got %v", dsID, datasetNode.DependsOn)
	}

	// The datasource node carries its spec type as an attribute.
	dsNode, ok := g.NodeByID(dsID)
	if !ok {
		t.Fatalf("datasource %s not found", dsID)
	}
	if dsNode.Attributes["type"] != "inline" {
		t.Fatalf("expected datasource type inline, got %q", dsNode.Attributes["type"])
	}

	// At least one component references a dataset.
	foundComponent := false
	for _, node := range g.Nodes {
		if node.Kind == NodeComponent && node.Attributes["dataset"] != "" {
			foundComponent = true
			break
		}
	}
	if !foundComponent {
		t.Fatalf("expected at least one component with a dataset attribute")
	}
}
