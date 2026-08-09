package refresh

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/httpserver"
	"bino.bi/bino/internal/logx"
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
// (both were missing before — /doc/* never live-reloaded and the context
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

// TestWithDocumentPageWidth covers the format/orientation width mapping and
// the bn-context style-attribute injection.
func TestWithDocumentPageWidth(t *testing.T) {
	t.Parallel()

	const page = `<html><head></head><body><bn-context locale='de'>x</bn-context></body></html>`
	tests := []struct {
		name        string
		doc         string
		format      string
		orientation string
		want        string
	}{
		{"a4 portrait", page, "a4", "portrait", `<bn-context style="--bn-doc-page-width:210mm" locale='de'>`},
		{"a4 landscape", page, "a4", "landscape", `<bn-context style="--bn-doc-page-width:297mm" locale='de'>`},
		{"letter portrait", page, "letter", "portrait", `<bn-context style="--bn-doc-page-width:215.9mm" locale='de'>`},
		{"unknown format falls back to a4", page, "tabloid", "", `<bn-context style="--bn-doc-page-width:210mm" locale='de'>`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := string(withDocumentPageWidth([]byte(tt.doc), tt.format, tt.orientation))
			if !strings.Contains(got, tt.want) {
				t.Errorf("withDocumentPageWidth(%q, %q) = %q, want substring %q", tt.format, tt.orientation, got, tt.want)
			}
		})
	}

	t.Run("no bn-context returns input unchanged", func(t *testing.T) {
		t.Parallel()
		in := `<html><head></head><body>plain</body></html>`
		if got := string(withDocumentPageWidth([]byte(in), "a4", "portrait")); got != in {
			t.Errorf("expected unchanged output, got %q", got)
		}
	})
}
