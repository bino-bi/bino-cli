package graph

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

type pluginKinds map[string]config.KindInfo

func (p pluginKinds) PluginKindNames() []string {
	names := make([]string, 0, len(p))
	for n := range p {
		names = append(names, n)
	}
	return names
}

func (p pluginKinds) GetKind(name string) (config.KindInfo, bool) {
	k, ok := p[name]
	return k, ok
}

var gaugeKinds = pluginKinds{
	"Gauge":           {KindName: "Gauge", Category: config.KindCategoryComponent},
	"GaugeDataSource": {KindName: "GaugeDataSource", Category: config.KindCategoryDataSource},
}

func salesDataSet() config.Document {
	return makeDoc("DataSet", "sales", json.RawMessage(`{
		"apiVersion": "bino.bi/v1alpha1",
		"kind": "DataSet",
		"metadata": {"name": "sales"},
		"spec": {"query": "SELECT 1 AS v"}
	}`))
}

func pageDoc(children string) config.Document {
	return makeDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1alpha1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {"children": `+children+`}
	}`))
}

func assertGaugeChild(t *testing.T, g *Graph) {
	t.Helper()
	childID := makeNodeID(NodeComponent, "page#0")
	child, ok := g.NodeByID(childID)
	if !ok {
		t.Fatalf("expected component node %s", childID)
	}
	if got := child.Attributes["componentKind"]; got != "Gauge" {
		t.Errorf("componentKind = %q, want Gauge", got)
	}
	if want := makeNodeID(NodeDataSet, "sales"); !slices.Contains(child.DependsOn, want) {
		t.Errorf("component depends on %v, want %s", child.DependsOn, want)
	}
	page, ok := g.NodeByID(makeNodeID(NodeLayoutPage, "page"))
	if !ok {
		t.Fatal("expected layout page node")
	}
	if !slices.Contains(page.DependsOn, childID) {
		t.Errorf("page depends on %v, want %s", page.DependsOn, childID)
	}
}

func TestBuildPluginLayoutChildInline(t *testing.T) {
	docs := []config.Document{
		salesDataSet(),
		// children is plugin-owned and not a list of layout children.
		pageDoc(`[{"kind": "Gauge", "spec": {"dataset": "sales", "children": "free-form"}}]`),
	}
	g, err := Build(context.Background(), docs, gaugeKinds)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	assertGaugeChild(t, g)
}

func TestBuildPluginLayoutChildRef(t *testing.T) {
	gauge := makeDoc("Gauge", "g1", json.RawMessage(`{
		"apiVersion": "bino.bi/v1alpha1",
		"kind": "Gauge",
		"metadata": {"name": "g1"},
		"spec": {"dataset": "sales", "title": "Revenue"}
	}`))
	docs := []config.Document{salesDataSet(), gauge, pageDoc(`[{"kind": "Gauge", "ref": "g1"}]`)}
	g, err := Build(context.Background(), docs, gaugeKinds)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	assertGaugeChild(t, g)
}

func TestBuildUnknownLayoutChildStillFails(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		kinds config.KindProvider
	}{
		{"no provider", "Gauge", nil},
		{"unknown kind", "Dial", gaugeKinds},
		{"non-component plugin kind", "GaugeDataSource", gaugeKinds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []config.Document{pageDoc(`[{"kind": "` + tt.kind + `", "spec": {}}]`)}
			_, err := Build(context.Background(), docs, tt.kinds)
			if err == nil || !strings.Contains(err.Error(), `unsupported child kind "`+tt.kind+`"`) {
				t.Fatalf("want unsupported child kind error, got %v", err)
			}
		})
	}
}

func TestBuildPluginGridCellInline(t *testing.T) {
	docs := []config.Document{
		salesDataSet(),
		pageDoc(`[{"kind": "Grid", "spec": {"rowHeaders": ["r"], "columnHeaders": ["c"], "children": [
			{"row": "r", "column": "c", "kind": "Gauge", "spec": {"dataset": "sales"}}
		]}}]`),
	}
	g, err := Build(context.Background(), docs, gaugeKinds)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	cellID := makeNodeID(NodeComponent, "page#0.0")
	cell, ok := g.NodeByID(cellID)
	if !ok {
		t.Fatalf("expected component node %s", cellID)
	}
	if got := cell.Attributes["componentKind"]; got != "Gauge" {
		t.Errorf("componentKind = %q, want Gauge", got)
	}
	if want := makeNodeID(NodeDataSet, "sales"); !slices.Contains(cell.DependsOn, want) {
		t.Errorf("grid cell depends on %v, want %s", cell.DependsOn, want)
	}
}

// Tree node refs are not followed by the graph, for built-in kinds too, so
// today neither a Table nor a Gauge node ref adds the dataset edge. A plugin
// node ref must behave like a built-in one.
func TestBuildTreeNodeRefParity(t *testing.T) {
	treeDependsOnSales := func(t *testing.T, kind string) bool {
		t.Helper()
		target := makeDoc(kind, "n1", json.RawMessage(`{
			"apiVersion": "bino.bi/v1alpha1",
			"kind": "`+kind+`",
			"metadata": {"name": "n1"},
			"spec": {"dataset": "sales"}
		}`))
		page := pageDoc(`[{"kind": "Tree", "spec": {"edges": [], "nodes": [{"id": "a", "kind": "` + kind + `", "ref": "n1"}]}}]`)
		g, err := Build(context.Background(), []config.Document{salesDataSet(), target, page}, gaugeKinds)
		if err != nil {
			t.Fatalf("Build returned error: %v", err)
		}
		tree, ok := g.NodeByID(makeNodeID(NodeComponent, "page#0"))
		if !ok {
			t.Fatal("expected tree component node")
		}
		return slices.Contains(tree.DependsOn, makeNodeID(NodeDataSet, "sales"))
	}

	builtin := treeDependsOnSales(t, "Table")
	plugin := treeDependsOnSales(t, "Gauge")
	if builtin != plugin {
		t.Errorf("tree node ref: Table edge %v, Gauge edge %v, want the same", builtin, plugin)
	}
}
