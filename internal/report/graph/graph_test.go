package graph

import (
	"context"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestBuildGraphFromExampleBundle(t *testing.T) {
	// TODO: re-enable this test when example manifests are stable
	t.Skip("Skipping graph build test; re-enable when example manifests are stable")
	t.Parallel()

	ctx := context.Background()
	workdir, err := filepath.Abs(filepath.Join("..", "..", "..", "examples", "minimal"))
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	docs, err := config.LoadDir(ctx, workdir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Check that the minimalReport artifact exists
	if _, ok := g.ReportArtefactByName("minimalReport"); !ok {
		t.Fatalf("expected artifact minimalReport to exist")
	}

	// Check dataset ppl_ds with dependency on ppl datasource
	datasetID := makeNodeID(NodeDataSet, "ppl_ds")
	datasetNode, ok := g.NodeByID(datasetID)
	if !ok {
		t.Fatalf("dataset node %s not found", datasetID)
	}
	if deps := datasetNode.Attributes["dependencies"]; deps != "ppl" {
		t.Fatalf("dataset dependencies not preserved: got %q, want %q", deps, "ppl")
	}

	// ppl datasource exists, so dataset should depend on it
	dsID := makeNodeID(NodeDataSource, "ppl")
	if len(datasetNode.DependsOn) == 0 {
		t.Fatalf("dataset with existing dependencies should have DependsOn")
	}
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

	// Check that ppl datasource exists and has csv type
	dsNode, ok := g.NodeByID(dsID)
	if !ok {
		t.Fatalf("datasource %s not found", dsID)
	}
	if dsNode.Attributes["type"] != "csv" {
		t.Fatalf("expected datasource type csv, got %q", dsNode.Attributes["type"])
	}

	// Check that components exist and reference datasets
	var foundComponent bool
	for _, node := range g.Nodes {
		if node.Kind != NodeComponent {
			continue
		}
		if dataset := node.Attributes["dataset"]; dataset != "" {
			foundComponent = true
			break
		}
	}
	if !foundComponent {
		t.Fatalf("expected at least one component with dataset attribute")
	}
}
