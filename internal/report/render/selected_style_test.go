package render

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// renderPageWithChild renders a LayoutPage with a single child of the given
// kind and spec JSON and returns the generated HTML.
func renderPageWithChild(t *testing.T, kind, childSpec string) string {
	t.Helper()

	layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{
					"kind": "`+kind+`",
					"metadata": {"name": "child"},
					"spec": `+childSpec+`
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

func TestSelectedStyle_EmittedForChildren(t *testing.T) {
	tests := []struct {
		kind string
		spec string
	}{
		{"Table", `{"dataset": "test", "selectedStyle": "corporate-style"}`},
		{"Text", `{"value": "hello", "selectedStyle": "corporate-style"}`},
		{"ChartStructure", `{"dataset": "test", "selectedStyle": "corporate-style"}`},
		{"ChartTime", `{"dataset": "test", "selectedStyle": "corporate-style"}`},
		{"Tree", `{"edges": [], "nodes": [], "selectedStyle": "corporate-style"}`},
		{"Grid", `{"children": [], "selectedStyle": "corporate-style"}`},
		{"Image", `{"source": "logo.png", "selectedStyle": "corporate-style"}`},
		{"LayoutCard", `{"children": [], "selectedStyle": "corporate-style"}`},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			html := renderPageWithChild(t, tt.kind, tt.spec)
			if !strings.Contains(html, `selected-style='corporate-style'`) {
				t.Fatalf("expected selected-style='corporate-style' in HTML for %s, got:\n%s", tt.kind, html)
			}
		})
	}
}

func TestSelectedStyle_LayoutPage(t *testing.T) {
	layoutPageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"selectedStyle": "corporate-style",
			"children": []
		}
	}`))

	result, _, err := GenerateHTMLFromDocuments(context.Background(), []config.Document{layoutPageDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}
	html := string(result.HTML)

	pageTag := html[strings.Index(html, "<bn-layout-page"):]
	pageTag = pageTag[:strings.Index(pageTag, ">")]
	if !strings.Contains(pageTag, `selected-style='corporate-style'`) {
		t.Fatalf("expected selected-style='corporate-style' on <bn-layout-page> tag, got:\n%s", pageTag)
	}
}

func TestSelectedStyle_AbsentByDefault(t *testing.T) {
	html := renderPageWithChild(t, "Table", `{"dataset": "test"}`)

	if strings.Contains(html, "selected-style=") {
		t.Fatalf("expected no selected-style attribute in HTML, got:\n%s", html)
	}
}

func TestComponentFromSpec_SelectedStyle(t *testing.T) {
	tests := []struct {
		kind string
		spec string
	}{
		{"Text", `{"value": "hello", "selectedStyle": "corporate-style"}`},
		{"Image", `{"source": "logo.png", "selectedStyle": "corporate-style"}`},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			html, err := ComponentFromSpec(tt.kind, json.RawMessage(tt.spec), nil)
			if err != nil {
				t.Fatalf("ComponentFromSpec failed: %v", err)
			}
			if !strings.Contains(html, `selected-style='corporate-style'`) {
				t.Fatalf("expected selected-style='corporate-style' in HTML, got:\n%s", html)
			}
		})
	}
}
