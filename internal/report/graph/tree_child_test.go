package graph

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func treeDoc() config.Document {
	return makeDoc("Tree", "org_tree", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Tree",
		"metadata": {"name": "org_tree"},
		"spec": {"children": []}
	}`))
}

// Regression: the builder's doc index omitted Tree while the renderer accepts
// it, so a layout child {kind: Tree, ref: ...} hard-errored in graph.Build and
// bino build failed on a bundle that renders fine.
func TestBuildLayoutPageWithTreeChild(t *testing.T) {
	ctx := context.Background()

	layoutPageDoc := makeDoc("LayoutPage", "main_page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "main_page"},
		"spec": {
			"children": [
				{"kind": "Tree", "ref": "org_tree"}
			]
		}
	}`))

	g, err := Build(ctx, []config.Document{treeDoc(), layoutPageDoc})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if _, ok := g.NodeByID(makeNodeID(NodeLayoutPage, "main_page")); !ok {
		t.Fatalf("expected layout page node main_page")
	}
}

// Standalone Tree documents must become graph nodes like every other
// component kind; without one, Tree is invisible to bino graph and to
// preview's affected-node invalidation.
func TestBuildStandaloneTreeComponent(t *testing.T) {
	ctx := context.Background()

	g, err := Build(ctx, []config.Document{treeDoc()})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if _, ok := g.NodeByID(makeNodeID(NodeComponent, "org_tree")); !ok {
		t.Fatalf("expected Tree component node org_tree in graph")
	}
}
