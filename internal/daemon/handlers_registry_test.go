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
)

const registryTestPackage = `kind: Table
apiVersion: bino.bi/v1
metadata:
  name: "@acme/kpi-card"
  params:
    - name: REGION
      type: select
      required: true
      options:
        items:
          - value: eu
            label: Europe
    - name: LIMIT
      type: number
      default: "10"
spec:
  title: KPI Card
`

const registryTestLockfile = `lockfile_version = 1

[[package]]
name = "@acme/kpi-card"
version = "1.2.0"
tag = "latest"
digest = "sha256:abc"
kind = "Table"
path = ".bino/registry/acme/kpi-card.yml"
direct = true
dependencies = []
`

// newRegistryTestServer materializes a project with one locked+installed
// package and one declared-but-unlocked dependency, isolating HOME so the
// resolved registry config never reads the developer's credentials.
func newRegistryTestServer(t *testing.T, registryURL string) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("BINO_REGISTRY_TOKEN", "")
	t.Setenv("BINO_REGISTRY_URL", "")

	regFile := filepath.Join(root, ".bino", "registry", "acme", "kpi-card.yml")
	if err := os.MkdirAll(filepath.Dir(regFile), 0o755); err != nil {
		t.Fatal(err)
	}
	binoToml := "report-id = \"test\"\n\n[registry]\nurl = \"" + registryURL + "\"\n\n" +
		"[dependencies]\n\"@acme/kpi-card\" = \"latest\"\n\"@acme/unlocked\" = \"1.0.0\"\n"
	for path, content := range map[string]string{
		regFile:                          registryTestPackage,
		filepath.Join(root, "bino.lock"): registryTestLockfile,
		filepath.Join(root, "bino.toml"): binoToml,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	srv := newWizardTestServer(t, root)
	if err := srv.state.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return srv
}

