package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// renderDocsWithArtefactStyle renders documents with an artefact-level
// selectedStyle and returns the generated HTML.
func renderDocsWithArtefactStyle(t *testing.T, docs []config.Document, artefactStyle, rootComponent string) string {
	t.Helper()

	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, nil, "en", "", "", ModePreview, nil, nil, "v1.0.0", nil, nil, rootComponent, artefactStyle, "")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocumentsWithDatasets failed: %v", err)
	}
	return string(result.HTML)
}

// openTag returns the first opening tag for the given element name, e.g.
// openTag(html, "bn-table") -> "<bn-table dataset='test' ...".
func openTag(t *testing.T, html, tag string) string {
	t.Helper()
	idx := strings.Index(html, "<"+tag)
	if idx < 0 {
		t.Fatalf("expected <%s> element in HTML, got:\n%s", tag, html)
	}
	rest := html[idx:]
	end := strings.Index(rest, ">")
	if end < 0 {
		t.Fatalf("unterminated <%s> tag in HTML", tag)
	}
	return rest[:end]
}

func pageDoc(spec string) config.Document {
	return makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": `+spec+`
	}`))
}

func TestSelectedStyleInherit_ArtefactToPage(t *testing.T) {
	html := renderDocsWithArtefactStyle(t, []config.Document{pageDoc(`{"children": []}`)}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `selected-style='corp'`) {
		t.Fatalf("expected artefact style on <bn-layout-page>, got:\n%s", html)
	}
}

func TestSelectedStyleInherit_ArtefactToChildren(t *testing.T) {
	tests := []struct {
		kind string
		spec string
		tag  string
	}{
		{"Table", `{"dataset": "test"}`, "bn-table"},
		{"Text", `{"value": "hello"}`, "bn-text"},
		{"ChartStructure", `{"dataset": "test"}`, "bn-chart-structure"},
		{"ChartTime", `{"dataset": "test"}`, "bn-chart-time"},
		{"ChartScatter", `{"dataset": "test"}`, "bn-chart-scatter"},
		{"ChartBubble", `{"dataset": "test"}`, "bn-chart-bubble"},
		{"ChartBullet", `{"dataset": "test"}`, "bn-chart-bullet"},
		{"Tree", `{"edges": [], "nodes": []}`, "bn-tree"},
		{"Grid", `{"children": []}`, "bn-grid"},
		{"Image", `{"source": "logo.png"}`, "bn-image"},
		{"LayoutCard", `{"children": []}`, "bn-layout-card"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			doc := pageDoc(`{"children": [{"kind": "` + tt.kind + `", "metadata": {"name": "child"}, "spec": ` + tt.spec + `}]}`)
			html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")
			if !strings.Contains(openTag(t, html, tt.tag), `selected-style='corp'`) {
				t.Fatalf("expected inherited selected-style='corp' on <%s>, got:\n%s", tt.tag, html)
			}
		})
	}
}

func TestSelectedStyleInherit_PageOverridesArtefact(t *testing.T) {
	doc := pageDoc(`{
		"selectedStyle": "page-style",
		"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]
	}`)
	html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `selected-style='page-style'`) {
		t.Fatalf("expected page's own style on <bn-layout-page>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='page-style'`) {
		t.Fatalf("expected page style inherited by <bn-table>, got:\n%s", html)
	}
}

func TestSelectedStyleInherit_CardOverrideAndSiblingIsolation(t *testing.T) {
	doc := pageDoc(`{
		"selectedStyle": "page-style",
		"children": [
			{"kind": "LayoutCard", "metadata": {"name": "card"}, "spec": {
				"selectedStyle": "card-style",
				"children": [{"kind": "Table", "metadata": {"name": "inner"}, "spec": {"dataset": "test"}}]
			}},
			{"kind": "Text", "metadata": {"name": "sibling"}, "spec": {"value": "hi"}}
		]
	}`)
	html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-card"), `selected-style='card-style'`) {
		t.Fatalf("expected card's own style on <bn-layout-card>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='card-style'`) {
		t.Fatalf("expected card style inherited by <bn-table> inside the card, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-text"), `selected-style='page-style'`) {
		t.Fatalf("expected page style on the sibling <bn-text> outside the card, got:\n%s", html)
	}
}

