package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	embedkinds "bino.bi/bino/internal/report/embed"
)

// newTestServer creates a Server with a nil state for handler testing.
// Only endpoints that don't require state (health, shutdown) can be tested.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer(ServerConfig{
		ListenAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if body["clients"] == nil {
		t.Error("missing clients field")
	}
}

func TestShutdownEndpoint(t *testing.T) {
	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/shutdown", nil)
	w := httptest.NewRecorder()

	srv.handleShutdown(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify shutdown channel is closed
	select {
	case <-srv.ShutdownCh():
		// Expected
	case <-time.After(time.Second):
		t.Fatal("shutdown channel not closed")
	}
}

func TestBroadcastEvent(t *testing.T) {
	srv := newTestServer(t)

	// Subscribe to SSE
	ch := srv.sse.Subscribe()
	defer srv.sse.Unsubscribe(ch)

	// Broadcast an event
	srv.BroadcastEvent("test-event", map[string]string{"hello": "world"})

	// Verify we received it
	select {
	case msg := <-ch:
		if len(msg) == 0 {
			t.Fatal("empty message")
		}
		msgStr := string(msg)
		if msgStr == "" {
			t.Fatal("empty string message")
		}
	case <-time.After(time.Second):
		t.Fatal("no message received")
	}
}

func TestServerStartAndShutdown(t *testing.T) {
	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel to trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

// TestKindsEndpoint asserts GET /kinds serves the render-embeddable flag from the
// single authority (internal/report/embed) for every kind, so the daemon, the
// MCP bino://kinds resource, and the extension never diverge. The extension
// derives its render-embeddable set from this served flag instead of a
// hand-maintained list.
func TestKindsEndpoint(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/kinds", nil)
	w := httptest.NewRecorder()

	srv.handleKinds(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body struct {
		Kinds []kindInfo `json:"kinds"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Kinds) == 0 {
		t.Fatal("/kinds returned no kinds")
	}

	got := make(map[string]bool, len(body.Kinds))
	for _, k := range body.Kinds {
		if want := embedkinds.IsEmbeddable(k.Name); k.Embeddable != want {
			t.Errorf("kind %s: embeddable = %v, want %v (authority set)", k.Name, k.Embeddable, want)
		}
		if k.Name == "Image" {
			t.Errorf("Image is a layout-child kind, not a manifest kind; it must not appear in /kinds")
		}
		got[k.Name] = k.Embeddable
	}
	for _, name := range []string{"Table", "ChartStructure", "ChartTime", "Text", "Tree", "Grid"} {
		if !got[name] {
			t.Errorf("component kind %s: embeddable = false, want true", name)
		}
	}
	if got["Asset"] {
		t.Error("Asset: embeddable = true, want false (Asset is a resource, not a standalone component)")
	}
}
