package lsp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
)

func healthServer(t *testing.T, capabilities []string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":       "ok",
			"version":      "0.0.0-test",
			"capabilities": capabilities,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestNewHTTPBackend_CapabilityHandshake: a daemon that health-checks fine but
// predates /validate-draft must be rejected so the caller falls back to a
// standalone backend - previously the 404s made diagnostics silently vanish.
func TestNewHTTPBackend_CapabilityHandshake(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")

	old := healthServer(t, []string{"introspect-draft"})
	if _, err := NewHTTPBackend(context.Background(), old.URL, "", log); err == nil {
		t.Fatal("a daemon without validate-draft must be rejected")
	} else if !strings.Contains(err.Error(), "validate-draft") {
		t.Fatalf("rejection should name the missing capability, got %v", err)
	}

	current := healthServer(t, []string{"introspect-draft", "validate", "validate-draft"})
	if _, err := NewHTTPBackend(context.Background(), current.URL, "", log); err != nil {
		t.Fatalf("a capable daemon must be accepted, got %v", err)
	}
}

// The proxy must send the port file's token on every request, SSE included.
func TestHTTPBackend_SendsToken(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	seen := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Path + " " + r.Header.Get("Authorization")
		if r.URL.Path == "/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "capabilities": []string{"validate-draft"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	t.Cleanup(srv.Close)

	b, err := NewHTTPBackend(context.Background(), srv.URL, "s3cret", log)
	if err != nil {
		t.Fatalf("NewHTTPBackend: %v", err)
	}
	if _, err := b.Index(context.Background()); err != nil {
		t.Fatalf("Index: %v", err)
	}
	for _, want := range []string{"/health Bearer s3cret", "/index Bearer s3cret"} {
		if got := <-seen; got != want {
			t.Errorf("request = %q, want %q", got, want)
		}
	}
}

func TestNewHTTPBackend_RejectedToken(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	if _, err := NewHTTPBackend(context.Background(), srv.URL, "wrong", log); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("a 401 must fail with a token error, got %v", err)
	}
}