func TestRegistryPackagesEndpoint(t *testing.T) {
	srv := newRegistryTestServer(t, "http://127.0.0.1:1") // packages is offline; URL unused
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/registry/packages", nil)
	w := httptest.NewRecorder()
	srv.handleRegistryPackages(w, req)

	var body struct {
		Packages []RegistryPackage `json:"packages"`
		Error    string            `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Error != "" {
		t.Fatalf("unexpected error: %s", body.Error)
	}
	if len(body.Packages) != 2 {
		t.Fatalf("expected 2 packages (locked + declared-unlocked), got %d: %+v", len(body.Packages), body.Packages)
	}

	locked := body.Packages[0] // sorted by name: @acme/kpi-card first
	if locked.Name != "@acme/kpi-card" || locked.Version != "1.2.0" || locked.Tag != "latest" || locked.Kind != "Table" {
		t.Errorf("locked package fields wrong: %+v", locked)
	}
	if !locked.Installed || !locked.Direct || locked.DeclaredRef != "latest" {
		t.Errorf("locked package flags wrong: %+v", locked)
	}
	if len(locked.Params) != 2 || locked.Params[0].Name != "REGION" || len(locked.Params[0].Options) != 1 {
		t.Errorf("locked package params should come from the loaded document: %+v", locked.Params)
	}

	unlocked := body.Packages[1]
	if unlocked.Name != "@acme/unlocked" || unlocked.Installed || !unlocked.Direct || unlocked.DeclaredRef != "1.0.0" {
		t.Errorf("declared-but-unlocked package wrong: %+v", unlocked)
	}
}

func TestRegistrySearchEndpoint(t *testing.T) {
	var gotQuery string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/registry/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"page":1,"perPage":20,"totalItems":1,"totalPages":1,` +
			`"items":[{"package":"@acme/kpi-card","kind":"Table","description":"KPIs","latestVersion":"1.2.0","pullsTotal":7}]}`))
	}))
	defer fake.Close()

	srv := newRegistryTestServer(t, fake.URL)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/registry/search?q=kpi&perPage=20", nil)
	w := httptest.NewRecorder()
	srv.handleRegistrySearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if gotQuery != "kpi" {
		t.Errorf("upstream query = %q, want kpi", gotQuery)
	}
	var body struct {
		Items []struct {
			Package string `json:"package"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].Package != "@acme/kpi-card" {
		t.Errorf("search result not passed through: %s", w.Body.String())
	}
}

func TestRegistryInfoEndpoint(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/registry/resolve/acme/kpi-card" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"package":"@acme/kpi-card","tag":"latest","version":"1.3.0",` +
			`"kind":"Table","digest":"sha256:def","dependencies":[],"downloadUrl":""}`))
	}))
	defer fake.Close()

	srv := newRegistryTestServer(t, fake.URL)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/registry/info?spec=%40acme%2Fkpi-card", nil)
	w := httptest.NewRecorder()
	srv.handleRegistryInfo(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var body struct {
		Version          string `json:"version"`
		InstalledVersion string `json:"installedVersion"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Version != "1.3.0" || body.InstalledVersion != "1.2.0" {
		t.Errorf("info should carry remote 1.3.0 + installed 1.2.0, got %+v", body)
	}

	// A malformed spec is a client error, not a proxy call.
	req = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/registry/info?spec=not-a-spec", nil)
	w = httptest.NewRecorder()
	srv.handleRegistryInfo(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("malformed spec should 400, got %d", w.Code)
	}
}

func TestRegistryReason(t *testing.T) {
	tests := []struct {
		name    string
		reasons []string
		want    bool
	}{
		{"lockfile", []string{"change bino.lock"}, true},
		{"project config", []string{"change reports/a.yaml", "change bino.toml"}, true},
		{"store file", []string{"change .bino/registry/acme/kpi.yml"}, true},
		{"unrelated", []string{"change reports/a.yaml"}, false},
		{"empty", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RegistryReason(tt.reasons); got != tt.want {
				t.Errorf("RegistryReason(%v) = %v, want %v", tt.reasons, got, tt.want)
			}
		})
	}
}

func TestHealthAdvertisesRegistryCapabilities(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	var body struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	caps := make(map[string]bool, len(body.Capabilities))
	for _, c := range body.Capabilities {
		caps[c] = true
	}
	for _, want := range []string{"registry-packages", "registry-search", "registry-info", "registry-events"} {
		if !caps[want] {
			t.Errorf("capabilities missing %q: %v", want, body.Capabilities)
		}
	}
}

// A package is a file tree, so the served shape lists every file it installs
// and reports "installed" only when all of them are on disk — stat-ing the
// package directory would succeed on an empty one. A single-document package
// with bundled resources must list each of them once, not its document three
// times.
func TestRegistryPackagesReportsEveryFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("BINO_REGISTRY_TOKEN", "")
	t.Setenv("BINO_REGISTRY_URL", "")

	lock := `lockfile_version = 2

[[package]]
name = "@acme/kit"
version = "2.0.0"
digest = "sha256:manifest"
format = "tree"
kind = "Table"
path = ".bino/registry/acme/kit/kit.yaml"
direct = true
dependencies = []
kinds = ["LayoutPage", "Table"]

[[package.files]]
path = "kit.yaml"
type = "document"
digest = "sha256:a"

[[package.files]]
path = "components/sales.yaml"
type = "document"
digest = "sha256:b"

[[package]]
name = "@acme/solo"
version = "1.0.0"
digest = "sha256:c"
format = "document"
kind = "Text"
path = ".bino/registry/acme/solo/solo.yml"
direct = true
dependencies = []

[[package.resources]]
name = "sales.csv"
content_hash = "sha256:d"
`
	write := func(rel, body string) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("bino.toml", "report-id = \"test\"\n")
	write("bino.lock", lock)
	// The tree's second document is deliberately absent: a partially
	// materialized package is not installed.
	write(".bino/registry/acme/kit/kit.yaml", "kind: LayoutPage\n")
	write(".bino/registry/acme/solo/solo.yml", "kind: Text\n")
	write(".bino/registry/acme/solo/sales.csv", "region\n")

	srv := newWizardTestServer(t, root)
	if err := srv.state.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/registry/packages", nil)
	w := httptest.NewRecorder()
	srv.handleRegistryPackages(w, req)

	var body struct {
		Packages []RegistryPackage `json:"packages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Packages) != 2 {
		t.Fatalf("packages = %+v", body.Packages)
	}
	kit, solo := body.Packages[0], body.Packages[1]

	wantKit := []string{".bino/registry/acme/kit/kit.yaml", ".bino/registry/acme/kit/components/sales.yaml"}
	if strings.Join(kit.Files, ",") != strings.Join(wantKit, ",") {
		t.Errorf("tree files = %v, want %v", kit.Files, wantKit)
	}
	if kit.Installed {
		t.Error("a package missing one of its files must not report as installed")
	}
	if strings.Join(kit.Kinds, ",") != "LayoutPage,Table" {
		t.Errorf("kinds = %v", kit.Kinds)
	}

	wantSolo := []string{".bino/registry/acme/solo/solo.yml", ".bino/registry/acme/solo/sales.csv"}
	if strings.Join(solo.Files, ",") != strings.Join(wantSolo, ",") {
		t.Errorf("document files = %v, want %v", solo.Files, wantSolo)
	}
	if !solo.Installed {
		t.Error("a fully materialized single-document package must report as installed")
	}
}
