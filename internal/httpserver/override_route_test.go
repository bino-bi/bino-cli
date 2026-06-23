package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// postOverride drives the override route through the full mux.
func postOverride(t *testing.T, srv *Server, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/__bino/embedding/override", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w.Result()
}

func TestOverrideRouteSetsAndClears(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	var (
		mu      sync.Mutex
		gotFile string
		gotBody string
		gotClr  bool
		calls   int
	)
	srv.SetEmbeddingOverrideFunc(func(file, content string, remove bool) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		gotFile, gotBody, gotClr = file, content, remove
		return nil
	})

	// Set
	resp := postOverride(t, srv, `{"file":"/proj/a.yaml","content":"hello"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set status = %d, want 204", resp.StatusCode)
	}
	mu.Lock()
	if gotFile != "/proj/a.yaml" || gotBody != "hello" || gotClr {
		t.Errorf("set delegated (%q,%q,%v), want (/proj/a.yaml,hello,false)", gotFile, gotBody, gotClr)
	}
	mu.Unlock()

	// Clear
	resp2 := postOverride(t, srv, `{"file":"/proj/a.yaml","clear":true}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204", resp2.StatusCode)
	}
	mu.Lock()
	if !gotClr || calls != 2 {
		t.Errorf("clear delegated clear=%v calls=%d, want clear=true calls=2", gotClr, calls)
	}
	mu.Unlock()
}

func TestOverrideRouteNoFuncIs503(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	resp := postOverride(t, srv, `{"file":"/proj/a.yaml","content":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when no override func installed", resp.StatusCode)
	}
}

func TestOverrideRouteMissingFileIs400(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.SetEmbeddingOverrideFunc(func(string, string, bool) error {
		t.Error("override func must not be called for missing file")
		return nil
	})
	resp := postOverride(t, srv, `{"content":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing file", resp.StatusCode)
	}
}

func TestOverrideRouteRejectsHTTPError(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.SetEmbeddingOverrideFunc(func(string, string, bool) error {
		return NewHTTPError(http.StatusForbidden, "outside root")
	})
	resp := postOverride(t, srv, `{"file":"/etc/passwd","content":"x"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (HTTPError mapped)", resp.StatusCode)
	}
}

func TestOverrideRouteGetNotAllowed(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.SetEmbeddingOverrideFunc(func(string, string, bool) error { return nil })
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/embedding/override", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	// The route is registered as "POST ...", so a GET should not match it.
	if resp.StatusCode == http.StatusNoContent {
		t.Fatalf("GET unexpectedly handled by POST-only override route (status %d)", resp.StatusCode)
	}
}
