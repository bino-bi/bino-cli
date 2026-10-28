package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleRefreshState(t *testing.T) {
	srv, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	get := func() (int, string) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/__preview/refresh-state", nil)
		w := httptest.NewRecorder()
		srv.handleRefreshState(w, req)
		return w.Code, strings.TrimSpace(w.Body.String())
	}

	if code, _ := get(); code != http.StatusServiceUnavailable {
		t.Errorf("before the first refresh: status %d, want 503", code)
	}

	srv.SetRefreshResult(errors.New("bad yaml"))
	if code, body := get(); code != http.StatusOK || body != `{"ok":false,"error":"bad yaml"}` {
		t.Errorf("after a failed refresh: %d %s", code, body)
	}

	// A queued change makes the last result stale.
	srv.SetRefreshPending()
	if code, _ := get(); code != http.StatusServiceUnavailable {
		t.Errorf("with a pending change: status %d, want 503", code)
	}

	// A later good refresh replaces the failure.
	srv.SetRefreshResult(nil)
	if code, body := get(); code != http.StatusOK || body != `{"ok":true}` {
		t.Errorf("after a good refresh: %d %s", code, body)
	}
}
