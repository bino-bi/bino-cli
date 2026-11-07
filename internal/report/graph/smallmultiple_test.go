package graph

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestBuildSmallMultipleDependsOnDatasetAndBase(t *testing.T) {
	avg := makeDoc("DataSet", "avg", json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"DataSet","metadata":{"name":"avg"},"spec":{"query":"SELECT 1"}}`))
	trend := makeDoc("ChartTime", "trend", json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"ChartTime","metadata":{"name":"trend"},"spec":{"dataset":"avg"}}`))
	page := pageDoc(`[{"kind": "SmallMultiple", "spec": {"dataset": "sales", "scale": "0.5",
		"base": {"kind": "ChartTime", "ref": "trend"},
		"panels": {"by": "rowGroup", "items": [{"label": "Average", "dataset": "avg"}]}}}]`)
	g, err := Build(context.Background(), []config.Document{salesDataSet(), avg, trend, page}, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sm, ok := g.NodeByID(makeNodeID(NodeComponent, "page#0"))
	if !ok || sm.Attributes["componentKind"] != "SmallMultiple" {
		t.Fatalf("small multiple node = %+v", sm)
	}
	for _, want := range []string{makeNodeID(NodeDataSet, "sales"), makeNodeID(NodeDataSet, "avg"), makeNodeID(NodeComponent, "page#0.0")} {
		if !slices.Contains(sm.DependsOn, want) {
			t.Errorf("depends on %v, want %s", sm.DependsOn, want)
		}
	}
	base, _ := g.NodeByID(makeNodeID(NodeComponent, "page#0.0"))
	if base == nil || !slices.Contains(base.DependsOn, makeNodeID(NodeDataSet, "sales")) || slices.Contains(base.DependsOn, makeNodeID(NodeDataSet, "avg")) {
		t.Errorf("the base reads the small multiple's dataset, not its own: %+v", base)
	}
}
