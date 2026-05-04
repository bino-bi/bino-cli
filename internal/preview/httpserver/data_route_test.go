package httpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDataRouteHit(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	body := []byte(`[{"x":1}]`)
	srv.PutDataset("sales", "abc123", body)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/sales?hash=abc123", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc == "" {
		t.Fatalf("Cache-Control missing; want immutable header")
	}
}

func TestDataRouteUnknownHashIs404(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.PutDataset("sales", "abc123", []byte(`[{}]`))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/sales?hash=stale", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestDataRouteUnknownNameIs404(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/datasource/missing?hash=h", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Result().StatusCode)
	}
}

func TestDataRouteRequiresHashParam(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.PutDataset("sales", "h", []byte("[]"))
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/sales", nil)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status without hash = %d, want 404", w.Result().StatusCode)
	}
}

func TestDataRouteKindIsolation(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	// Same name, same hash, different kind → must not cross over.
	srv.PutDataset("foo", "h", []byte("dataset"))
	srv.PutDatasource("foo", "h", []byte("datasource"))

	req1 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/foo?hash=h", nil)
	w1 := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w1, req1)
	got1, _ := io.ReadAll(w1.Result().Body)
	if string(got1) != "dataset" {
		t.Fatalf("dataset path returned %q", got1)
	}

	req2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/datasource/foo?hash=h", nil)
	w2 := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w2, req2)
	got2, _ := io.ReadAll(w2.Result().Body)
	if string(got2) != "datasource" {
		t.Fatalf("datasource path returned %q", got2)
	}
}
