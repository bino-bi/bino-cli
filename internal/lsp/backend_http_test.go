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
// standalone backend — previously the 404s made diagnostics silently vanish.
func TestNewHTTPBackend_CapabilityHandshake(t *testing.T) {
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")

	old := healthServer(t, []string{"introspect-draft"})
	if _, err := NewHTTPBackend(context.Background(), old.URL, log); err == nil {
		t.Fatal("a daemon without validate-draft must be rejected")
	} else if !strings.Contains(err.Error(), "validate-draft") {
		t.Fatalf("rejection should name the missing capability, got %v", err)
	}

	current := healthServer(t, []string{"introspect-draft", "validate", "validate-draft"})
	if _, err := NewHTTPBackend(context.Background(), current.URL, log); err != nil {
		t.Fatalf("a capable daemon must be accepted, got %v", err)
	}
}
