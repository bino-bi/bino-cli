package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

type recordingPluginRenderer struct {
	kind, name string
	spec       []byte
}

func (r *recordingPluginRenderer) RenderComponent(_ context.Context, kind, name string, spec []byte, _ string) (string, error) {
	r.kind, r.name, r.spec = kind, name, spec
	return `<bn-gauge></bn-gauge>`, nil
}

func TestRenderLayoutChildPluginRef(t *testing.T) {
	gauge := config.Document{
		Kind: "Gauge",
		Name: "g1",
		Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{"title":"Revenue","dataset":"sales"}}`),
	}
	pr := &recordingPluginRenderer{}
	rc := newRenderCtx(context.Background(), []config.Document{gauge}, nil, nil, nil, pr, "build")

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
	if pr.kind != "Gauge" || pr.name != "g1" {
		t.Errorf("plugin got kind %q name %q", pr.kind, pr.name)
	}
	var got map[string]string
	if err := json.Unmarshal(pr.spec, &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "Override" || got["dataset"] != "sales" {
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
