package refresh

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/pipeline"
	"bino.bi/bino/internal/report/render"
)

const documentArtefactManifest = `apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata:
  name: handbook
spec:
  format: a4
  title: Handbook
  sources:
    - notes.md
`

// startTestServer creates and starts a preview server on an ephemeral port,
// stopped via test cleanup.
func startTestServer(t *testing.T) *httpserver.Server {
	t.Helper()
	srv, err := httpserver.New(httpserver.Config{})
	if err != nil {
		t.Fatalf("httpserver.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Start(ctx) }()
	return srv
}

// writeDocBundle writes a minimal docs-only bundle (one DocumentArtefact plus
// its markdown source) and returns the symlink-resolved bundle dir.
func writeDocBundle(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "document.yaml"), []byte(documentArtefactManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Chapter One\n\nHello.\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	return dir
}

// fetchContext fetches the SSE context cache entry for a preview path and
// returns status code and body.
func fetchContext(t *testing.T, srv *httpserver.Server, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL()+"/__preview/context?path="+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET context: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(body)
}

// TestRunBroadcastsDocumentArtefactContent proves the doc loop broadcasts
// content for /doc/* routes: the refresh returns the doc path in its
// broadcast list and the SSE context cache serves the rendered document
// (both were missing before - /doc/* never live-reloaded and the context
// endpoint 404'd forever).
func TestRunBroadcastsDocumentArtefactContent(t *testing.T) {
	t.Parallel()

	dir := writeDocBundle(t)
	srv := startTestServer(t)
	cfg := &Config{Logger: logx.Nop(), Workdir: dir}

	paths, err := Run(context.Background(), "test", nil, srv, nil, cfg, NewState())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !slices.Contains(paths, "/") {
		t.Errorf("broadcast paths missing /: %v", paths)
	}
	if !slices.Contains(paths, "/doc/handbook") {
		t.Errorf("broadcast paths missing /doc/handbook: %v", paths)
	}

	status, body := fetchContext(t, srv, "/doc/handbook")
	if status != http.StatusOK {
		t.Fatalf("context fetch status = %d, want 200", status)
	}
	if !strings.Contains(body, "bn-document-content") {
		t.Errorf("context body missing bn-document-content")
	}
	if !strings.Contains(body, "Chapter One") {
		t.Errorf("context body missing rendered markdown content")
	}

	t.Run("page width is an attribute on bn-context", func(t *testing.T) {
		if !strings.Contains(body, `<bn-context style="--bn-doc-page-width:210mm"`) {
			t.Errorf("bn-context missing morph-synced page-width style attribute")
		}
		if strings.Contains(body, ":root{--bn-doc-page-width") {
			t.Errorf("page width still injected as head style tag")
		}
	})
}

// TestRunSelectiveMarkdownEdit proves a markdown edit triggers a selective
// refresh scoped to its document: only /doc/<name> is re-rendered and
// broadcast. Before the graph stored resolved file paths, every markdown
// edit demoted to a full rebuild (All Pages plus every artefact).
func TestRunSelectiveMarkdownEdit(t *testing.T) {
	t.Parallel()

	dir := writeDocBundle(t)
	mdPath := filepath.Join(dir, "notes.md")
	srv := startTestServer(t)
	cfg := &Config{Logger: logx.Nop(), Workdir: dir}
	state := NewState()

	if _, err := Run(context.Background(), "initial", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("initial Run: %v", err)
	}

	if err := os.WriteFile(mdPath, []byte("# Chapter One\n\nHello.\n\n## Section Two\n"), 0o600); err != nil {
		t.Fatalf("edit markdown: %v", err)
	}

	paths, err := Run(context.Background(), "md edit", []string{mdPath}, srv, nil, cfg, state)
	if err != nil {
		t.Fatalf("selective Run: %v", err)
	}
	if !slices.Equal(paths, []string{"/doc/handbook"}) {
		t.Fatalf("broadcast paths = %v, want exactly [/doc/handbook] (selective doc-only refresh)", paths)
	}

	status, body := fetchContext(t, srv, "/doc/handbook")
	if status != http.StatusOK {
		t.Fatalf("context fetch status = %d, want 200", status)
	}
	if !strings.Contains(body, "Section Two") {
		t.Errorf("context body missing the edited section")
	}
}

// TestWithDocumentPreviewMeta covers the format/orientation width mapping,
// the header/footer marker with validated margins, and the bn-context
// style-attribute injection (attributes morph-sync, head styles would not).
func TestWithDocumentPreviewMeta(t *testing.T) {
	t.Parallel()

	const page = `<html><head></head><body><bn-context locale='de'>x</bn-context></body></html>`
	tests := []struct {
		name        string
		doc         string
		spec        config.DocumentArtefactSpec
		want        []string
		notContains []string
	}{
		{
			name: "a4 portrait",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "a4", Orientation: "portrait"},
			want: []string{`<bn-context style="--bn-doc-page-width:210mm" locale='de'>`},
			notContains: []string{
				"data-bino-doc-hf",
				"--bn-doc-margin-top",
			},
		},
		{
			name: "a4 landscape",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "a4", Orientation: "landscape"},
			want: []string{`<bn-context style="--bn-doc-page-width:297mm" locale='de'>`},
		},
		{
			name: "letter portrait",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "letter"},
			want: []string{`--bn-doc-page-width:215.9mm`},
		},
		{
			name: "unknown format falls back to a4",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "tabloid"},
			want: []string{`--bn-doc-page-width:210mm`},
		},
		{
			name: "header footer marks the context and mirrors margins",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "a4", DisplayHeaderFooter: true, MarginTop: "30mm"},
			want: []string{
				`data-bino-doc-hf='true'`,
				`--bn-doc-margin-top:30mm`,
				`--bn-doc-margin-bottom:15mm`, // Chrome default when unset
			},
		},
		{
			name: "invalid margin falls back to the Chrome default",
			doc:  page,
			spec: config.DocumentArtefactSpec{Format: "a4", DisplayHeaderFooter: true, MarginTop: "30mm; }injection"},
			want: []string{`--bn-doc-margin-top:20mm`},
			notContains: []string{
				"injection",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := string(withDocumentPreviewMeta([]byte(tt.doc), tt.spec))
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("output = %q, want substring %q", got, want)
				}
			}
			for _, ban := range tt.notContains {
				if strings.Contains(got, ban) {
					t.Errorf("output must not contain %q:\n%s", ban, got)
				}
			}
		})
	}

	t.Run("no bn-context returns input unchanged", func(t *testing.T) {
		t.Parallel()
		in := `<html><head></head><body>plain</body></html>`
		if got := string(withDocumentPreviewMeta([]byte(in), config.DocumentArtefactSpec{Format: "a4"})); got != in {
			t.Errorf("expected unchanged output, got %q", got)
		}
	})
}

