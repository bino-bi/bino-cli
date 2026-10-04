package httpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if cc := resp.Header.Get("Cache-Control"); cc != "private, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want private, max-age=31536000, immutable", cc)
	}
}

// A data body is report data, on `bino serve` one viewer's query result. No
// mode may invite a shared cache to keep it, and serve must not have it
// stored at all, not even the 404 of a body that is gone.
func TestDataRouteCacheControl(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		noStore bool
		hash    string
		status  int
		want    string
	}{
		{name: "default hit", hash: "abc123", status: http.StatusOK, want: "private, max-age=31536000, immutable"},
		{name: "no-store hit", noStore: true, hash: "abc123", status: http.StatusOK, want: "private, no-store"},
		{name: "no-store miss", noStore: true, hash: "gone", status: http.StatusNotFound, want: "private, no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, err := New(Config{NoStore: tc.noStore})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}
			srv.PutDataset("tenant_sales", "abc123", []byte(`[{"tenant":"acme"}]`))

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/tenant_sales?hash="+tc.hash, nil)
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			cc := resp.Header.Get("Cache-Control")
			if cc != tc.want {
				t.Errorf("Cache-Control = %q, want %q", cc, tc.want)
			}
			if strings.Contains(cc, "public") {
				t.Errorf("Cache-Control = %q; must not contain \"public\"", cc)
			}
		})
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

// With a DataFunc installed the route serves what the func returns and does
// not fall back to the store. `bino serve` relies on this: the func alone
// decides how long a body stays available.
func TestDataRouteDataFunc(t *testing.T) {
	t.Parallel()
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	srv.PutDataset("sales", "stored", []byte(`["store"]`))

	get := func(hash string) (int, string) {
		t.Helper()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__bino/data/dataset/sales?hash="+hash, nil)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		resp := w.Result()
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	srv.SetDataFunc(func(kind, name, hash string) ([]byte, bool) {
		if kind == DataKindDataset && name == "sales" && hash == "live" {
			return []byte(`["func"]`), true
		}
		return nil, false
	})
	if status, body := get("live"); status != http.StatusOK || body != `["func"]` {
		t.Errorf("func body: status = %d, body = %q; want 200 and the func's body", status, body)
	}
	if status, _ := get("stored"); status != http.StatusNotFound {
		t.Errorf("store body with a func installed: status = %d, want 404", status)
	}

	srv.SetDataFunc(nil)
	if status, body := get("stored"); status != http.StatusOK || body != `["store"]` {
		t.Errorf("store body after the func was removed: status = %d, body = %q; want 200", status, body)
	}
}
