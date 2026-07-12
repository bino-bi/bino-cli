package registry

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(Config{URL: srv.URL})
}

func TestClientResolve(t *testing.T) {
	var gotPath, gotRef, gotAuth string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotRef, gotAuth = r.URL.Path, r.URL.Query().Get("ref"), r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"package":"@acme/tbl","tag":"latest","version":"2.1.0","kind":"Table","digest":"sha256:abcd","dependencies":["@bino/style_a"],"downloadUrl":"/api/registry/download/acme/tbl/2.1.0"}`))
	}))
	res, err := c.Resolve(context.Background(), "acme", "tbl", "latest")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gotPath != "/api/registry/resolve/acme/tbl" || gotRef != "latest" {
		t.Errorf("request = %s?ref=%s", gotPath, gotRef)
	}
	if gotAuth != "" {
		t.Errorf("anonymous client sent Authorization %q", gotAuth)
	}
	if res.Version != "2.1.0" || res.Tag != "latest" || res.Digest != "sha256:abcd" || len(res.Dependencies) != 1 {
		t.Errorf("unexpected result: %+v", res)
	}
}

func TestClientBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, Token: "tok123"})
	if _, err := c.Resolve(context.Background(), "a", "b", ""); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestClientAPIError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":"package_not_found","message":"no such package"}`))
	}))
	_, err := c.Resolve(context.Background(), "a", "b", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %v", err)
	}
	if apiErr.Code != "package_not_found" || apiErr.Status != http.StatusNotFound {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClientYanked(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
		w.Write([]byte(`{"code":"version_yanked","message":"yanked"}`))
	}))
	_, _, err := c.Download(context.Background(), "a", "b", "1.0.0")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "version_yanked" {
		t.Fatalf("expected version_yanked, got %v", err)
	}
}

func TestClientRateLimitRetry(t *testing.T) {
	calls := 0
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":"rate_limited","message":"slow down"}`))
			return
		}
		w.Write([]byte(`{"version":"1.0.0"}`))
	}))
	res, err := c.Resolve(context.Background(), "a", "b", "")
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if calls != 2 || res.Version != "1.0.0" {
		t.Errorf("calls = %d, res = %+v", calls, res)
	}
}

func TestClientRateLimitExhausted(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"code":"rate_limited","message":"slow down"}`))
	}))
	_, err := c.Resolve(context.Background(), "a", "b", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "rate_limited" {
		t.Fatalf("expected rate_limited after one retry, got %v", err)
	}
}

func TestClientDownloadETag(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/registry/download/acme/tbl/2.1.0" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("ETag", `"sha256:abcd"`)
		w.Write([]byte(`{"kind":"Table"}`))
	}))
	body, digest, err := c.Download(context.Background(), "acme", "tbl", "2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:abcd" {
		t.Errorf("digest = %q", digest)
	}
	if string(body) != `{"kind":"Table"}` {
		t.Errorf("body = %q", body)
	}
}

func TestClientSearch(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"page":1,"perPage":20,"totalItems":1,"totalPages":1,"items":[{"package":"@acme/tbl","kind":"Table","description":"d","latestVersion":"2.1.0","pullsTotal":7}]}`))
	}))
	res, err := c.Search(context.Background(), SearchParams{Query: "rev", Kinds: []string{"Table"}, Scopes: []string{"@acme"}, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "kind=Table&page=1&q=rev&scope=acme" {
		t.Errorf("query = %q", gotQuery)
	}
	if res.TotalItems != 1 || res.Items[0].Package != "@acme/tbl" {
		t.Errorf("result = %+v", res)
	}
}

func TestClientBearerExchangesPATOnce(t *testing.T) {
	var exchanges, authed int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/registry/auth/pat/exchange" {
			atomic.AddInt64(&exchanges, 1)
			w.Write([]byte(`{"token":"jwt-x","expires":"2026-01-01T00:00:00Z","id":"p1"}`))
			return
		}
		if r.Header.Get("Authorization") == "Bearer jwt-x" {
			atomic.AddInt64(&authed, 1)
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, Token: "bino_pat_abc123"})
	for range 2 {
		if _, err := c.Resolve(context.Background(), "a", "b", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt64(&exchanges); got != 1 {
		t.Errorf("exchange calls = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&authed); got != 2 {
		t.Errorf("requests with exchanged JWT = %d, want 2", got)
	}
}

func TestClientBearerJWTPassesThrough(t *testing.T) {
	var exchanges int64
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/registry/auth/pat/exchange" {
			atomic.AddInt64(&exchanges, 1)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, Token: "raw-jwt"})
	if _, err := c.Resolve(context.Background(), "a", "b", ""); err != nil {
		t.Fatal(err)
	}
	if exchanges != 0 || gotAuth != "Bearer raw-jwt" {
		t.Errorf("exchanges = %d, auth = %q", exchanges, gotAuth)
	}
}
