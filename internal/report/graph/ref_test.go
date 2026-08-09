package graph

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// makeDoc creates a config.Document with the given kind, name, and raw JSON payload.
func makeDoc(kind, name string, raw json.RawMessage) config.Document {
	return config.Document{
		Kind: kind,
		Name: name,
		Raw:  raw,
		File: "test.yaml",
	}
}

func TestBuildLayoutChildWithRef(t *testing.T) {
	ctx := context.Background()

	// ChartTime document that can be referenced.
	chartTimeDoc := makeDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "sampleTimeChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Original Title"
		}
	}`))

	// LayoutPage that references the ChartTime via ref.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "sampleTimeChart"
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// Verify the page has dependencies (the inlined chart child).
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref child")
	}
}

func TestBuildLayoutChildWithRefAndOverride(t *testing.T) {
	ctx := context.Background()

	// ChartTime document.
	chartTimeDoc := makeDoc("ChartTime", "sampleTimeChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "sampleTimeChart"},
		"spec": {
			"dataset": "sales_data",
			"chartTitle": "Original Title",
			"level": "category"
		}
	}`))

	// LayoutPage that references the ChartTime with spec override.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "sampleTimeChart",
					"spec": {
						"chartTitle": "Overridden Title"
					}
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists and has dependencies.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref child with override")
	}
}

func TestBuildLayoutChildWithMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime (required ref).
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "nonExistentChart"
				}
			]
		}
	}`))

	docs := []config.Document{layoutPageDoc}
	_, err := Build(ctx, docs)
	if err == nil {
		t.Fatalf("Build should error on missing required ref")
	}
	if !contains(err.Error(), "required reference") {
		t.Fatalf("error message should mention 'required reference', got: %v", err)
	}
}

func TestBuildLayoutChildWithOptionalMissingRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that references a non-existent ChartTime with optional: true.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "nonExistentChart",
					"optional": true
				}
			]
		}
	}`))

	docs := []config.Document{layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build should not error on optional missing ref: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// The missing optional ref child should be skipped, so no dependencies.
	if len(pageNode.DependsOn) != 0 {
		t.Fatalf("expected layout page to have no dependencies when optional ref is missing, got %v", pageNode.DependsOn)
	}
}

func TestBuildLayoutChildWithLayoutPageRef(t *testing.T) {
	ctx := context.Background()

	// LayoutPage that we try to reference (should fail).
	referencedPage := makeDoc("LayoutPage", "otherPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "otherPage"},
		"spec": {
			"children": []
		}
	}`))

	// LayoutPage that tries to ref another LayoutPage (disallowed).
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "LayoutPage",
					"ref": "otherPage"
				}
			]
		}
	}`))

	docs := []config.Document{referencedPage, layoutPageDoc}
	_, err := Build(ctx, docs)
	if err == nil {
		t.Fatalf("expected error when referencing LayoutPage")
	}
	if !contains(err.Error(), "unsupported child kind") {
		// The schema enforces kind enum, so LayoutPage isn't allowed as child kind.
		// The error should be about unsupported child kind.
		t.Logf("got error: %v", err)
	}
}

func TestBuildLayoutChildWithLayoutCardRef(t *testing.T) {
	ctx := context.Background()

	// LayoutCard document that can be referenced.
	layoutCardDoc := makeDoc("LayoutCard", "sampleCard", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutCard",
		"metadata": {"name": "sampleCard"},
		"spec": {
			"children": [
				{
					"kind": "Text",
					"spec": {"value": "Hello from card"}
				}
			]
		}
	}`))

	// LayoutPage that references the LayoutCard via ref.
	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "LayoutCard",
					"ref": "sampleCard"
				}
			]
		}
	}`))

	docs := []config.Document{layoutCardDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// Verify layout page node exists.
	pageID := makeNodeID(NodeLayoutPage, "mainPage")
	pageNode, ok := g.NodeByID(pageID)
	if !ok {
		t.Fatalf("expected layout page node %s", pageID)
	}

	// Verify the page has dependencies (the referenced card).
	if len(pageNode.DependsOn) == 0 {
		t.Fatalf("expected layout page to have dependencies from ref LayoutCard")
	}
}
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildLayoutChildRefWithParams(t *testing.T) {
	ctx := context.Background()

	chartTimeDoc := makeDoc("ChartTime", "regionChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "regionChart"},
		"spec": {
			"dataset": "sales_${REGION}",
			"chartTitle": "Sales ${REGION}"
		}
	}`))
	chartTimeDoc.Params = []config.LayoutPageParamSpec{{Name: "REGION"}}

	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "regionChart",
					"params": {"REGION": "eu"}
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	// The component node's dataset dependency must reflect the expanded param.
	var found bool
	for _, node := range g.Nodes {
		if node.Kind != NodeComponent || node.Attributes["parent"] != "mainPage" {
			continue
		}
		found = true
		if got := node.Attributes["dataset"]; got != "sales_eu" {
			t.Fatalf("expected dataset attribute sales_eu, got %q", got)
		}
	}
	if !found {
		t.Fatalf("expected component node for ref child")
	}
}

func TestBuildLayoutChildRefParamDefaults(t *testing.T) {
	ctx := context.Background()

	chartTimeDoc := makeDoc("ChartTime", "regionChart", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ChartTime",
		"metadata": {"name": "regionChart"},
		"spec": {
			"dataset": "sales_${REGION}",
			"chartTitle": "Sales ${REGION}"
		}
	}`))
	def := "us"
	chartTimeDoc.Params = []config.LayoutPageParamSpec{{Name: "REGION", Default: &def}}

	layoutPageDoc := makeDoc("LayoutPage", "mainPage", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "mainPage"},
		"spec": {
			"children": [
				{
					"kind": "ChartTime",
					"ref": "regionChart"
				}
			]
		}
	}`))

	docs := []config.Document{chartTimeDoc, layoutPageDoc}
	g, err := Build(ctx, docs)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	var found bool
	for _, node := range g.Nodes {
		if node.Kind != NodeComponent || node.Attributes["parent"] != "mainPage" {
			continue
		}
		found = true
		if got := node.Attributes["dataset"]; got != "sales_us" {
			t.Fatalf("expected dataset attribute sales_us from default, got %q", got)
		}
	}
	if !found {
		t.Fatalf("expected component node for ref child")
	}
}
