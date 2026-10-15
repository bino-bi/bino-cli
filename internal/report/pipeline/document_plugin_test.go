package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/render"
)

type gaugeKinds struct{}

func (gaugeKinds) PluginKindNames() []string { return []string{"Gauge"} }

func (gaugeKinds) GetKind(name string) (config.KindInfo, bool) {
	if name != "Gauge" {
		return config.KindInfo{}, false
	}
	return config.KindInfo{KindName: "Gauge", Category: config.KindCategoryComponent}, true
}

type recordingPluginRenderer struct {
	mu    sync.Mutex
	modes []string
}

func (r *recordingPluginRenderer) RenderComponent(_ context.Context, req render.PluginRenderRequest) (string, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modes = append(r.modes, req.RenderMode)
	return "<x-gauge></x-gauge>", nil, nil
}

// renderPluginRefDoc renders the doc-bundle "refdoc" with a :ref to a plugin
// Gauge document through the Builder, as bino build does. The Gauge binds
// unused_ds, which nothing else in the document references.
func renderPluginRefDoc(t *testing.T, pluginOpts *render.PluginOptions) string {
	t.Helper()
	ctx := context.Background()
	workdir := copyBundle(t, "doc-bundle")
	mdPath := filepath.Join(workdir, "docs", "refdoc.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read markdown: %v", err)
	}
	if err := os.WriteFile(mdPath, append(md, []byte("\n:ref[Gauge:g1]\n")...), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	docs := append(loadResult.Documents, config.Document{
		Kind: "Gauge",
		Name: "g1",
		Raw:  json.RawMessage(`{"apiVersion":"bino.bi/v1alpha1","kind":"Gauge","metadata":{"name":"g1"},"spec":{"dataset":"unused_ds"}}`),
	})
	artefacts, err := config.CollectDocumentArtefacts(docs)
	if err != nil {
		t.Fatalf("collect document artefacts: %v", err)
	}
	var artifact config.DocumentArtefact
	for _, a := range artefacts {
		if a.Document.Name == "refdoc" {
			artifact = a
		}
	}

	b := &Builder{Workdir: workdir, EngineVersion: "v1.0.0", PluginOptions: pluginOpts, KindProvider: gaugeKinds{}}
	result, err := b.RenderDocumentHTML(ctx, docs, artifact, DocumentArtefactRenderOptions{})
	if err != nil {
		t.Fatalf("render document: %v", err)
	}
	return string(result.HTML)
}

func TestDocumentArtefactPluginRef(t *testing.T) {
	t.Parallel()

	const head = "<script type='module' src='/plugins/demo/gauge.js'></script>"

	t.Run("plugin options add head markup and render as build", func(t *testing.T) {
		t.Parallel()
		fake := &recordingPluginRenderer{}
		got := renderPluginRefDoc(t, &render.PluginOptions{ExtraHeadMarkup: head, ComponentRenderer: fake})
		headEnd := strings.Index(got, "</head>")
		if i := strings.Index(got, head); i < 0 || i > headEnd {
			t.Errorf("<head> missing plugin markup %q", head)
		}
		if !strings.Contains(got, "<x-gauge></x-gauge>") {
			t.Errorf("body missing plugin component html")
		}
		if len(fake.modes) != 1 || fake.modes[0] != "build" {
			t.Errorf("render modes = %q, want [build]", fake.modes)
		}
		if !strings.Contains(got, "<bn-dataset name='unused_ds'") {
			t.Errorf("document must embed the dataset the plugin component binds")
		}
	})

	t.Run("nil plugin options add nothing", func(t *testing.T) {
		t.Parallel()
		got := renderPluginRefDoc(t, nil)
		if strings.Contains(got, "/plugins/") {
			t.Errorf("output must not reference plugin assets")
		}
		if !strings.Contains(got, "<bn-ref kind='Gauge' name='g1'></bn-ref>") {
			t.Errorf("plugin ref without a renderer must fall back to the placeholder")
		}
	})
}
