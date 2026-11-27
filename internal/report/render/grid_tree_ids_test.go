package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

func renderGridWithConstraints(t *testing.T, gridSpec string, cc *spec.ConstraintContext) (string, []string) {
	t.Helper()
	rc := newRenderCtx(context.Background(), nil, cc, nil, nil, nil, string(cc.Mode))
	var warnings []string
	rc.warnings = &warnings
	out, _, err := renderLayoutChild(layoutChild{Kind: "Grid", Spec: json.RawMessage(gridSpec)}, rc)
	if err != nil {
		t.Fatal(err)
	}
	return out, warnings
}

// A Grid child whose own constraints do not match is not rendered.
func TestGridChild_LabelConstraintFiltersChild(t *testing.T) {
	out, _ := renderGridWithConstraints(t, `{
		"rowHeaders": ["A"], "columnHeaders": ["X", "Y"],
		"children": [
			{"row": 0, "column": 0, "kind": "Text", "metadata": {"name": "detailed", "constraints": ["labels.detail == high"]}, "spec": {"value": "high-detail"}},
			{"row": 0, "column": 1, "kind": "Text", "metadata": {"name": "always"}, "spec": {"value": "always-shown"}}
		]
	}`, &spec.ConstraintContext{Labels: map[string]string{"detail": "low"}, Mode: spec.ModeBuild})
	if strings.Contains(out, "high-detail") {
		t.Errorf("constrained child must not render:\n%s", out)
	}
	if !strings.Contains(out, "always-shown") {
		t.Errorf("unconstrained child must render:\n%s", out)
	}
}

func TestGridChild_ModeConstraint(t *testing.T) {
	grid := `{
		"rowHeaders": ["A"], "columnHeaders": ["X"],
		"children": [
			{"row": 0, "column": 0, "kind": "Text", "metadata": {"name": "built", "constraints": ["mode == build"]}, "spec": {"value": "build-only"}},
			{"row": 0, "column": 0, "kind": "Text", "metadata": {"name": "previewed", "constraints": ["mode == preview"]}, "spec": {"value": "preview-only"}}
		]
	}`
	build, _ := renderGridWithConstraints(t, grid, &spec.ConstraintContext{Mode: spec.ModeBuild})
	if strings.Contains(build, "preview-only") || !strings.Contains(build, "build-only") {
		t.Errorf("build mode shows only the build child:\n%s", build)
	}
	preview, _ := renderGridWithConstraints(t, grid, &spec.ConstraintContext{Mode: spec.ModePreview})
	if strings.Contains(preview, "build-only") || !strings.Contains(preview, "preview-only") {
		t.Errorf("preview mode shows only the preview child:\n%s", preview)
	}
}

// A left-out child takes no position, so it does not cause a span warning.
func TestGridChild_FilteredChildNoSpanWarning(t *testing.T) {
	out, warnings := renderGridWithConstraints(t, `{
		"rowHeaders": ["A"], "columnHeaders": ["X", "Y"],
		"children": [
			{"row": 0, "column": 0, "columnSpan": 2, "kind": "Text", "metadata": {"name": "wide", "constraints": ["mode == preview"]}, "spec": {"value": "wide"}},
			{"row": 0, "column": 1, "kind": "Text", "metadata": {"name": "narrow"}, "spec": {"value": "narrow"}}
		]
	}`, &spec.ConstraintContext{Mode: spec.ModeBuild})
	if len(warnings) != 0 {
		t.Errorf("no span warning expected, got %v", warnings)
	}
	if !strings.Contains(out, "narrow") {
		t.Errorf("the other child must render:\n%s", out)
	}
}

// A Tree ref node carries the id a screenshot ref finds it by; inline nodes get none.
func TestTreeNode_WritesComponentID(t *testing.T) {
	out, _, err := renderLayoutChild(layoutChild{Kind: "Tree", Spec: json.RawMessage(`{
		"nodes": [
			{"id": "root", "kind": "ChartStructure", "ref": "x"},
			{"id": "leaf", "kind": "Label", "spec": {"value": "inline"}}
		],
		"edges": [{"from": "root", "to": "leaf"}]
	}`)}, newRenderCtx(context.Background(), []config.Document{
		makeTestDoc("ChartStructure", "x", json.RawMessage(`{"kind":"ChartStructure","metadata":{"name":"x"},"spec":{"dataset":"test"}}`)),
	}, nil, nil, nil, nil, "build"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<div slot='root' id='bino-chartstructure-x'`) {
		t.Errorf("ref node slot must carry the id:\n%s", out)
	}
	if strings.Count(out, "id='bino-") != 1 {
		t.Errorf("inline node must have no id:\n%s", out)
	}
}
