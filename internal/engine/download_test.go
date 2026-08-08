package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// zipBytes returns an in-memory zip archive with the given name → content entries.
func zipBytes(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// engineArchive returns a valid engine release zip containing the entry point.
func engineArchive(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, map[string]string{
		"bn-template-engine/" + EntryPoint: "// engine",
	})
}

// countingRewriteTransport counts every request issued through the injected
// client and redirects it to a test server. Accesses are sequential (the
// client is only used from the test goroutine).
type countingRewriteTransport struct {
	calls     int
	targetURL string
}

func (t *countingRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.targetURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

// newTestManager wires a Manager to the given test server through an injected
// client, returning the transport so tests can assert the client was used.
func newTestManager(t *testing.T, srvURL string) (*Manager, *countingRewriteTransport) {
	t.Helper()
	rt := &countingRewriteTransport{targetURL: srvURL}
	return NewManagerWithClient(t.TempDir(), &http.Client{Transport: rt}), rt
}

func TestDownload_UsesInjectedClient(t *testing.T) {
	const version = "v0.0.1-test.1"
	archive := engineArchive(t)

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	mgr, rt := newTestManager(t, srv.URL)
	info, err := mgr.Download(context.Background(), version)
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if rt.calls == 0 {
		t.Fatal("Download() bypassed the injected HTTP client")
	}

	wantPath := strings.TrimPrefix(GitHubReleasesURL, "https://github.com") +
		"/" + version + "/bn-template-engine-" + version + ".zip"
	if gotPath != wantPath {
		t.Errorf("request path = %q, want %q", gotPath, wantPath)
	}

	if info.Version != version {
		t.Errorf("Download().Version = %q, want %q", info.Version, version)
	}
	data, err := os.ReadFile(info.EntryPath)
	if err != nil {
		t.Fatalf("read entry point: %v", err)
	}
	if string(data) != "// engine" {
		t.Errorf("entry point content = %q, want %q", data, "// engine")
	}
}

func TestDownload_FollowsRedirects(t *testing.T) {
	archive := engineArchive(t)

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected/engine.zip" {
			_, _ = w.Write(archive)
			return
		}
		http.Redirect(w, r, srvURL+"/redirected/engine.zip", http.StatusFound)
	}))
	srvURL = srv.URL
	defer srv.Close()

	mgr, rt := newTestManager(t, srv.URL)
	info, err := mgr.Download(context.Background(), "v0.0.1-test.1")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if rt.calls < 2 {
		t.Errorf("injected client calls = %d, want at least 2 (redirect + target)", rt.calls)
	}
	if _, err := os.Stat(info.EntryPath); err != nil {
		t.Errorf("entry point missing after redirected download: %v", err)
	}
}

func TestDownload_HTTPErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr string
	}{
		{name: "404 maps to version not found", status: http.StatusNotFound, wantErr: "not found on GitHub"},
		{name: "non-200 reports status", status: http.StatusInternalServerError, wantErr: "HTTP 500"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()

			mgr, rt := newTestManager(t, srv.URL)
			_, err := mgr.Download(context.Background(), "v0.0.1-test.1")
			if err == nil {
				t.Fatal("Download() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if rt.calls == 0 {
				t.Fatal("Download() bypassed the injected HTTP client")
			}
		})
	}
}

func TestDownload_TruncatedBody(t *testing.T) {
	archive := engineArchive(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Announce more bytes than are sent so the client sees an unexpected EOF.
		w.Header().Set("Content-Length", "1048576")
		_, _ = w.Write(archive[:16])
	}))
	defer srv.Close()

	mgr, rt := newTestManager(t, srv.URL)
	_, err := mgr.Download(context.Background(), "v0.0.1-test.1")
	if err == nil {
		t.Fatal("Download() expected error for truncated body, got nil")
	}
	if !strings.Contains(err.Error(), "write download") {
		t.Errorf("error = %v, want it to contain %q", err, "write download")
	}
	if rt.calls == 0 {
		t.Fatal("Download() bypassed the injected HTTP client")
	}
}

func TestDownload_MissingEntryPoint(t *testing.T) {
	archive := zipBytes(t, map[string]string{
		"bn-template-engine/other.js": "// not the entry point",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	mgr, rt := newTestManager(t, srv.URL)
	_, err := mgr.Download(context.Background(), "v0.0.1-test.1")
	if err == nil {
		t.Fatal("Download() expected error for archive without entry point, got nil")
	}
	if !strings.Contains(err.Error(), "missing entry point") {
		t.Errorf("error = %v, want it to contain %q", err, "missing entry point")
	}
	if rt.calls == 0 {
		t.Fatal("Download() bypassed the injected HTTP client")
	}
	if _, err := os.Stat(mgr.CacheDir() + "/v0.0.1-test.1"); !os.IsNotExist(err) {
		t.Error("partial extraction was not cleaned up")
	}
}

func TestFetchLatestRemoteVersion_DoesNotFollowRedirect(t *testing.T) {
	followed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tag/") {
			// Only reachable if the client followed the redirect.
			followed = true
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", "https://github.com/bino-bi/bn-template-engine-releases/releases/tag/v1.2.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	mgr, rt := newTestManager(t, srv.URL)
	version, err := mgr.FetchLatestRemoteVersion(context.Background())
	if err != nil {
		t.Fatalf("FetchLatestRemoteVersion() error = %v", err)
	}
	if followed {
		t.Error("FetchLatestRemoteVersion() followed the redirect instead of reading the Location header")
	}
	if version != "v1.2.3" {
		t.Errorf("FetchLatestRemoteVersion() = %q, want %q", version, "v1.2.3")
	}
	if rt.calls != 1 {
		t.Errorf("injected client calls = %d, want 1", rt.calls)
	}
}

func TestNewManagerWithClient_NilClientFallsBack(t *testing.T) {
	mgr := NewManagerWithClient(t.TempDir(), nil)
	if mgr.httpClient == nil {
		t.Fatal("NewManagerWithClient(dir, nil) left httpClient nil; Download and FetchLatestRemoteVersion would panic")
	}
}
