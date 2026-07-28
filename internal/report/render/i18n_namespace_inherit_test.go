package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// The engine resolves each component's namespace at runtime from its own
// i18n-namespace attribute or the nearest ancestor carrying one (layout card,
// layout page, tree, grid, or bn-context). The CLI therefore only emits the
// attribute where it is authored — it never stamps inherited values on
// children — and writes the artefact-level namespace on <bn-context>.

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

func TestI18nNamespace_ArtefactLevelOnContext(t *testing.T) {
	doc := pageDoc(`{"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-context"), `i18n-namespace='corp'`) {
		t.Fatalf("expected artefact namespace on <bn-context>, got:\n%s", html)
	}
	// Runtime inheritance: no stamping on pages or children.
	if strings.Contains(openTag(t, html, "bn-layout-page"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on <bn-layout-page>, got:\n%s", html)
	}
	if strings.Contains(openTag(t, html, "bn-table"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on <bn-table>, got:\n%s", html)
	}
}

func TestI18nNamespace_NoArtefactValueNoAttr(t *testing.T) {
	doc := pageDoc(`{"children": []}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	if strings.Contains(openTag(t, html, "bn-context"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace on <bn-context>, got:\n%s", html)
	}
}

func TestI18nNamespace_AuthoredOnComponents(t *testing.T) {
	tests := []struct {
		kind string
		spec string
		tag  string
	}{
		{"Table", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-table"},
		{"Text", `{"value": "hello", "i18nNamespace": "own-ns"}`, "bn-text"},
		{"ChartStructure", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-chart-structure"},
		{"ChartTime", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-chart-time"},
		{"ChartScatter", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-chart-scatter"},
		{"ChartBubble", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-chart-bubble"},
		{"ChartBullet", `{"dataset": "test", "i18nNamespace": "own-ns"}`, "bn-chart-bullet"},
		{"Tree", `{"edges": [], "nodes": [], "i18nNamespace": "own-ns"}`, "bn-tree"},
		{"Grid", `{"children": [], "i18nNamespace": "own-ns"}`, "bn-grid"},
		{"LayoutCard", `{"children": [], "i18nNamespace": "own-ns"}`, "bn-layout-card"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			doc := pageDoc(`{"children": [{"kind": "` + tt.kind + `", "metadata": {"name": "child"}, "spec": ` + tt.spec + `}]}`)
			html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")
			if !strings.Contains(openTag(t, html, tt.tag), `i18n-namespace='own-ns'`) {
				t.Fatalf("expected authored i18n-namespace='own-ns' on <%s>, got:\n%s", tt.tag, html)
			}
		})
	}
}

func TestI18nNamespace_PageEmitsOwnAttr(t *testing.T) {
	doc := pageDoc(`{
		"i18nNamespace": "page-ns",
		"children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]
	}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "corp", "")

	if !strings.Contains(openTag(t, html, "bn-layout-page"), `i18n-namespace='page-ns'`) {
		t.Fatalf("expected page's own i18n-namespace attr, got:\n%s", html)
	}
	// The child inherits at runtime; nothing stamped.
	if strings.Contains(openTag(t, html, "bn-table"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on <bn-table>, got:\n%s", html)
	}
}

func TestI18nNamespace_TitleNamespaceEmittedVerbatim(t *testing.T) {
	doc := pageDoc(`{"titleNamespace": "legacy", "children": []}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	pageTag := openTag(t, html, "bn-layout-page")
	if !strings.Contains(pageTag, `title-namespace='legacy'`) {
		t.Fatalf("expected deprecated titleNamespace as title-namespace, got:\n%s", html)
	}
	if strings.Contains(pageTag, "i18n-namespace=") {
		t.Fatalf("expected titleNamespace NOT to become i18n-namespace, got:\n%s", html)
	}
}

func TestI18nNamespace_CardEmitsBothAttrs(t *testing.T) {
	doc := pageDoc(`{
		"children": [{"kind": "LayoutCard", "metadata": {"name": "card"}, "spec": {
			"titleNamespace": "legacy",
			"i18nNamespace": "card-ns",
			"children": [{"kind": "Table", "metadata": {"name": "inner"}, "spec": {"dataset": "test"}}]
		}}]
	}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	cardTag := openTag(t, html, "bn-layout-card")
	if !strings.Contains(cardTag, `i18n-namespace='card-ns'`) || !strings.Contains(cardTag, `title-namespace='legacy'`) {
		t.Fatalf("expected both namespace attrs on <bn-layout-card>, got:\n%s", cardTag)
	}
	if strings.Contains(openTag(t, html, "bn-table"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on the card's <bn-table>, got:\n%s", html)
	}
}

func TestI18nNamespace_TreeLabelHasNoAttr(t *testing.T) {
	doc := pageDoc(`{"children": [{"kind": "Tree", "metadata": {"name": "tree"}, "spec": {
		"edges": [],
		"i18nNamespace": "tree-ns",
		"nodes": [{"id": "n1", "kind": "Label", "spec": {"value": "label"}}]
	}}]}`)
	html := renderDocsWithArtefactNamespace(t, []config.Document{doc}, "", "")

	if !strings.Contains(openTag(t, html, "bn-tree"), `i18n-namespace='tree-ns'`) {
		t.Fatalf("expected tree's own i18n-namespace attr, got:\n%s", html)
	}
	if strings.Contains(openTag(t, html, "bn-text"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace on the Label <bn-text> (inherits at runtime), got:\n%s", html)
	}
}

func TestI18nNamespace_StandaloneRootInheritsFromContext(t *testing.T) {
	textDoc := makeTestDoc("Text", "standalone", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Text",
		"metadata": {"name": "standalone"},
		"spec": {"value": "hello"}
	}`))
	html := renderDocsWithArtefactNamespace(t, []config.Document{textDoc}, "embed-ns", "standalone")

	if !strings.Contains(openTag(t, html, "bn-context"), `i18n-namespace='embed-ns'`) {
		t.Fatalf("expected artefact namespace on <bn-context>, got:\n%s", html)
	}
	if strings.Contains(openTag(t, html, "bn-text"), "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on standalone <bn-text>, got:\n%s", html)
	}
}

func TestI18nNamespace_Presentation(t *testing.T) {
	doc := pageDoc(`{"i18nNamespace": "page-ns", "children": [{"kind": "Table", "metadata": {"name": "child"}, "spec": {"dataset": "test"}}]}`)
	artifact := config.Artifact{
		Document: makeTestDoc("ReportArtefact", "deck", json.RawMessage(`{"kind": "ReportArtefact", "metadata": {"name": "deck"}, "spec": {}}`)),
		Spec:     config.ReportArtefactSpec{I18nNamespace: "corp"},
	}

	result, _, err := GeneratePresentationHTML(context.Background(), []config.Document{doc}, nil, artifact, config.PresentationConfig{}, nil, nil, "v1.0.0", nil, nil, nil)
	if err != nil {
		t.Fatalf("GeneratePresentationHTML failed: %v", err)
	}
	html := string(result.HTML)

	if !strings.Contains(openTag(t, html, "bn-context"), `i18n-namespace='corp'`) {
		t.Fatalf("expected artefact namespace on the presentation <bn-context>, got:\n%s", html)
	}
	if !strings.Contains(openTag(t, html, "bn-layout-page"), `i18n-namespace='page-ns'`) {
		t.Fatalf("expected page's own i18n-namespace on the slide, got:\n%s", html)
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
