package render

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

type recordingPluginRenderer struct {
	reqs     []PluginRenderRequest
	warnings []string
	err      error
}

func (r *recordingPluginRenderer) RenderComponent(_ context.Context, req PluginRenderRequest) (string, []string, error) {
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return "", r.warnings, r.err
	}
	return `<bn-gauge></bn-gauge>`, r.warnings, nil
}

// spec decodes the spec of the i-th request.
func (r *recordingPluginRenderer) spec(t *testing.T, i int) map[string]any {
	t.Helper()
	if i >= len(r.reqs) {
		t.Fatalf("want request %d, got %d requests", i, len(r.reqs))
	}
	var got map[string]any
	if err := json.Unmarshal(r.reqs[i].Spec, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func gaugeDoc() config.Document {
	return config.Document{
		Kind: "Gauge",
		Name: "g1",
		Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{"title":"Revenue","dataset":"sales"}}`),
	}
}

func TestRenderLayoutChildPluginRef(t *testing.T) {
	pr := &recordingPluginRenderer{}
	rc := newRenderCtx(context.Background(), []config.Document{gaugeDoc()}, nil, nil, nil, pr, "build")

	child := layoutChild{Kind: "Gauge", Ref: "g1", Spec: json.RawMessage(`{"title":"Override"}`)}
	out, skip, err := renderLayoutChild(child, rc)
	if err != nil {
		t.Fatalf("renderLayoutChild: %v", err)
	}
	if skip {
		t.Fatal("child was skipped")
	}
	if !strings.Contains(out, "<bn-gauge>") {
		t.Errorf("plugin HTML missing from %q", out)
	}
	req := pr.reqs[0]
	if req.Kind != "Gauge" || req.Name != "g1" || req.RenderMode != "build" {
		t.Errorf("plugin got kind %q name %q mode %q", req.Kind, req.Name, req.RenderMode)
	}
	if got := pr.spec(t, 0); got["title"] != "Override" || got["dataset"] != "sales" {
		t.Errorf("plugin got spec %v, want ref spec merged with override", got)
	}
}

func TestCollectReferencedDatasourcesPluginDoc(t *testing.T) {
	gauge := config.Document{
		Kind: "Gauge",
		Name: "g1",
		Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{"dataset":"$sales_src"}}`),
	}
	page := config.Document{
		Kind: "LayoutPage",
		Name: "page",
		Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"LayoutPage","metadata":{"name":"page"},"spec":{"children":[{"kind":"Gauge","ref":"g1"}]}}`),
	}
	refs := collectReferencedDatasources([]config.Document{gauge, page}, nil)
	if !refs["sales_src"] {
		t.Errorf("want sales_src in referenced datasources, got %v", refs)
	}
}

func TestRenderLayoutChildPluginName(t *testing.T) {
	tests := []struct {
		name  string
		child layoutChild
		want  string
	}{
		{"metadata name", layoutChild{Kind: "Gauge", Metadata: layoutChildMeta{Name: "rev"}, Spec: json.RawMessage(`{}`)}, "rev"},
		{"no ref no name", layoutChild{Kind: "Gauge", Spec: json.RawMessage(`{}`)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &recordingPluginRenderer{}
			rc := newRenderCtx(context.Background(), nil, nil, nil, nil, pr, "build")
			if _, _, err := renderLayoutChild(tt.child, rc); err != nil {
				t.Fatalf("renderLayoutChild: %v", err)
			}
			if got := pr.reqs[0].Name; got != tt.want {
				t.Errorf("plugin got name %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderGridPluginCell(t *testing.T) {
	pr := &recordingPluginRenderer{}
	rc := newRenderCtx(context.Background(), []config.Document{gaugeDoc()}, nil, nil, nil, pr, "build")
	rc.inheritedStyle = "corp"

	child := layoutChild{Kind: "Grid", Spec: json.RawMessage(`{
		"selectedStyle": "grid-style",
		"children": [
			{"row": "r1", "column": "c1", "kind": "Gauge", "metadata": {"name": "rev"}, "spec": {"title": "Inline"}},
			{"row": "r1", "column": "c2", "kind": "Gauge", "ref": "g1", "spec": {"title": "Override"}}
		]
	}`)}
	out, _, err := renderLayoutChild(child, rc)
	if err != nil {
		t.Fatalf("renderLayoutChild: %v", err)
	}
	for _, slot := range []string{"r1-c1", "r1-c2"} {
		if !strings.Contains(out, "<div slot='"+slot+"'><bn-gauge></bn-gauge></div>") {
			t.Errorf("plugin HTML missing from slot %s in %q", slot, out)
		}
	}
	if len(pr.reqs) != 2 {
		t.Fatalf("want 2 plugin calls, got %d", len(pr.reqs))
	}
	if r := pr.reqs[0]; r.Kind != "Gauge" || r.Name != "rev" {
		t.Errorf("inline cell: plugin got kind %q name %q", r.Kind, r.Name)
	}
	if r := pr.reqs[1]; r.Kind != "Gauge" || r.Name != "g1" {
		t.Errorf("ref cell: plugin got kind %q name %q", r.Kind, r.Name)
	}
	if got := pr.spec(t, 1); got["title"] != "Override" || got["dataset"] != "sales" {
		t.Errorf("ref cell: plugin got spec %v, want ref spec merged with override", got)
	}
	for i := range pr.reqs {
		if _, ok := pr.spec(t, i)["selectedStyle"]; ok {
			t.Errorf("plugin spec %d got an inherited selectedStyle: %s", i, pr.reqs[i].Spec)
		}
	}
}

func TestRenderTreePluginNode(t *testing.T) {
	pr := &recordingPluginRenderer{}
	rc := newRenderCtx(context.Background(), []config.Document{gaugeDoc()}, nil, nil, nil, pr, "build")

	child := layoutChild{Kind: "Tree", Spec: json.RawMessage(`{
		"selectedStyle": "tree-style",
		"edges": [],
		"nodes": [
			{"id": "n0", "kind": "Label", "spec": {"value": "label"}},
			{"id": "n1", "kind": "Gauge", "spec": {"title": "Inline"}},
			{"id": "n2", "kind": "Gauge", "ref": "g1", "spec": {"title": "Override"}}
		]
	}`)}
	out, _, err := renderLayoutChild(child, rc)
	if err != nil {
		t.Fatalf("renderLayoutChild: %v", err)
	}
	for _, slot := range []string{"n1", "n2"} {
		if !strings.Contains(out, "<div slot='"+slot+"'><bn-gauge></bn-gauge></div>") {
			t.Errorf("plugin HTML missing from slot %s in %q", slot, out)
		}
	}
	if !strings.Contains(openTag(t, out, "bn-text"), `selected-style='tree-style'`) {
		t.Errorf("Label lost the inherited style: %s", out)
	}
	if len(pr.reqs) != 2 {
		t.Fatalf("want 2 plugin calls, got %d", len(pr.reqs))
	}
	// The node id is a slot id, not a name.
	if r := pr.reqs[0]; r.Kind != "Gauge" || r.Name != "" {
		t.Errorf("inline node: plugin got kind %q name %q", r.Kind, r.Name)
	}
	if r := pr.reqs[1]; r.Kind != "Gauge" || r.Name != "g1" {
		t.Errorf("ref node: plugin got kind %q name %q", r.Kind, r.Name)
	}
	if got := pr.spec(t, 1); got["title"] != "Override" || got["dataset"] != "sales" {
		t.Errorf("ref node: plugin got spec %v, want ref spec merged with override", got)
	}
	for i := range pr.reqs {
		if _, ok := pr.spec(t, i)["selectedStyle"]; ok {
			t.Errorf("plugin spec %d got an inherited selectedStyle: %s", i, pr.reqs[i].Spec)
		}
	}
}

func TestRenderPluginWarningsAndErrors(t *testing.T) {
	page := pageDoc(`{"children": [{"kind": "Grid", "spec": {
		"children": [{"row": "r1", "column": "c1", "kind": "Gauge", "spec": {"title": "T"}}]
	}}]}`)
	run := func(pr *recordingPluginRenderer) (Result, error) {
		res, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), []config.Document{page}, nil, "en", "", "", ModeBuild, nil, nil, "v1.0.0", nil,
			&PluginOptions{ComponentRenderer: pr}, "", "", "")
		return res, err
	}

	t.Run("warnings reach the build result", func(t *testing.T) {
		pr := &recordingPluginRenderer{warnings: []string{`plugin gauges: Gauge "": no data`}}
		res, err := run(pr)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if !slices.Contains(res.Warnings, `plugin gauges: Gauge "": no data`) {
			t.Errorf("Result.Warnings = %q, want the plugin warning", res.Warnings)
		}
	})

	t.Run("error fails the render", func(t *testing.T) {
		pr := &recordingPluginRenderer{err: errors.New("boom")}
		_, err := run(pr)
		if err == nil || !strings.Contains(err.Error(), "plugin render Gauge: boom") {
			t.Fatalf("want plugin render error, got %v", err)
		}
	})
}

func TestRenderPluginKindWithoutRenderer(t *testing.T) {
	tests := []struct {
		name  string
		child layoutChild
		want  string
	}{
		{"layout child", layoutChild{Kind: "Gauge", Spec: json.RawMessage(`{}`)}, `unsupported child kind "Gauge"`},
		{"grid child", layoutChild{Kind: "Grid", Spec: json.RawMessage(`{"children": [{"row": "r", "column": "c", "kind": "Gauge", "spec": {}}]}`)}, `unsupported grid child kind "Gauge"`},
		{"tree node", layoutChild{Kind: "Tree", Spec: json.RawMessage(`{"edges": [], "nodes": [{"id": "n", "kind": "Gauge", "spec": {}}]}`)}, `unsupported tree node kind "Gauge"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := newRenderCtx(context.Background(), nil, nil, nil, nil, nil, "build")
			_, _, err := renderLayoutChild(tt.child, rc)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %s, got %v", tt.want, err)
			}
		})
	}
}
