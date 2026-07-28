package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// renderDocsWithArtefactNamespace renders documents with an artefact-level
// i18nNamespace and returns the generated HTML.
func renderDocsWithArtefactNamespace(t *testing.T, docs []config.Document, artefactNamespace, rootComponent string) string {
	t.Helper()

	result, _, err := GenerateHTMLFromDocumentsWithDatasets(context.Background(), docs, nil, "en", "", "", ModePreview, nil, nil, "v1.0.0", nil, nil, rootComponent, "", artefactNamespace)
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocumentsWithDatasets failed: %v", err)
	}
	return string(result.HTML)
}

func TestI18nNamespaceInherit_ArtefactToPageTitle(t *testing.T) {
	html := renderDocsWithArtefactNamespace(t, []config.Document{pageDoc(`{"children": []}`)}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `title-namespace='corp'`) {
		t.Fatalf("expected artefact namespace as title-namespace on <bn-layout-page>, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_ArtefactToChildren(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			doc := pageDoc(`{"children": [{"kind": "` + tt.kind + `", "metadata": {"name": "child"}, "spec": ` + tt.spec + `}]}`)
			html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")
			if !strings.Contains(openTag(t, html, tt.tag), `namespace='corp'`) {
				t.Fatalf("expected inherited namespace='corp' on <%s>, got:\n%s", tt.tag, html)
			}
		})
	}
}

func TestI18nNamespaceInherit_PageOverridesArtefact(t *testing.T) {
	doc := pageDoc(`{
		"i18nNamespace": "page-ns",
		"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]
	}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `title-namespace='page-ns'`) {
		t.Fatalf("expected page's own namespace as title-namespace, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `namespace='page-ns'`) {
		t.Fatalf("expected page namespace inherited by <bn-table>, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_TitleNamespaceStaysTitleOnly(t *testing.T) {
	doc := pageDoc(`{
		"titleNamespace": "legacy",
		"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]
	}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `title-namespace='legacy'`) {
		t.Fatalf("expected deprecated titleNamespace on title-namespace, got:\n%s", html)
	}
	if strings.Contains(openTag(t, html, "bn-table"), "namespace=") {
		t.Fatalf("expected titleNamespace NOT to inherit to children, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_I18nNamespaceWinsOverTitleNamespace(t *testing.T) {
	doc := pageDoc(`{"titleNamespace": "legacy", "i18nNamespace": "modern", "children": []}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `title-namespace='modern'`) {
		t.Fatalf("expected i18nNamespace to win over titleNamespace, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_ChildOwnNamespaceWins(t *testing.T) {
	doc := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test", "i18nNamespace": "own-ns"}}]}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-table"), `namespace='own-ns'`) {
		t.Fatalf("expected child's own namespace to win, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_CardAndSiblingIsolation(t *testing.T) {
	doc := pageDoc(`{
		"i18nNamespace": "page-ns",
		"children": [
			{"kind": "LayoutCard", "metadata": {"name": "card"}, "spec": {
				"i18nNamespace": "card-ns",
				"children": [{"kind": "Table", "metadata": {"name": "inner"}, "spec": {"dataset": "test"}}]
			}},
			{"kind": "Text", "metadata": {"name": "sibling"}, "spec": {"value": "hi"}}
		]
	}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-card"), `title-namespace='card-ns'`) {
		t.Fatalf("expected card's own namespace as its title-namespace, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `namespace='card-ns'`) {
		t.Fatalf("expected card namespace inherited by <bn-table> inside the card, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-text"), `namespace='page-ns'`) {
		t.Fatalf("expected page namespace on the sibling <bn-text> outside the card, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_GridAndTreeChildren(t *testing.T) {
	t.Run("grid cells", func(t *testing.T) {
		doc := pageDoc(`{"children": [{"kind": "Grid", "metadata": {"name": "grid"}, "spec": {
			"i18nNamespace": "grid-ns",
			"children": [{"row": "r1", "column": "c1", "kind": "Text", "spec": {"value": "cell"}}]
		}}]}`)
		html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-text"), `namespace='grid-ns'`) {
			t.Fatalf("expected grid namespace inherited by the cell <bn-text>, got:\n%s", html)
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
		html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")
		if !strings.Contains(openTag(t, html, "bn-text"), `namespace='corp'`) {
			t.Fatalf("expected inherited namespace on the Label <bn-text>, got:\n%s", html)
		}
		if !strings.Contains(openTag(t, html, "bn-table"), `namespace='corp'`) {
			t.Fatalf("expected inherited namespace on the node <bn-table>, got:\n%s", html)
		}
	})
}

func TestI18nNamespaceInherit_EmptyStringOptsOut(t *testing.T) {
	doc := pageDoc(`{"children": [
		{"kind": "Table", "metadata": {"name": "optout"}, "spec": {"dataset": "test", "i18nNamespace": ""}},
		{"kind": "Text", "metadata": {"name": "inherits"}, "spec": {"value": "hi"}}
	]}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if strings.Contains(openTag(t, html, "bn-table"), "namespace=") {
		t.Fatalf("expected no namespace on the opted-out <bn-table>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-text"), `namespace='corp'`) {
		t.Fatalf("expected sibling <bn-text> to still inherit, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_StandaloneRootComponent(t *testing.T) {
	textDoc := makeTestDoc("Text", "standalone", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Text",
		"metadata": {"name": "standalone"},
		"spec": {"value": "hello"}
	}`))
	html := renderDocsWithArtefactNamespace(t, []config.Document{textDoc}, "embed-ns", "standalone")

	if !strings.Contains(openTag(t, html, "bn-text"), `namespace='embed-ns'`) {
		t.Fatalf("expected artefact namespace on standalone <bn-text>, got:\n%s", html)
	}
}

func TestI18nNamespaceInherit_Presentation(t *testing.T) {
	doc := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]}`)
	artifact := config.Artifact{
		Document: makeTestDoc("ReportArtefact", "deck", json.RawMessage(`{"kind": "ReportArtefact", "metadata": {"name": "deck"}, "spec": {}}`)),
		Spec:     config.ReportArtefactSpec{I18nNamespace: "corp"},
	}

	result, _, err := GeneratePresentationHTML(context.Background(), []config.Document{doc}, nil, artifact, config.PresentationConfig{}, nil, nil, "v1.0.0", nil, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePresentationHTML failed: %v", err)
	}
	html := string(result.HTML)

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `title-namespace='corp'`) {
		t.Fatalf("expected artefact namespace on the slide <bn-layout-page>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-table"), `namespace='corp'`) {
		t.Fatalf("expected artefact namespace inherited by the slide's <bn-table>, got:\n%s", html)
	}
}

func TestCollectInternationalizations_DefaultsNamespace(t *testing.T) {
	doc := makeTestDoc("Internationalization", "de", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Internationalization",
		"metadata": {"name": "de"},
		"spec": {"code": "de", "content": {"global.ac1": "Ist"}}
	}`))

	entries, err := collectInternationalizations([]config.Document{doc})
	if err != nil {
		t.Fatalf("collectInternationalizations failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].namespace != "_system" {
		t.Fatalf("expected default namespace _system, got %q", entries[0].namespace)
	}

	segments := renderInternationalizations(entries)
	if len(segments) != 1 || !strings.Contains(segments[0], `namespace='_system'`) {
		t.Fatalf("expected namespace='_system' in emitted element, got: %v", segments)
	}
}
