package markdown

import (
	"strings"
	"testing"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/render"
)

// TestWrapDocumentWithContext covers the full document shell: the KaTeX
// stylesheet link is emitted exactly when math is enabled, and the basic
// structure (doctype, bn-context, content section) is present.
func TestWrapDocumentWithContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		math        bool
		contains    []string
		notContains []string
	}{
		{
			name: "math enabled links the embedded katex stylesheet",
			math: true,
			contains: []string{
				"<link rel='stylesheet' href='/__bino/static/katex/katex.min.css'>",
			},
		},
		{
			name: "math disabled emits no katex link",
			math: false,
			notContains: []string{
				"katex.min.css",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			html, emitted := WrapDocumentWithContext([]byte("<h1>Test</h1>"), FullDocumentOptions{
				DocumentOptions: DocumentOptions{Title: "T", Format: "a4"},
				Locale:          "en",
				Math:            tt.math,
			})
			if emitted != nil {
				t.Errorf("inline mode must not emit data bodies, got %d", len(emitted))
			}
			got := string(html)
			for _, want := range append([]string{
				"<!DOCTYPE html>",
				"<bn-context locale='en'>",
				"<section class='bn-document-content'>",
				"<h1>Test</h1>",
			}, tt.contains...) {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q", want)
				}
			}
			for _, ban := range tt.notContains {
				if strings.Contains(got, ban) {
					t.Errorf("output must not contain %q", ban)
				}
			}
		})
	}
}

// TestWrapDocumentWithContextDataElements covers how the document shell
// delivers datasource and dataset payloads in both data modes.
func TestWrapDocumentWithContextDataElements(t *testing.T) {
	t.Parallel()

	sourceBody := []byte(`[{"v":1}]`)
	setBody := []byte(`[{"a":1}]`)
	wrap := func(dataMode string) (string, []render.EmittedData) {
		html, emitted := WrapDocumentWithContext([]byte("<h1>Test</h1>"), FullDocumentOptions{
			DocumentOptions: DocumentOptions{Title: "T", Format: "a4"},
			Locale:          "en",
			RenderContext: &RenderContext{
				DatasourceResults: []datasource.Result{{Name: "events", Data: sourceBody}},
				DatasetResults:    []dataset.Result{{Name: "sales", Data: setBody}},
				DataMode:          dataMode,
			},
		})
		return string(html), emitted
	}

	t.Run("url mode writes the URL into src and into the body", func(t *testing.T) {
		t.Parallel()
		got, emitted := wrap(render.DataModeURL)
		sourceURL := "/__bino/data/datasource/events?hash=" + render.ContentHash(sourceBody)
		setURL := "/__bino/data/dataset/sales?hash=" + render.ContentHash(setBody)
		for _, want := range []string{
			"<bn-datasource name='events' src='" + sourceURL + "'>" + sourceURL + "</bn-datasource>",
			"<bn-dataset name='sales' static='true' src='" + setURL + "'>" + setURL + "</bn-dataset>",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q", want)
			}
		}
		if len(emitted) != 2 {
			t.Errorf("emitted len = %d, want 2", len(emitted))
		}
	})

	// The page has <script src=...>, so match the whole opening tag.
	t.Run("inline mode sets no src", func(t *testing.T) {
		t.Parallel()
		got, emitted := wrap(render.DataModeInline)
		for _, want := range []string{
			"<bn-datasource name='events' raw='false'>",
			"<bn-dataset name='sales' static='true' raw='false'>",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q", want)
			}
		}
		if emitted != nil {
			t.Errorf("inline mode must not emit data bodies, got %d", len(emitted))
		}
	})
}