func TestSelectedStyleInherit_ChildOwnStyleWins(t *testing.T) {
	t.Run("inline", func(t *testing.T) {
		doc := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test", "selectedStyle": "own-style"}}]}`)
		html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='own-style'`) {
			t.Fatalf("expected child's own style to win, got:\n%s", html)
		}
	})

	t.Run("referenced doc", func(t *testing.T) {
		tableDoc := makeTestDoc("Table", "styled", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "Table",
			"metadata": {"name": "styled"},
			"spec": {"dataset": "test", "selectedStyle": "own-style"}
		}`))
		doc := pageDoc(`{"children": [{"kind": "Table", "ref": "styled"}]}`)
		html := renderDocsWithArtefactStyle(t, []config.Document{doc, tableDoc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='own-style'`) {
			t.Fatalf("expected referenced doc's style to win, got:\n%s", html)
		}
	})

	t.Run("ref with spec override", func(t *testing.T) {
		tableDoc := makeTestDoc("Table", "plain", json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "Table",
			"metadata": {"name": "plain"},
			"spec": {"dataset": "test"}
		}`))
		doc := pageDoc(`{"children": [{"kind": "Table", "ref": "plain", "spec": {"selectedStyle": "override-style"}}]}`)
		html := renderDocsWithArtefactStyle(t, []config.Document{doc, tableDoc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='override-style'`) {
			t.Fatalf("expected child spec override style to win, got:\n%s", html)
		}
	})
}

func TestSelectedStyleInherit_GridAndTreeChildren(t *testing.T) {
	t.Run("grid cells", func(t *testing.T) {
		doc := pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {
			"selectedStyle": "grid-style",
			"children": [{"row": "r1", "column": "c1", "kind": "Text", "spec": {"value": "cell"}}]
		}}]}`)
		html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-grid"), `selected-style='grid-style'`) {
			t.Fatalf("expected grid's own style on <bn-grid>, got:\n%s", html)
		}
		if !strings.Contains(openTag(t, html, "bn-text"), `selected-style='grid-style'`) {
			t.Fatalf("expected grid style inherited by the cell <bn-text>, got:\n%s", html)
		}
	})

	t.Run("tree nodes and labels", func(t *testing.T) {
		doc := pageDoc(`{"children": [{"kind": "Tree", "metadata": {"name": "tree"}, "spec": {
			"edges": [],
			"nodes": [
				{"id": "n1", "kind": "Label", "spec": {"value": "label"}},
				{"id": "n2", "kind": "Table", "spec": {"dataset": "test"}}
			]
		}}]}`)
		html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-tree"), `selected-style='corp'`) {
			t.Fatalf("expected inherited style on <bn-tree>, got:\n%s", html)
		}
		if !strings.Contains(openTag(t, html, "bn-text"), `selected-style='corp'`) {
			t.Fatalf("expected inherited style on the Label <bn-text>, got:\n%s", html)
		}
		if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='corp'`) {
			t.Fatalf("expected inherited style on the node <bn-table>, got:\n%s", html)
		}
	})
}

func TestSelectedStyleInherit_EmptyStringOptsOut(t *testing.T) {
	doc := pageDoc(`{"children": [
		{"kind": "Table", "metadata": {"name": "optout"}, "spec": {"dataset": "test", "selectedStyle": ""}},
		{"kind": "Text", "metadata": {"name": "inherits"}, "spec": {"value": "hi"}}
	]}`)
	html := renderDocsWithArtefactStyle(t, []config.Document{doc}, "corp", "")

	if strings.Contains(openTag(t, html, "bn-table"), "selected-style=") {
		t.Fatalf("expected no selected-style on the opted-out <bn-table>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-text"), `selected-style='corp'`) {
		t.Fatalf("expected sibling <bn-text> to still inherit, got:\n%s", html)
	}
}

func TestSelectedStyleInherit_StandaloneRootComponent(t *testing.T) {
	textDoc := makeTestDoc("Text", "standalone", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Text",
		"metadata": {"name": "standalone"},
		"spec": {"value": "hello"}
	}`))
	html := renderDocsWithArtefactStyle(t, []config.Document{textDoc}, "embed-style", "standalone")

	if !strings.Contains(openTag(t, html, "bn-text"), `selected-style='embed-style'`) {
		t.Fatalf("expected artefact style on standalone <bn-text>, got:\n%s", html)
	}
}

func TestSelectedStyleInherit_Presentation(t *testing.T) {
	doc := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]}`)
	artifact := config.Artifact{
		Document: makeTestDoc("ReportArtefact", "deck", json.RawMessage(`{"kind": "ReportArtefact", "metadata": {"name": "deck"}, "spec": {}}`)),
		Spec:     config.ReportArtefactSpec{SelectedStyle: "corp"},
	}

	result, _, err := GeneratePresentationHTML(context.Background(), []config.Document{doc}, nil, artifact, config.PresentationConfig{}, nil, nil, "v1.0.0", nil, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePresentationHTML failed: %v", err)
	}
	html := string(result.HTML)

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `selected-style='corp'`) {
		t.Fatalf("expected artefact style on the slide <bn-layout-page>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `selected-style='corp'`) {
		t.Fatalf("expected artefact style inherited by the slide's <bn-table>, got:\n%s", html)
	}
}
