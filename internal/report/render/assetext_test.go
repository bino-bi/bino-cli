package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

func TestAssetExtension(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assetURLs map[string]string
		wantSub   string // substring that must appear in output
		wantNot   string // substring that must NOT appear (empty = skip check)
	}{
		{
			name:      "local asset resolved",
			input:     "![Logo](asset:company_logo)",
			assetURLs: map[string]string{"company_logo": "/assets/files/company_logo"},
			wantSub:   `src="/assets/files/company_logo"`,
		},
		{
			name:      "base64 asset resolved",
			input:     "![Icon](asset:icon)",
			assetURLs: map[string]string{"icon": "data:image/png;base64,abc123"},
			wantSub:   `src="data:image/png;base64,abc123"`,
		},
		{
			name:      "remote asset resolved",
			input:     "![Photo](asset:photo)",
			assetURLs: map[string]string{"photo": "https://example.com/photo.jpg"},
			wantSub:   `src="https://example.com/photo.jpg"`,
		},
		{
			name:      "unknown asset gets broken marker",
			input:     "![Missing](asset:missing)",
			assetURLs: map[string]string{"other": "/assets/files/other"},
			wantSub:   `src="#asset-not-found:missing"`,
		},
		{
			name:      "non-asset image unchanged",
			input:     "![Cat](https://example.com/cat.png)",
			assetURLs: map[string]string{"company_logo": "/assets/files/company_logo"},
			wantSub:   `src="https://example.com/cat.png"`,
			wantNot:   "asset-not-found",
		},
		{
			name:  "mixed content",
			input: "# Title\n\n![Logo](asset:logo)\n\nSome text.\n\n![Cat](https://cat.png)",
			assetURLs: map[string]string{
				"logo": "/assets/files/logo",
			},
			wantSub: `src="/assets/files/logo"`,
			wantNot: "asset:logo",
		},
		{
			name:      "nil map is no-op",
			input:     "![Logo](asset:logo)",
			assetURLs: nil,
			wantSub:   `src="asset:logo"`,
		},
		{
			name:      "empty map is no-op",
			input:     "![Logo](asset:logo)",
			assetURLs: map[string]string{},
			wantSub:   `src="asset:logo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []goldmark.Option{goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe())}
			if len(tt.assetURLs) > 0 {
				opts = append(opts, goldmark.WithExtensions(NewAssetExtension(tt.assetURLs)))
			}
			md := goldmark.New(opts...)

			var buf bytes.Buffer
			if err := md.Convert([]byte(tt.input), &buf); err != nil {
				t.Fatalf("goldmark.Convert: %v", err)
			}
			got := buf.String()

			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("output missing %q\ngot: %s", tt.wantSub, got)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("output should not contain %q\ngot: %s", tt.wantNot, got)
			}
		})
	}
}
