package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func servePath(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

func TestPluginAssetsRoute(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	srv.SetContentFunc(StaticContent([]byte("<html>page</html>"), "text/html; charset=utf-8"))

	if w := servePath(t, srv, "/plugins/x/a.js"); w.Code != http.StatusNotFound {
		t.Fatalf("without handler: status = %d, want 404", w.Code)
	}

	srv.SetPluginAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/plugins/x/a.js" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("console.log(1)"))
	}))

	w := servePath(t, srv, "/plugins/x/a.js")
	if w.Code != http.StatusOK {
		t.Fatalf("with handler: status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/javascript" {
		t.Errorf("Content-Type = %q, want application/javascript", ct)
	}
	if body := w.Body.String(); body != "console.log(1)" {
		t.Errorf("body = %q", body)
	}

	if w := servePath(t, srv, "/"); w.Code != http.StatusOK || w.Body.String() != "<html>page</html>" {
		t.Errorf("GET / = %d %q, want the page", w.Code, w.Body.String())
	}
}
