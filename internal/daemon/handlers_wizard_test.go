package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/pkg/duckdb"
)

func newWizardTestServer(t *testing.T, projectRoot string) *Server {
	t.Helper()
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Fatalf("duckdb options: %v", err)
	}
	session, err := duckdb.OpenSession(context.Background(), opts)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	st, err := NewState(projectRoot, session, logx.Nop())
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	t.Cleanup(st.Close)

	srv, err := NewServer(ServerConfig{ListenAddr: "127.0.0.1:0", State: st})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestHealthHasVersionAndCapabilities(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["version"] == nil || body["version"] == "" {
		t.Error("missing version field")
	}
	caps, ok := body["capabilities"].([]any)
	if !ok || len(caps) == 0 {
		t.Fatalf("missing capabilities: %v", body["capabilities"])
	}
	found := false
	for _, c := range caps {
		if c == "introspect-draft" {
			found = true
		}
	}
	if !found {
		t.Errorf("capabilities missing introspect-draft: %v", caps)
	}
}

func TestHandleIntrospectDraftCSV(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.csv"), []byte("id,name\n1,Ada\n2,Nik\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newWizardTestServer(t, dir)

	body := `{"spec":{"type":"csv","path":"data.csv"},"limit":5}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/introspect-draft", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleIntrospectDraft(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp introspectDraftResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Version == "" {
		t.Error("missing version in response")
	}
	if len(resp.Columns) != 2 || resp.Columns[0].Name != "id" || resp.Columns[1].Name != "name" {
		t.Fatalf("columns = %+v, want [id name]", resp.Columns)
	}
	if len(resp.SampleRows) != 2 {
		t.Errorf("sample rows = %d, want 2", len(resp.SampleRows))
	}
}

func TestHandleTypedSelect(t *testing.T) {
	srv := newTestServer(t)
	body := `{"source":"sales","pretty":false,"columns":[{"name":"id","type":"BIGINT"},{"name":"Full Name","type":"VARCHAR"}]}`
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/sqlgen/typed-select", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleTypedSelect(w, req)

	var resp struct {
		SQL     string   `json:"sql"`
		Aliases []string `json:"aliases"`
		Version string   `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := `SELECT "id", "Full Name" AS full_name FROM "sales"`
	if resp.SQL != want {
		t.Errorf("sql = %q, want %q", resp.SQL, want)
	}
	if resp.Version == "" {
		t.Error("missing version")
	}
}
