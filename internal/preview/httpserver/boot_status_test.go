package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleBootStatus(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Before any broadcast: 204 with no body so the loading page knows there
	// is nothing to render yet.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__preview/boot-status", nil)
	w := httptest.NewRecorder()
	srv.handleBootStatus(w, req)
	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 before any broadcast, got %d", resp.StatusCode)
	}

	// After a broadcast the snapshot should be served verbatim.
	srv.BroadcastBootStatus([]byte(`{"phase":"duckdb","message":"Loading"}`))
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__preview/boot-status", nil)
	w = httptest.NewRecorder()
	srv.handleBootStatus(w, req)
	resp = w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after broadcast, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want json", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body not valid JSON: %v (%s)", err, body)
	}
	if got["phase"] != "duckdb" || got["message"] != "Loading" {
		t.Errorf("snapshot mismatch: %v", got)
	}

	// Method not allowed for non-GET.
	req = httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/__preview/boot-status", nil)
	w = httptest.NewRecorder()
	srv.handleBootStatus(w, req)
	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for POST, got %d", w.Result().StatusCode)
	}
}

func TestBroadcastBootStatusFanOutToSSE(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ch := srv.sse.Subscribe()
	defer srv.sse.Unsubscribe(ch)

	srv.BroadcastBootStatus([]byte(`{"phase":"ready"}`))

	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), "event: boot-status") {
			t.Errorf("expected boot-status event, got %q", msg)
		}
		if !strings.Contains(string(msg), `"phase":"ready"`) {
			t.Errorf("expected payload to contain phase ready, got %q", msg)
		}
	default:
		t.Fatal("expected SSE message, got none")
	}
}
