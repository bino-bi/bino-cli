package refresh

import (
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

// TestEmbedRenderOptsEmitRelativeDataURLs guards the design-mode fix: the
// preview/embed render must emit same-origin RELATIVE data URLs in url mode,
// never an absolute base pinned to one host. The preview server binds 127.0.0.1
// but the VS Code webview iframe loads /__embedding from localhost; an absolute
// 127.0.0.1 data base would make the engine's cross-origin fetch fail ("No
// Data"). preview.go therefore leaves PluginOptions.DataBaseURL empty in url
// mode. This test asserts the embed option builders propagate that empty base
// (with DataMode still "url") so it reaches render.buildDataURL — which, given
// an empty base, produces a relative "/__bino/data/..." path (see
// render.TestRenderDatasetsURLMode).
func TestEmbedRenderOptsEmitRelativeDataURLs(t *testing.T) {
	t.Parallel()

	// Exactly what post-fix preview.go produces for url mode.
	cfg := &Config{
		PluginOptions: &render.PluginOptions{
			DataMode:    render.DataModeURL,
			DataBaseURL: "",
		},
	}

	t.Run("embedRenderOpts", func(t *testing.T) {
		t.Parallel()
		opts := embedRenderOpts(cfg)
		if opts.PluginOptions == nil {
			t.Fatal("embedRenderOpts dropped PluginOptions")
		}
		if opts.PluginOptions.DataMode != render.DataModeURL {
			t.Errorf("DataMode = %q, want %q", opts.PluginOptions.DataMode, render.DataModeURL)
		}
		if opts.PluginOptions.DataBaseURL != "" {
			t.Errorf("DataBaseURL = %q, want empty (same-origin relative URLs)", opts.PluginOptions.DataBaseURL)
		}
	})

	t.Run("embedComponentOpts", func(t *testing.T) {
		t.Parallel()
		opts := embedComponentOpts(cfg, "standalone_table")
		if opts.PluginOptions == nil {
			t.Fatal("embedComponentOpts dropped PluginOptions")
		}
		if opts.PluginOptions.DataMode != render.DataModeURL {
			t.Errorf("DataMode = %q, want %q", opts.PluginOptions.DataMode, render.DataModeURL)
		}
		if opts.PluginOptions.DataBaseURL != "" {
			t.Errorf("DataBaseURL = %q, want empty (same-origin relative URLs)", opts.PluginOptions.DataBaseURL)
		}
	})
}

func TestPageFormatAndOrientation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		raw             string
		wantFormat      string
		wantOrientation string
	}{
		{
			name:            "explicit format and orientation",
			raw:             `{"kind":"LayoutPage","spec":{"pageFormat":"a4","pageOrientation":"portrait"}}`,
			wantFormat:      "a4",
			wantOrientation: "portrait",
		},
		{
			name:            "missing format falls back to defaults",
			raw:             `{"kind":"LayoutPage","spec":{}}`,
			wantFormat:      config.DefaultArtefactFormat,
			wantOrientation: config.DefaultArtefactOrientation,
		},
		{
			name:            "empty raw falls back to defaults",
			raw:             ``,
			wantFormat:      config.DefaultArtefactFormat,
			wantOrientation: config.DefaultArtefactOrientation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := config.Document{Kind: "LayoutPage", Name: "p", Raw: []byte(tt.raw)}
			gotF, gotO := pageFormatAndOrientation(doc)
			if gotF != tt.wantFormat {
				t.Errorf("format = %q, want %q", gotF, tt.wantFormat)
			}
			if gotO != tt.wantOrientation {
				t.Errorf("orientation = %q, want %q", gotO, tt.wantOrientation)
			}
		})
	}
}

// TestSyntheticPageArtefactAdoptsPageFormat guards the fix for the empty-context
// bug: the synthetic artefact must adopt the page's own pageFormat, otherwise the
// artefact-vs-page format filter in renderLayoutPage drops the page and the
// /__embedding response renders an empty <bn-context>.
func TestSyntheticPageArtefactAdoptsPageFormat(t *testing.T) {
	t.Parallel()

	page := config.Document{
		Kind: "LayoutPage",
		Name: "bn201_example_table_and_text",
		Raw:  []byte(`{"kind":"LayoutPage","spec":{"pageFormat":"a4","pageOrientation":"landscape"}}`),
	}
	art := syntheticPageArtefact(page)

	if art.Spec.Format != "a4" {
		t.Errorf("synthetic artefact format = %q, want %q (the page format must win, else the page is filtered out)", art.Spec.Format, "a4")
	}
	if art.Spec.Orientation != "landscape" {
		t.Errorf("synthetic artefact orientation = %q, want %q", art.Spec.Orientation, "landscape")
	}
	if len(art.Spec.LayoutPages) != 1 || art.Spec.LayoutPages[0].Page != page.Name {
		t.Errorf("synthetic artefact must reference exactly page %q, got %+v", page.Name, art.Spec.LayoutPages)
	}
	if art.Document.Kind != "ReportArtefact" {
		t.Errorf("synthetic artefact kind = %q, want ReportArtefact", art.Document.Kind)
	}
}
