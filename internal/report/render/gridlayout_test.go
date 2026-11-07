package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

const cellLayoutGrid = `{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {
	"rowHeaders": [{"label": "A", "id": "r1"}], "columnHeaders": ["X", "Y"],
	"cellHeight": "equal", "cellWidth": "equal",
	"cellVerticalAlign": "baseline", "cellHorizontalAlign": "baseline",
	"children": [
		{"row": "r1", "column": 0, "kind": "Text", "caption": "North \"1\"", "spec": {"value": "a"}},
		{"row": "r1", "column": 1, "kind": "Text", "spec": {"value": "b"}}
	]
}}]}`

func TestGridCellLayout_WritesAttributes(t *testing.T) {
	html := renderDocsWithArtefactStyle(t, []config.Document{pageDoc(cellLayoutGrid)}, "", "")
	tag := openTag(t, html, "bn-grid")
	for _, want := range []string{
		`cell-height='equal'`,
		`cell-width='equal'`,
		`cell-vertical-align='baseline'`,
		`cell-horizontal-align='baseline'`,
		`cell-captions='{&#34;r1-0&#34;:&#34;North \&#34;1\&#34;&#34;}'`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("missing %s in %s", want, tag)
		}
	}
}

func TestGridCellLayout_DefaultsWriteNothing(t *testing.T) {
	html := renderDocsWithArtefactStyle(t, []config.Document{pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {
		"rowHeaders": ["A"], "columnHeaders": ["X"],
		"children": [{"row": 0, "column": 0, "kind": "Text", "spec": {"value": "a"}}]
	}}]}`)}, "", "")
	if tag := openTag(t, html, "bn-grid"); strings.Contains(tag, "cell-height") || strings.Contains(tag, "cell-width") ||
		strings.Contains(tag, "cell-vertical-align") || strings.Contains(tag, "cell-horizontal-align") || strings.Contains(tag, "cell-captions") {
		t.Fatalf("a grid without the fields must not write them: %s", tag)
	}
}

func TestGridCellLayout_EngineGate(t *testing.T) {
	docs := []config.Document{pageDoc(cellLayoutGrid)}
	_, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.30")
	if err == nil || !strings.Contains(err.Error(), "v1.0.0-next.31") || !strings.Contains(err.Error(), "grid-cell-layout-engine-version") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), docs, "en", "", "", ModeBuild, "v1.0.0-next.31"); err != nil {
		t.Fatalf("new engine: %v", err)
	}
	plain := makeTestDoc("LayoutPage", "page", json.RawMessage(`{"kind":"LayoutPage","metadata":{"name":"page"},"spec":{"children":[]}}`))
	if _, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{plain}, "en", "", "", ModeBuild, "v1.0.0-next.30"); err != nil {
		t.Fatalf("no grid layout fields: %v", err)
	}
}

func TestGridCellLayout_SkippedChildGetsNoCaption(t *testing.T) {
	html := renderDocsWithArtefactStyle(t, []config.Document{pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {
		"rowHeaders": ["A"], "columnHeaders": ["X", "Y"],
		"children": [
			{"row": 0, "column": 0, "kind": "Table", "ref": "missing", "optional": true, "caption": "North"},
			{"row": 0, "column": 1, "kind": "Text", "caption": "South", "spec": {"value": "b"}}
		]
	}}]}`)}, "", "")
	if tag := openTag(t, html, "bn-grid"); !strings.Contains(tag, `cell-captions='{&#34;0-1&#34;:&#34;South&#34;}'`) {
		t.Fatalf("only the rendered cell has a caption: %s", tag)
	}
}
