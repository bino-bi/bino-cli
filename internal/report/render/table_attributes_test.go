package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// renderTablePage renders a LayoutPage with a single Table child using the
// given spec JSON and returns the generated HTML.
func renderTablePage(t *testing.T, tableSpec string) string {
	t.Helper()

	layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{
					"kind": "Table",
					"metadata": {"name": "child"},
					"spec": `+tableSpec+`
				}
			]
		}
	}`))

	result, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{layoutPageDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}
	return string(result.HTML)
}

func TestTableAttributes_ArrayForm(t *testing.T) {
	html := renderTablePage(t, `{
		"dataset": "test",
		"attributes": [
			{"label": "Verkaufsleiter", "expression": "set(_leiter)"},
			{"label": "Umsatz gesamt", "expression": "sum(ac1)"}
		]
	}`)

	// writeAttr HTML-escapes the value, so quotes become &#34;.
	want := `attributes='{&#34;Verkaufsleiter&#34;:&#34;set(_leiter)&#34;,&#34;Umsatz gesamt&#34;:&#34;sum(ac1)&#34;}'`
	if !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

func TestTableAttributes_StringFormVerbatim(t *testing.T) {
	// Non-alphabetical key order and internal spacing must survive verbatim.
	html := renderTablePage(t, `{
		"dataset": "test",
		"attributes": "{\"Zebra\": \"sum(ac1)\", \"Alpha\": \"set(_leiter)\"}"
	}`)

	want := `attributes='{&#34;Zebra&#34;: &#34;sum(ac1)&#34;, &#34;Alpha&#34;: &#34;set(_leiter)&#34;}'`
	if !strings.Contains(html, want) {
		t.Fatalf("expected %s in HTML, got:\n%s", want, html)
	}
}

func TestTableAttributes_AbsentByDefault(t *testing.T) {
	html := renderTablePage(t, `{"dataset": "test"}`)

	if strings.Contains(html, "attributes=") {
		t.Fatalf("expected no attributes attribute in HTML, got:\n%s", html)
	}
}
