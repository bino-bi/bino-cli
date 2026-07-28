package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// TestRenderArtefactHTML_I18nNamespaceInheritance verifies that an
// artefact-level i18nNamespace is written as the i18n-namespace attribute on
// <bn-context>, from which the engine resolves it at runtime — nothing is
// stamped on pages or child components.
func TestRenderArtefactHTML_I18nNamespaceInheritance(t *testing.T) {
	ctx := context.Background()

	pageDoc := config.Document{
		Kind: "LayoutPage",
		Name: "page",
		File: "page.yaml",
		Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "LayoutPage",
			"metadata": {"name": "page"},
			"spec": {
				"children": [
					{"kind": "Text", "metadata": {"name": "child"}, "spec": {"value": "hello"}}
				]
			}
		}`),
	}

	artefactRaw := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "ReportArtefact",
		"metadata": {"name": "report"},
		"spec": {"filename": "report.pdf", "title": "Report", "i18nNamespace": "corp"}
	}`)
	artifact := config.Artifact{
		Document: config.Document{Kind: "ReportArtefact", Name: "report", File: "report.yaml", Raw: artefactRaw},
		Spec: config.ReportArtefactSpec{
			Format:        config.DefaultArtefactFormat,
			Orientation:   config.DefaultArtefactOrientation,
			Language:      "en",
			Filename:      "report.pdf",
			Title:         "Report",
			LayoutPages:   config.LayoutPagesOrRefs{{Page: "*"}},
			I18nNamespace: "corp",
		},
	}

	result, err := RenderArtefactHTML(ctx, t.TempDir(), []config.Document{pageDoc}, artifact, RenderArtefactOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}

	html := string(result.HTML)
	contextTag := html[strings.Index(html, "<bn-context"):]
	contextTag = contextTag[:strings.Index(contextTag, ">")]
	if !strings.Contains(contextTag, `i18n-namespace='corp'`) {
		t.Fatalf("expected artefact i18nNamespace on <bn-context>, got:\n%s", contextTag)
	}
	textTag := html[strings.Index(html, "<bn-text"):]
	textTag = textTag[:strings.Index(textTag, ">")]
	if strings.Contains(textTag, "i18n-namespace=") {
		t.Fatalf("expected no i18n-namespace stamped on <bn-text> (runtime inheritance), got:\n%s", textTag)
	}
}
