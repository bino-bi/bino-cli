package render

import (
	"encoding/json"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestRenderInlineMarkdown(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assetURLs map[string]string
		want      string
	}{
		{name: "empty input", input: "", want: ""},

		// Plain values must survive byte-identical.
		{name: "plain text", input: "Acme Corp", want: "Acme Corp"},
		{name: "apostrophe", input: "it's here", want: "it's here"},
		{name: "ampersand is escaped", input: "Sales & Marketing", want: "Sales &amp; Marketing"},

		// Block Markdown must stay literal: these are ordinary title/message values.
		{name: "ordered list marker stays literal", input: "1. Quartal 2024", want: "1. Quartal 2024"},
		{name: "heading marker stays literal", input: "# 1 Vertrieb", want: "# 1 Vertrieb"},
		{name: "bullet marker stays literal", input: "- Sales", want: "- Sales"},
		{name: "thematic break stays literal", input: "---", want: "---"},
		{name: "email is not autolinked", input: "sales@acme.com", want: "sales@acme.com"},
		{name: "table stays literal", input: "| A | B |", want: "| A | B |"},

		// Inline Markdown and inline HTML are what the fields are for.
		{name: "bold", input: "**bold**", want: "<strong>bold</strong>"},
		{name: "italic", input: "*italic*", want: "<em>italic</em>"},
		{name: "strikethrough", input: "~~gone~~", want: "<del>gone</del>"},
		{name: "explicit line break", input: "Zeile 1<br />Zeile 2", want: "Zeile 1<br />Zeile 2"},
		{name: "mixed markup", input: "Revenue **above plan**<br/>Q3 2026", want: "Revenue <strong>above plan</strong><br/>Q3 2026"},

		// A lone newline is a soft break (CommonMark), a blank line starts a paragraph.
		{name: "single newline stays a soft break", input: "Zeile 1\nZeile 2", want: "Zeile 1\nZeile 2"},
		{name: "blank line keeps paragraphs", input: "erster\n\nzweiter", want: "<p>erster</p>\n<p>zweiter</p>"},

		{
			name:      "asset image reference is resolved",
			input:     "![logo](asset:logo)",
			assetURLs: map[string]string{"logo": "/assets/logo.png"},
			want:      `<img src="/assets/logo.png" alt="logo">`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderInlineMarkdown(tt.input, tt.assetURLs)
			if got != tt.want {
				t.Errorf("renderInlineMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestLayoutAttrs_InlineMarkdown asserts the rendered attributes on the layout
// elements, which is what the template engine sanitizes and injects as HTML.
func TestLayoutAttrs_InlineMarkdown(t *testing.T) {
	pageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"titleBusinessUnit": "Acme **Corp**",
			"messageText": "Placeholder message<br />second line",
			"children": [
				{
					"kind": "LayoutCard",
					"metadata": {"name": "card"},
					"spec": {"titleBusinessUnit": "Sales *EMEA*", "children": []}
				}
			]
		}
	}`))

	result, _, err := GenerateHTMLFromDocuments(t.Context(), []config.Document{pageDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}
	html := string(result.HTML)

	// html.EscapeString escapes the markup into the single-quoted attribute; the
	// browser decodes it back to an HTML string when the component reads the prop.
	want := []string{
		`message-text='Placeholder message&lt;br /&gt;second line'`,
		`title-business-unit='Acme &lt;strong&gt;Corp&lt;/strong&gt;'`,
		`title-business-unit='Sales &lt;em&gt;EMEA&lt;/em&gt;'`,
	}
	for _, w := range want {
		if !strings.Contains(html, w) {
			t.Errorf("expected %s in HTML, got:\n%s", w, html)
		}
	}
}

func TestLayoutAttrs_PlainValuesUnchanged(t *testing.T) {
	pageDoc := makeTestDoc("LayoutPage", "page", json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {"titleBusinessUnit": "1. Quartal 2024", "messageText": "Placeholder message"}
	}`))

	result, _, err := GenerateHTMLFromDocuments(t.Context(), []config.Document{pageDoc}, "en", "", "", ModePreview, "v1.0.0")
	if err != nil {
		t.Fatalf("GenerateHTMLFromDocuments failed: %v", err)
	}
	html := string(result.HTML)

	for _, w := range []string{`title-business-unit='1. Quartal 2024'`, `message-text='Placeholder message'`} {
		if !strings.Contains(html, w) {
			t.Errorf("expected %s in HTML, got:\n%s", w, html)
		}
	}
}
