package render

import (
	"strings"
	"testing"
)

func TestRenderMarkdown(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assetURLs map[string]string
		want      string // exact match (only for empty case)
		wantSub   string // substring that must appear
		wantNot   string // substring that must NOT appear (empty = skip)
	}{
		{
			name: "empty input",
			want: "",
		},
		{
			name:    "bold text",
			input:   "**bold**",
			wantSub: "<strong>bold</strong>",
		},
		{
			name:    "italic text",
			input:   "*italic*",
			wantSub: "<em>italic</em>",
		},
		{
			name:    "GFM table",
			input:   "| A | B |\n|---|---|\n| 1 | 2 |",
			wantSub: "<table>",
		},
		{
			name:    "GFM table has thead",
			input:   "| A | B |\n|---|---|\n| 1 | 2 |",
			wantSub: "<thead>",
		},
		{
			name:    "GFM table has tbody",
			input:   "| A | B |\n|---|---|\n| 1 | 2 |",
			wantSub: "<tbody>",
		},
		{
			name:    "table cell alignment",
			input:   "| Left | Center | Right |\n|:-----|:------:|------:|\n| a | b | c |",
			wantSub: `text-align:center`,
		},
		{
			name:    "strikethrough",
			input:   "~~deleted~~",
			wantSub: "<del>deleted</del>",
		},
		{
			name:    "autolink",
			input:   "Visit https://example.com for info",
			wantSub: `href="https://example.com"`,
		},
		{
			name:      "asset image with GFM",
			input:     "![Logo](asset:logo)",
			assetURLs: map[string]string{"logo": "/assets/logo.png"},
			wantSub:   `src="/assets/logo.png"`,
		},
		{
			name:      "table and asset image together",
			input:     "| Col |\n|-----|\n| val |\n\n![Img](asset:img)",
			assetURLs: map[string]string{"img": "/assets/img.png"},
			wantSub:   "<table>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderMarkdown(tt.input, tt.assetURLs)

			if tt.want != "" || (tt.input == "" && tt.wantSub == "") {
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
				return
			}

			if tt.wantSub != "" && !strings.Contains(got, tt.wantSub) {
				t.Errorf("output missing %q\ngot: %s", tt.wantSub, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("output should not contain %q\ngot: %s", tt.wantNot, got)
			}
		})
	}
}