type gaugeKindProvider struct{}

func (gaugeKindProvider) PluginKindNames() []string { return []string{"Gauge"} }

func (gaugeKindProvider) GetKind(name string) (config.KindInfo, bool) {
	if name != "Gauge" {
		return config.KindInfo{}, false
	}
	return config.KindInfo{KindName: "Gauge", Category: config.KindCategoryComponent, PluginName: "demo"}, true
}

type modeRecordingRenderer struct {
	mu    sync.Mutex
	modes []string
}

func (r *modeRecordingRenderer) RenderComponent(_ context.Context, req render.PluginRenderRequest) (string, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modes = append(r.modes, req.RenderMode)
	return "<x-gauge></x-gauge>", nil, nil
}

// TestRunDocumentPluginRefRendersAsPreview proves the live preview and the
// embed endpoint render a plugin :ref in a DocumentArtefact with render mode
// "preview", although the document options default to "build".
func TestRunDocumentPluginRefRendersAsPreview(t *testing.T) {
	t.Parallel()

	dir := writeDocBundle(t)
	gauge := "apiVersion: bino.bi/v1alpha1\nkind: Gauge\nmetadata:\n  name: g1\nspec: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "gauge.yaml"), []byte(gauge), 0o600); err != nil {
		t.Fatalf("write gauge manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Chapter One\n\n:ref[Gauge:g1]\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	srv := startTestServer(t)
	fake := &modeRecordingRenderer{}
	cfg := &Config{
		Logger:        logx.Nop(),
		Workdir:       dir,
		KindProvider:  gaugeKindProvider{},
		PluginOptions: &render.PluginOptions{ComponentRenderer: fake},
	}

	checkModes := func(t *testing.T) {
		t.Helper()
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if len(fake.modes) == 0 {
			t.Fatal("plugin renderer never called")
		}
		for _, m := range fake.modes {
			if m != "preview" {
				t.Errorf("render modes = %q, want all preview", fake.modes)
				break
			}
		}
		fake.modes = nil
	}

	state := NewState()
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, body := fetchContext(t, srv, "/doc/handbook"); !strings.Contains(body, "<x-gauge></x-gauge>") {
		t.Errorf("document body missing plugin component html")
	}
	checkModes(t)

	body, err := EmbedByName(context.Background(), "handbook", "DocumentArtefact", "", &sync.Mutex{}, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if !strings.Contains(string(body), "<x-gauge></x-gauge>") {
		t.Errorf("embedded document missing plugin component html")
	}
	checkModes(t)
}

// TestEmbedByNameKeepsPluginOptionsOfItsStart proves an embed render, which
// runs without the refresh mutex, does not see a plugin reload that rewrites
// the shared PluginOptions while it renders.
func TestEmbedByNameKeepsPluginOptionsOfItsStart(t *testing.T) {
	t.Parallel()

	dir := writeDocBundle(t)
	gauge := "apiVersion: bino.bi/v1alpha1\nkind: Gauge\nmetadata:\n  name: g1\nspec: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "gauge.yaml"), []byte(gauge), 0o600); err != nil {
		t.Fatalf("write gauge manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Chapter One\n\n:ref[Gauge:g1]\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	srv := startTestServer(t)
	before := &modeRecordingRenderer{}
	after := &modeRecordingRenderer{}
	cfg := &Config{
		Logger:        logx.Nop(),
		Workdir:       dir,
		KindProvider:  gaugeKindProvider{},
		PluginOptions: &render.PluginOptions{ComponentRenderer: before},
	}
	state := NewState()
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	before.modes = nil

	// The hook runs inside the render, before the plugin components.
	cfg.PostDatasetHook = func(context.Context, []pipeline.DatasetPayload) error {
		cfg.PluginOptions.ComponentRenderer = after
		return nil
	}
	if _, err := EmbedByName(context.Background(), "handbook", "DocumentArtefact", "", &sync.Mutex{}, state, cfg, srv); err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if len(before.modes) == 0 || len(after.modes) != 0 {
		t.Errorf("renderer calls: before reload %d, after reload %d; want the renderer of the embed start only", len(before.modes), len(after.modes))
	}
}

// writeEmbedBundle writes a report whose single page shows a local image, so a
// render has an asset to register, and returns the bundle dir.
func writeEmbedBundle(t *testing.T, title string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG"), 0o600); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	writeEmbedManifest(t, dir, title)
	return dir
}

func writeEmbedManifest(t *testing.T, dir, title string) {
	t.Helper()
	manifest := `apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./logo.png
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: ` + title + `
`
	if err := os.WriteFile(filepath.Join(dir, "report.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // test
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

func TestRunEmbedOnlyRendersNothing(t *testing.T) {
	t.Parallel()

	dir := writeEmbedBundle(t, "first")
	srv := startTestServer(t)
	state := NewState()
	cfg := &Config{Logger: logx.Nop(), Workdir: dir, EmbedOnly: true}

	paths, err := Run(context.Background(), "test", nil, srv, nil, cfg, state)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("broadcast paths = %v, want none", paths)
	}
	if len(state.allPagesFrameHTML) != 0 {
		t.Error("the All Pages view was rendered")
	}
	if status, body := get(t, srv.URL()+"/"); status != http.StatusOK || !strings.Contains(body, "--embed-only") {
		t.Errorf("root = %d %q, want the notice", status, body)
	}
}

func TestEmbedOnlyEmbeddingFollowsTheFiles(t *testing.T) {
	t.Parallel()

	dir := writeEmbedBundle(t, "first")
	srv := startTestServer(t)
	state := NewState()
	mu := &sync.Mutex{}
	cfg := &Config{Logger: logx.Nop(), Workdir: dir, EmbedOnly: true}
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	body, err := EmbedByName(context.Background(), "note", "Text", "", mu, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if !strings.Contains(string(body), "first") {
		t.Errorf("first embedding does not show the first text: %s", body)
	}

	writeEmbedManifest(t, dir, "second")
	if _, err := Run(context.Background(), "test", []string{filepath.Join(dir, "report.yaml")}, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	body, err = EmbedByName(context.Background(), "note", "Text", "", mu, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if !strings.Contains(string(body), "second") || strings.Contains(string(body), "first") {
		t.Errorf("embedding after the edit is stale: %s", body)
	}
}

// Under --embed-only no refresh registers assets, so the embedding has to.
func TestEmbedOnlyServesTheAssetsOfAnEmbedding(t *testing.T) {
	t.Parallel()

	dir := writeEmbedBundle(t, "first")
	srv := startTestServer(t)
	state := NewState()
	cfg := &Config{Logger: logx.Nop(), Workdir: dir, EmbedOnly: true}
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	body, err := EmbedByName(context.Background(), "note", "Text", "", &sync.Mutex{}, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	match := regexp.MustCompile(`/assets/[^"'\s)<]+`).FindString(string(body))
	if match == "" {
		t.Fatalf("no asset reference in %s", body)
	}
	if status, _ := get(t, srv.URL()+match); status != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", match, status)
	}
}

// The refresh under --embed-only is quick enough to finish while a render runs.
// That render's body describes the old files and must not land in the new cache.
func TestEmbedRenderOutlivedByARefreshDoesNotFillTheCache(t *testing.T) {
	t.Parallel()

	dir := writeEmbedBundle(t, "first")
	srv := startTestServer(t)
	state := NewState()
	mu := &sync.Mutex{}
	cfg := &Config{Logger: logx.Nop(), Workdir: dir, EmbedOnly: true}
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The hook runs inside the render, after the docs it uses were picked.
	var once sync.Once
	cfg.PostDatasetHook = func(ctx context.Context, _ []pipeline.DatasetPayload) error {
		once.Do(func() {
			writeEmbedManifest(t, dir, "second")
			if _, err := Run(ctx, "test", nil, srv, nil, cfg, state); err != nil {
				t.Errorf("Run: %v", err)
			}
		})
		return nil
	}
	if _, err := EmbedByName(context.Background(), "note", "Text", "", mu, state, cfg, srv); err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}

	body, err := EmbedByName(context.Background(), "note", "Text", "", mu, state, cfg, srv)
	if err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if !strings.Contains(string(body), "second") {
		t.Errorf("the render from before the refresh is still served: %s", body)
	}
}

func TestEmbedOnlyLogsDataFailures(t *testing.T) {
	t.Parallel()

	dir := writeEmbedBundle(t, "first")
	broken := `
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: broken
spec:
  query: SELECT no_such_col FROM no_such_table
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: tbl
spec:
  dataset: broken
  type: sum
`
	f, err := os.OpenFile(filepath.Join(dir, "report.yaml"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(broken); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	var logs bytes.Buffer
	srv := startTestServer(t)
	state := NewState()
	cfg := &Config{Logger: logx.NewJSON(&logs, &logs, true), Workdir: dir, EmbedOnly: true}
	if _, err := Run(context.Background(), "test", nil, srv, nil, cfg, state); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := EmbedByName(context.Background(), "tbl", "Table", "", &sync.Mutex{}, state, cfg, srv); err != nil {
		t.Fatalf("EmbedByName: %v", err)
	}
	if !strings.Contains(logs.String(), "no_such_table") {
		t.Errorf("the query failure is not in the log: %q", logs.String())
	}
}
