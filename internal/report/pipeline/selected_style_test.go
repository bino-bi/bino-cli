package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// TestRenderArtefactHTML_SelectedStyleInheritance verifies that an artefact-level
// selectedStyle is stamped on pages and inherited by child components.
func TestRenderArtefactHTML_SelectedStyleInheritance(t *testing.T) {
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
		"spec": {"filename": "report.pdf", "title": "Report", "selectedStyle": "corp"}
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
			SelectedStyle: "corp",
		},
	}

	result, err := RenderArtefactHTML(ctx, t.TempDir(), []config.Document{pageDoc}, artifact, RenderArtefactOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}

	html := string(result.HTML)
	pageTag := html[strings.Index(html, "<bn-layout-page"):]
	pageTag = pageTag[:strings.Index(pageTag, ">")]
	if !strings.Contains(pageTag, `selected-style='corp'`) {
		t.Fatalf("expected artefact selectedStyle on <bn-layout-page>, got:\n%s", pageTag)
	}
	if !strings.Contains(html, `<bn-text`) || !strings.Contains(html, `selected-style='corp'`) {
		t.Fatalf("expected artefact selectedStyle inherited by <bn-text>, got:\n%s", html)
	}
	textTag := html[strings.Index(html, "<bn-text"):]
	textTag = textTag[:strings.Index(textTag, ">")]
	if !strings.Contains(textTag, `selected-style='corp'`) {
		t.Fatalf("expected artefact selectedStyle on <bn-text>, got:\n%s", textTag)
	}
}
