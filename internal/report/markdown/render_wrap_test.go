package markdown

import (
	"strings"
	"testing"
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
