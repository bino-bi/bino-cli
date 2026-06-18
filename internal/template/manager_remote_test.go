package template

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const testSHA = "0123456789abcdef0123456789abcdef01234567"

// buildTemplateZip returns a GitHub-style archive (single top dir) carrying a
// minimal ProjectTemplate with a template/ render root.
func buildTemplateZip(t *testing.T, topDir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		topDir + "/bino.template.yaml": "apiVersion: bino.bi/v1alpha1\nkind: ProjectTemplate\nmetadata:\n  name: demo\nspec:\n  fields:\n    - { name: Name, default: world }\n",
		topDir + "/template/hello.txt": "Hi {{ .Name }}\n",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newMockGitHub(t *testing.T, zipBytes []byte, hits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if r.Header.Get("Accept") != "application/vnd.github.sha" {
			t.Errorf("missing sha Accept header: %q", r.Header.Get("Accept"))
		}
		fmt.Fprint(w, testSHA)
	})
	mux.HandleFunc("/o/r/zip/", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Write(zipBytes)
	})
	return httptest.NewServer(mux)
}

func newTestManager(t *testing.T, srv *httptest.Server) *Manager {
	t.Helper()
	m := NewManagerWithClient(t.TempDir(), srv.Client())
	m.apiBase = srv.URL
	m.codeload = srv.URL
	return m
}

func TestResolveGitHubFetchesCachesAndServesOffline(t *testing.T) {
	zipBytes := buildTemplateZip(t, "r-"+testSHA)
	var hits int32
	srv := newMockGitHub(t, zipBytes, &hits)
	defer srv.Close()

	m := newTestManager(t, srv)
	src := Source{Kind: SourceShorthand, Owner: "o", Repo: "r"}

	// First fetch: resolves SHA (1) + downloads archive (1) == 2 requests.
	res, err := m.Resolve(context.Background(), src, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.SHA != testSHA {
		t.Errorf("SHA = %q, want %q", res.SHA, testSHA)
	}
	if res.Provenance != "o/r@"+testSHA {
		t.Errorf("Provenance = %q", res.Provenance)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("expected 2 requests, got %d", got)
	}
	out, err := Render("hello", mustRead(t, res.Root, "hello.txt"), map[string]any{"Name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "Hi x\n" {
		t.Errorf("rendered %q, want %q", out, "Hi x\n")
	}

	// Second resolve, offline, against the SAME cache dir but a client that must
	// never be used: served from cache.
	m2 := NewManagerWithClient(m.cacheDir, &http.Client{Transport: failTransport{t}})
	m2.apiBase, m2.codeload = "http://unused", "http://unused"
	res2, err := m2.Resolve(context.Background(), src, true)
	if err != nil {
		t.Fatalf("offline cache hit failed: %v", err)
	}
	if res2.SHA != testSHA {
		t.Errorf("offline SHA = %q, want %q", res2.SHA, testSHA)
	}
}

func TestResolveOfflineMissErrors(t *testing.T) {
	m := NewManagerWithClient(t.TempDir(), &http.Client{Transport: failTransport{t}})
	_, err := m.Resolve(context.Background(), Source{Kind: SourceShorthand, Owner: "o", Repo: "missing"}, true)
	if err == nil || !strings.Contains(err.Error(), "not cached") {
		t.Fatalf("expected offline cache-miss error, got %v", err)
	}
}

func TestResolveExplicitSHASkipsAPICall(t *testing.T) {
	zipBytes := buildTemplateZip(t, "r-"+testSHA)
	var hits int32
	srv := newMockGitHub(t, zipBytes, &hits)
	defer srv.Close()

	m := newTestManager(t, srv)
	src := Source{Kind: SourceShorthand, Owner: "o", Repo: "r", Ref: testSHA}
	if _, err := m.Resolve(context.Background(), src, false); err != nil {
		t.Fatal(err)
	}
	// An explicit SHA needs no API call — only the archive download.
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 request for explicit SHA, got %d", got)
	}
}

func mustRead(t *testing.T, fsys fs.FS, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type failTransport struct{ t *testing.T }

func (f failTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.t.Errorf("unexpected network call to %s in offline mode", r.URL)
	return nil, errors.New("network disabled")
}
