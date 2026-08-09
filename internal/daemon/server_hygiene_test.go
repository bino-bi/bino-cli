package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// Regression: handlePreviewStart ignored the body-decode error, so a
// malformed body silently started the preview on the default port instead of
// the one the editor asked for.
func TestPreviewStartRejectsMalformedBody(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/preview/start", strings.NewReader("{not json"))
	w := httptest.NewRecorder()
	srv.handlePreviewStart(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a malformed body", w.Code)
	}
	assertJSONError(t, w)
}

// Regression: handleBuild ignored the body-decode error, so a malformed body
// built ALL artefacts instead of the requested one.
func TestBuildRejectsMalformedBody(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/build", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	srv.handleBuild(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a malformed body", w.Code)
	}
	assertJSONError(t, w)
}

// The daemon API must answer errors as JSON — handleColumns used http.Error,
// which stamps text/plain onto a JSON-shaped body.
func TestColumnsMissingNameErrorIsJSON(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/columns", nil)
	w := httptest.NewRecorder()
	srv.handleColumns(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	assertJSONError(t, w)
}

// handleSchema's unknown-kind failure must be a JSON error, not text/plain.
func TestSchemaUnknownKindErrorIsJSON(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/schema?kind=Bogus", nil)
	w := httptest.NewRecorder()
	srv.handleSchema(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	assertJSONError(t, w)
}

func assertJSONError(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("error body is not JSON: %v\nbody: %s", err, w.Body.String())
	}
	if payload.Error == "" {
		t.Errorf("error body has no error field: %s", w.Body.String())
	}
}

// Regression: Documents()/Diagnostics() documented "returns a copy" but
// returned the internal slice — an append by one caller wrote into the
// backing array another goroutine was reading.
func TestStateAccessorsReturnCopies(t *testing.T) {
	s := &State{
		documents:   make([]config.Document, 1, 4),
		diagnostics: make([]Diagnostic, 1, 4),
	}

	docs := s.Documents()
	if len(docs) > 0 && &docs[0] == &s.documents[0] {
		t.Error("Documents() returns an alias of the internal slice, not a copy")
	}
	diags := s.Diagnostics()
	if len(diags) > 0 && &diags[0] == &s.diagnostics[0] {
		t.Error("Diagnostics() returns an alias of the internal slice, not a copy")
	}
}
