package refresh

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

const tableManifest = `apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: standalone_table
spec:
  dataset: financial_summary
  scenarios:
    - ac1
    - pp1
`

// TestResolveEmbedTargetComponent confirms a standalone component document is
// resolved as compDoc and a ReportArtefact wins by priority when kind is empty.
func TestResolveEmbedTargetComponent(t *testing.T) {
	t.Parallel()

	tableDoc := config.Document{Kind: "Table", Name: "standalone_table", Raw: []byte(`{"kind":"Table"}`)}
	docs := []config.Document{tableDoc}

	t.Run("component by kind", func(t *testing.T) {
		t.Parallel()
		got := resolveEmbedTarget("standalone_table", "Table", nil, nil, docs)
		if !got.found() || got.compDoc == nil {
			t.Fatalf("expected compDoc resolved, got %+v", got)
		}
		if got.compDoc.Name != "standalone_table" {
			t.Errorf("compDoc.Name = %q, want standalone_table", got.compDoc.Name)
		}
	})

	t.Run("artefact priority when kind empty", func(t *testing.T) {
		t.Parallel()
		art := config.Artifact{Document: config.Document{Kind: "ReportArtefact", Name: "shared"}}
		comp := config.Document{Kind: "Table", Name: "shared", Raw: []byte(`{"kind":"Table"}`)}
		got := resolveEmbedTarget("shared", "", []config.Artifact{art}, nil, []config.Document{comp})
		if got.reportArt == nil || got.compDoc != nil {
			t.Fatalf("expected ReportArtefact to win priority, got %+v", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		got := resolveEmbedTarget("missing", "", nil, nil, docs)
		if got.found() {
			t.Fatalf("expected not found, got %+v", got)
		}
	})
}

// TestEmbedByNameOverrideBypassesCache proves the override path: when a live
// override is set, EmbedByName ignores embeddingCache and renders from a FRESH
// overlaid load of the workdir. The override here removes the embeddable target
// (an empty document), so embedFromOverlay returns 404 BEFORE any render —
// evidence that the overlaid load (not the cache, not state.lastDocs) drove the
// result. A pre-seeded cache entry is neither returned nor mutated.
func TestEmbedByNameOverrideBypassesCache(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "standalone.yaml")
	if err := os.WriteFile(file, []byte(tableManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	srv, err := httpserver.New(httpserver.Config{})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}
	cfg := &Config{Workdir: dir}

	mu := &sync.Mutex{}
	state := NewState()
	// Pre-seed the cache and a disk snapshot for the SAME key so we can prove
	// the override path ignores both.
	cacheKey := "Table:standalone_table"
	state.embeddingCache = map[string][]byte{cacheKey: []byte("STALE-CACHED-HTML")}
	state.lastDocs = []config.Document{{
		Kind: "Table", Name: "standalone_table", Raw: []byte(`{"kind":"Table"}`),
	}}

	// Override the file with content that has NO embeddable document, so the
	// fresh overlaid load resolves nothing and returns 404 before rendering.
	state.SetLiveOverride(file, "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: not_embeddable\nspec:\n  source: x\n  query: SELECT 1\n")

	_, err = EmbedByName(context.Background(), "standalone_table", "Table", mu, state, cfg, srv)
	if err == nil {
		t.Fatal("expected not-found error from overlaid load, got nil (cache or disk snapshot was used)")
	}
	var httpErr *httpserver.HTTPError
	if !errors.As(err, &httpErr) || httpErr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 HTTPError from overlaid load, got %v", err)
	}

	// The override path must not consult or overwrite the cache.
	mu.Lock()
	cached := string(state.embeddingCache[cacheKey])
	mu.Unlock()
	if cached != "STALE-CACHED-HTML" {
		t.Errorf("embeddingCache mutated by override path: %q", cached)
	}
}

// TestEmbedByNameDiskPathUsesCache confirms the non-override path is unchanged:
// with no live overrides, a cached entry is returned verbatim (no render, no
// disk read).
func TestEmbedByNameDiskPathUsesCache(t *testing.T) {
	t.Parallel()

	srv, err := httpserver.New(httpserver.Config{})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}
	cfg := &Config{Workdir: t.TempDir()}

	mu := &sync.Mutex{}
	state := NewState()
	cacheKey := "Table:standalone_table"
	want := []byte("CACHED-HTML")
	state.embeddingCache = map[string][]byte{cacheKey: want}

	got, err := EmbedByName(context.Background(), "standalone_table", "Table", mu, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName (disk path) = %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("disk path returned %q, want cached %q", got, want)
	}
}

// TestLiveOverrideLifecycle exercises the State override accessors.
func TestLiveOverrideLifecycle(t *testing.T) {
	t.Parallel()

	state := NewState()
	if snap := state.liveOverridesSnapshot(); snap != nil {
		t.Fatalf("empty state snapshot = %v, want nil", snap)
	}

	abs, _ := filepath.Abs("/tmp/x/a.yaml")
	state.SetLiveOverride(abs, "content-a")
	// A non-clean form of the same path must hit the same key.
	state.SetLiveOverride(abs+"/../a.yaml", "content-a2")

	snap := state.liveOverridesSnapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1 (keys must normalize)", len(snap))
	}
	if snap[abs] != "content-a2" {
		t.Errorf("snapshot[%q] = %q, want content-a2", abs, snap[abs])
	}
	// Snapshot is a copy: mutating it must not affect state.
	snap[abs] = "mutated"
	if state.liveOverridesSnapshot()[abs] != "content-a2" {
		t.Error("snapshot is not a defensive copy")
	}

	state.ClearLiveOverride(abs)
	if snap := state.liveOverridesSnapshot(); snap != nil {
		t.Fatalf("after clear, snapshot = %v, want nil", snap)
	}

	state.SetLiveOverride(abs, "x")
	state.ClearLiveOverrides()
	if snap := state.liveOverridesSnapshot(); snap != nil {
		t.Fatalf("after ClearLiveOverrides, snapshot = %v, want nil", snap)
	}
}

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
