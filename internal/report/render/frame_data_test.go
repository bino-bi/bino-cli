package render

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
)

// fakePlugin renders every plugin kind as an empty element.
type fakePlugin struct{}

func (fakePlugin) RenderComponent(context.Context, string, string, []byte, string) (string, error) {
	return "<bn-fake></bn-fake>", nil
}

func specDoc(kind, name, spec string) config.Document {
	return makeTestDoc(kind, name, json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "`+kind+`",
		"metadata": {"name": "`+name+`"},
		"spec": `+spec+`
	}`))
}

func datasetResults(names ...string) []dataset.Result {
	results := make([]dataset.Result, 0, len(names))
	for _, n := range names {
		results = append(results, dataset.Result{Name: n, Data: json.RawMessage(`[{"v":1}]`)})
	}
	return results
}

func frame(t *testing.T, docs []config.Document, results []dataset.Result, format string, mode Mode, dataMode string) FrameResult {
	t.Helper()
	res, _, err := GenerateFrameAndContext(context.Background(), docs, results, "en", format, mode, nil, nil, "v1.0.0", nil,
		&PluginOptions{DataMode: dataMode, ComponentRenderer: fakePlugin{}}, "", "")
	if err != nil {
		t.Fatalf("GenerateFrameAndContext failed: %v", err)
	}
	return res
}

// emitted lists EmittedData as "kind/name".
func emitted(res FrameResult) []string {
	out := make([]string, 0, len(res.EmittedData))
	for _, e := range res.EmittedData {
		out = append(out, e.Kind+"/"+e.Name)
	}
	return out
}

func TestGenerateFrameAndContext_ServeEmitsOnlyBoundDatasets(t *testing.T) {
	docs := []config.Document{pageDoc(`{"children": [{"kind": "Table", "spec": {"dataset": "A"}}]}`)}
	results := datasetResults("A", "B")

	for _, dataMode := range []string{DataModeInline, DataModeURL} {
		t.Run(dataMode, func(t *testing.T) {
			res := frame(t, docs, results, "", ModeServe, dataMode)
			ctxHTML := string(res.ContextHTML)
			if !strings.Contains(ctxHTML, "<bn-dataset name='A'") {
				t.Fatalf("expected bound dataset A in context, got:\n%s", ctxHTML)
			}
			if strings.Contains(ctxHTML, "<bn-dataset name='B'") {
				t.Fatalf("unbound dataset B must not be in context, got:\n%s", ctxHTML)
			}
			var want []string
			if dataMode == DataModeURL {
				want = []string{EmittedKindDataset + "/A"}
				if strings.Contains(ctxHTML, "/__bino/data/dataset/B") {
					t.Fatalf("unbound dataset B must have no URL, got:\n%s", ctxHTML)
				}
			}
			if got := emitted(res); !slices.Equal(got, want) {
				t.Fatalf("EmittedData = %v, want %v", got, want)
			}

			preview := string(frame(t, docs, results, "", ModePreview, dataMode).ContextHTML)
			if !strings.Contains(preview, "<bn-dataset name='B'") {
				t.Fatalf("preview should still carry dataset B, got:\n%s", preview)
			}
		})
	}
}

func TestGenerateFrameAndContext_ServeEmitsOnlyBoundDatasources(t *testing.T) {
	docs := []config.Document{
		specDoc("DataSource", "used_src", `{"type": "inline", "content": [{"v": 1}]}`),
		specDoc("DataSource", "orphan_src", `{"type": "inline", "content": [{"v": 2}]}`),
		specDoc("Table", "used_table", `{"dataset": "$used_src"}`),
		specDoc("Table", "orphan_table", `{"dataset": "$orphan_src"}`),
		// A plain name binds a DataSet, never the DataSource of that name.
		pageDoc(`{"children": [
			{"kind": "Table", "ref": "used_table"},
			{"kind": "Table", "spec": {"dataset": "orphan_src"}}
		]}`),
	}

	for _, dataMode := range []string{DataModeInline, DataModeURL} {
		t.Run(dataMode, func(t *testing.T) {
			res := frame(t, docs, nil, "", ModeServe, dataMode)
			ctxHTML := string(res.ContextHTML)
			if !strings.Contains(ctxHTML, "<bn-datasource name='used_src'") {
				t.Fatalf("expected bound datasource used_src in context, got:\n%s", ctxHTML)
			}
			if strings.Contains(ctxHTML, "<bn-datasource name='orphan_src'") || strings.Contains(ctxHTML, "/__bino/data/datasource/orphan_src") {
				t.Fatalf("unbound datasource orphan_src must not be in context, got:\n%s", ctxHTML)
			}
			var want []string
			if dataMode == DataModeURL {
				want = []string{EmittedKindDatasource + "/used_src"}
			}
			if got := emitted(res); !slices.Equal(got, want) {
				t.Fatalf("EmittedData = %v, want %v", got, want)
			}

			preview := string(frame(t, docs, nil, "", ModePreview, dataMode).ContextHTML)
			if !strings.Contains(preview, "<bn-datasource name='orphan_src'") {
				t.Fatalf("preview should still carry datasource orphan_src, got:\n%s", preview)
			}
		})
	}
}

func TestGenerateFrameAndContext_ServeFollowsRenderedComponents(t *testing.T) {
	paramTable := specDoc("Table", "param_table", `{"dataset": "${DS}"}`)
	paramTable.Params = []config.LayoutPageParamSpec{{Name: "DS"}}
	docs := []config.Document{
		specDoc("Table", "base_table", `{"dataset": "base_ds"}`),
		paramTable,
		specDoc("Table", "unused_table", `{"dataset": "unused_ds"}`),
		// The page style makes every child render with a copied render context.
		pageDoc(`{"selectedStyle": "page_style", "children": [
			{"kind": "LayoutCard", "spec": {"children": [{"kind": "Table", "spec": {"dataset": "card_ds"}}]}},
			{"kind": "Grid", "spec": {"rowHeaders": ["r1"], "columnHeaders": ["c1"], "children": [
				{"row": 0, "column": 0, "kind": "ChartTime", "spec": {"dataset": "grid_ds"}}
			]}},
			{"kind": "Tree", "spec": {"edges": [], "nodes": [
				{"id": "label", "kind": "Label", "spec": {"value": "L", "dataset": "label_ds"}},
				{"id": "param", "kind": "Table", "ref": "param_table", "params": {"DS": "param_ds"}}
			]}},
			{"kind": "Table", "ref": "base_table", "spec": {"dataset": "override_ds"}},
			{"kind": "Table", "spec": {"dataset": "$dollar_ds"}},
			{"kind": "FakeChart", "spec": {"dataset": "plugin_ds"}}
		]}`),
		// xga is the render format, so this page is skipped.
		specDoc("LayoutPage", "skipped", `{"pageFormat": "a4", "children": [{"kind": "Table", "spec": {"dataset": "skipped_ds"}}]}`),
	}
	present := []string{"card_ds", "grid_ds", "label_ds", "param_ds", "override_ds", "dollar_ds", "plugin_ds"}
	absent := []string{"base_ds", "skipped_ds", "unused_ds"}

	ctxHTML := string(frame(t, docs, datasetResults(slices.Concat(present, absent)...), "xga", ModeServe, DataModeInline).ContextHTML)
	for _, name := range present {
		if !strings.Contains(ctxHTML, "<bn-dataset name='"+name+"'") {
			t.Errorf("expected bound dataset %s in context", name)
		}
	}
	for _, name := range absent {
		if strings.Contains(ctxHTML, "<bn-dataset name='"+name+"'") {
			t.Errorf("unbound dataset %s must not be in context", name)
		}
	}
}

func TestGenerateFrameAndContext_ServePagelessContextStaysBlank(t *testing.T) {
	docs := []config.Document{
		specDoc("LayoutPage", "skipped", `{"pageFormat": "a4", "children": [{"kind": "Table", "spec": {"dataset": "A"}}]}`),
	}

	ctxHTML := string(frame(t, docs, datasetResults("A"), "xga", ModeServe, DataModeInline).ContextHTML)
	if strings.Contains(ctxHTML, "empty-state") || strings.Contains(ctxHTML, "<bn-dataset") {
		t.Fatalf("expected a blank context, got:\n%s", ctxHTML)
	}
}
