package pipeline

import (
	"testing"

	"bino.bi/bino/internal/report/render"
)

func TestBuilderWaitForPlugins(t *testing.T) {
	components := &render.PluginOptions{HasComponentKinds: true}
	tests := []struct {
		name    string
		plugins *render.PluginOptions
		opts    PDFRenderOptions
		want    bool
	}{
		{"no plugin options", nil, PDFRenderOptions{WaitForComponentReady: true}, false},
		{"data mode only", &render.PluginOptions{DataMode: "url"}, PDFRenderOptions{WaitForComponentReady: true}, false},
		{"components, no wait", components, PDFRenderOptions{}, false},
		{"components, engine wait", components, PDFRenderOptions{WaitForComponentReady: true}, true},
		{"components, document", components, PDFRenderOptions{WaitForPlugins: true}, true},
		{"document without components", &render.PluginOptions{}, PDFRenderOptions{WaitForPlugins: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &Builder{PluginOptions: tt.plugins}
			got := b.chromePDFOptions("http://127.0.0.1/", tt.opts)
			if got.WaitForPlugins != tt.want {
				t.Errorf("PDF WaitForPlugins = %v, want %v", got.WaitForPlugins, tt.want)
			}
		})
	}
}

func TestBuilderPassesAllowIncompletePlugins(t *testing.T) {
	b := &Builder{PluginOptions: &render.PluginOptions{HasComponentKinds: true}}
	got := b.chromePDFOptions("http://127.0.0.1/", PDFRenderOptions{WaitForPlugins: true, AllowIncompletePlugins: true})
	if !got.AllowIncompletePlugins {
		t.Error("AllowIncompletePlugins was dropped")
	}
}
