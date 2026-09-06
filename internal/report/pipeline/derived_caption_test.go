package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func derivedCaptionDocs(shift string, authored bool) []config.Document {
	docs := []config.Document{
		{
			Kind: "DataSet",
			Name: "sales",
			File: "sales.yaml",
			Raw: json.RawMessage(fmt.Sprintf(`{
				"apiVersion": "bino.bi/v1",
				"kind": "DataSet",
				"metadata": {"name": "sales"},
				"spec": {
					"query": "SELECT 1 AS ac1, '2024-01-31' AS date",
					"derive": {"pp1": {"from": "ac1", "shift": %q, "grain": "month"}}
				}
			}`, shift)),
		},
		{
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
		},
	}
	if authored {
		docs = append(docs, config.Document{
			Kind: "Internationalization",
			Name: "en",
			File: "en.yaml",
			Raw: json.RawMessage(`{
				"apiVersion": "bino.bi/v1",
				"kind": "Internationalization",
				"metadata": {"name": "en"},
				"spec": {"code": "en", "content": {"global.pp1": "Prev. month"}}
			}`),
		})
	}
	return docs
}

func renderDerivedCaptionHTML(t *testing.T, shift string, authored bool) string {
	t.Helper()
	artifact := config.Artifact{
		Document: config.Document{Kind: "ReportArtefact", Name: "report", File: "report.yaml", Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1",
			"kind": "ReportArtefact",
			"metadata": {"name": "report"},
			"spec": {"filename": "report.pdf", "title": "Report", "language": "en"}
		}`)},
		Spec: config.ReportArtefactSpec{
			Format:      config.DefaultArtefactFormat,
			Orientation: config.DefaultArtefactOrientation,
			Language:    "en",
			Filename:    "report.pdf",
			Title:       "Report",
			LayoutPages: config.LayoutPagesOrRefs{{Page: "*"}},
		},
	}
	result, err := RenderArtefactHTML(context.Background(), t.TempDir(), derivedCaptionDocs(shift, authored), artifact, RenderArtefactOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render artefact html: %v", err)
	}
	return string(result.HTML)
}

func TestRenderArtefactHTML_DerivedMonthShiftCaptionsPP(t *testing.T) {
	html := renderDerivedCaptionHTML(t, "1 month", false)
	const open = `<bn-internationalization code='en' namespace='_system'>`
	if strings.Count(html, "<bn-internationalization") != 1 || !strings.Contains(html, open) {
		t.Fatalf("expected exactly one synthesized _system bundle for en, got:\n%s", html)
	}
	body := html[strings.Index(html, open)+len(open):]
	body = body[:strings.Index(body, "</bn-internationalization>")]
	if body != `{&#34;global.pp1&#34;:&#34;PP&#34;}` {
		t.Fatalf("expected global.pp1 = PP, got %s", body)
	}
}

func TestRenderArtefactHTML_DerivedYearShiftEmitsNothing(t *testing.T) {
	html := renderDerivedCaptionHTML(t, "1 year", false)
	if strings.Contains(html, "<bn-internationalization") {
		t.Fatalf("a year shift keeps the engine default, expected no bundle, got:\n%s", html)
	}
}

func TestRenderArtefactHTML_ProjectBundleFollowsDerivedCaption(t *testing.T) {
	html := renderDerivedCaptionHTML(t, "1 month", true)
	synthesized := strings.Index(html, "PP&#34;}")
	authored := strings.Index(html, "Prev. month")
	if synthesized < 0 || authored < 0 {
		t.Fatalf("expected both bundles, got:\n%s", html)
	}
	// The engine merges same-code _system bundles in DOM order, later wins per
	// key, so the authored one has to be emitted after the synthesized one.
	if synthesized > authored {
		t.Fatalf("authored bundle must follow the synthesized caption so it wins:\n%s", html)
	}
}
