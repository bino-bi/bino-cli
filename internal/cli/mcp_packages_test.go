package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/registry"
)

const packagesTestPackage = `kind: Table
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

const packagesTestLockfile = `lockfile_version = 1

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

// newPackagesProject materializes a project with one locked+installed package
// and one declared-but-unlocked dependency, isolating HOME so the resolved
// registry config never reads the developer's credentials.
func newPackagesProject(t *testing.T, registryURL string) string {
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
		regFile:                          packagesTestPackage,
		filepath.Join(root, "bino.lock"): packagesTestLockfile,
		filepath.Join(root, "bino.toml"): binoToml,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// newPackagesClient builds an MCP server with the registry tools enabled over
// root and returns a connected in-memory client plus the shared State.
func newPackagesClient(t *testing.T, root string) (*mcpsdk.ClientSession, *daemon.State) {
	t.Helper()
	ctx := context.Background()

	state := newTestState(t, root)
	server := mcp.NewServer(mcp.Deps{State: state, Packages: newCLIPackages(state)})
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, state
}

// newFakeRegistry serves the search and (v1) resolve routes the tests need;
// everything else 404s, which makes ResolveTree fall back from v2 to v1.
func newFakeRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/registry/search":
			_, _ = w.Write([]byte(`{"page":1,"perPage":20,"totalItems":1,"totalPages":1,` +
				`"items":[{"package":"@acme/kpi-card","kind":"Table","description":"KPIs","latestVersion":"1.2.0","pullsTotal":7}]}`))
		case "/api/registry/resolve/acme/kpi-card":
			_, _ = w.Write([]byte(`{"package":"@acme/kpi-card","tag":"latest","version":"1.3.0",` +
				`"kind":"Table","digest":"sha256:def","dependencies":[],"downloadUrl":""}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

func toolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", res.Content[0])
	}
	return tc.Text
}

func TestRegistryPackagesOffline(t *testing.T) {
	root := newPackagesProject(t, "http://127.0.0.1:1") // closed port: the network is unreachable
	cs, _ := newPackagesClient(t, root)
	ctx := context.Background()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"registry_packages", "registry_search", "registry_info", "registry_auth_status", "registry_add", "registry_update", "registry_remove", "registry_install"} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %s missing: %v", want, names)
		}
	}

	res := callTool(t, cs, "registry_packages", nil)
	if res.IsError {
		t.Fatalf("registry_packages failed: %+v", res.Content)
	}
	text := toolText(t, res)
	var body struct {
		Packages []daemon.RegistryPackage `json:"packages"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("unmarshal %q: %v", text, err)
	}
	if len(body.Packages) != 2 {
		t.Fatalf("expected 2 packages (locked + declared-unlocked), got %d: %+v", len(body.Packages), body.Packages)
	}
	locked := body.Packages[0] // sorted by name: @acme/kpi-card first
	if locked.Name != "@acme/kpi-card" || locked.Version != "1.2.0" || !locked.Installed || !locked.Direct || locked.DeclaredRef != "latest" {
		t.Errorf("locked package wrong: %+v", locked)
	}
	if len(locked.Params) != 2 || locked.Params[0].Name != "REGION" {
		t.Errorf("locked package params should come from the loaded document: %+v", locked.Params)
	}
	unlocked := body.Packages[1]
	if unlocked.Name != "@acme/unlocked" || unlocked.DeclaredRef != "1.0.0" || !unlocked.Direct || unlocked.Installed || unlocked.Version != "" {
		t.Errorf("declared-but-unlocked package wrong: %+v", unlocked)
	}

	// The resource serves the same payload as the tool.
	rr, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "bino://packages"})
	if err != nil {
		t.Fatalf("read bino://packages: %v", err)
	}
	if len(rr.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(rr.Contents))
	}
	var viaTool, viaResource any
	if err := json.Unmarshal([]byte(text), &viaTool); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(rr.Contents[0].Text), &viaResource); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(viaTool, viaResource) {
		t.Errorf("resource payload differs from tool payload:\n%s\n%s", text, rr.Contents[0].Text)
	}
}

func TestRegistryAuthStatusNeverReturnsToken(t *testing.T) {
	const secret = "tok-secret-must-not-leak"
	root := newPackagesProject(t, "http://127.0.0.1:1")
	cs, _ := newPackagesClient(t, root)
	t.Setenv("BINO_REGISTRY_TOKEN", secret)

	res := callTool(t, cs, "registry_auth_status", nil)
	if res.IsError {
		t.Fatalf("registry_auth_status failed: %+v", res.Content)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("token leaked into the tool result: %s", raw)
	}
	var status mcp.RegistryAuthStatus
	if err := json.Unmarshal([]byte(toolText(t, res)), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated {
		t.Errorf("expected authenticated with BINO_REGISTRY_TOKEN set: %+v", status)
	}
	if status.URL != "http://127.0.0.1:1" {
		t.Errorf("url = %q, want the bino.toml registry url", status.URL)
	}
	if want := filepath.Join(root, ".bino", "credentials.json"); status.CredentialsPath != want {
		t.Errorf("credentialsPath = %q, want %q", status.CredentialsPath, want)
	}

	t.Setenv("BINO_REGISTRY_TOKEN", "")
	res = callTool(t, cs, "registry_auth_status", nil)
	if res.IsError {
		t.Fatalf("registry_auth_status failed: %+v", res.Content)
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &status); err != nil {
		t.Fatal(err)
	}
	if status.Authenticated {
		t.Errorf("expected anonymous without a token: %+v", status)
	}
	if !strings.Contains(status.Hint, "bino registry login") {
		t.Errorf("hint should tell the human to run `bino registry login`: %q", status.Hint)
	}
}

func TestRegistryInfoMalformedSpecIsToolError(t *testing.T) {
	root := newPackagesProject(t, "http://127.0.0.1:1")
	cs, _ := newPackagesClient(t, root)

	res := callTool(t, cs, "registry_info", map[string]any{"spec": "not-a-spec"})
	if !res.IsError {
		t.Fatalf("malformed spec should be a tool error: %+v", res.Content)
	}
	if text := toolText(t, res); !strings.Contains(text, "invalid package") {
		t.Errorf("error text = %q, want an invalid package message", text)
	}

	// An unreachable registry is a readable tool error too, not a protocol error.
	res = callTool(t, cs, "registry_search", map[string]any{"query": "x"})
	if !res.IsError {
		t.Errorf("search against an unreachable registry should be a tool error: %+v", res.Content)
	}
}

// The daemon HTTP endpoints and the MCP tools are two surfaces over one
// computation; for the same project they must return equal payloads.
func TestRegistryHTTPAndMCPPayloadsMatch(t *testing.T) {
	fake := newFakeRegistry(t)
	root := newPackagesProject(t, fake.URL)
	cs, state := newPackagesClient(t, root)

	srv, err := daemon.NewServer(daemon.ServerConfig{ListenAddr: "127.0.0.1:0", State: state})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srvCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Start(srvCtx) }()

	tests := []struct {
		name string
		path string
		tool string
		args map[string]any
	}{
		{"packages", "/registry/packages", "registry_packages", nil},
		{"search", "/registry/search?q=kpi", "registry_search", map[string]any{"query": "kpi"}},
		{"info", "/registry/info?spec=%40acme%2Fkpi-card", "registry_info", map[string]any{"spec": "@acme/kpi-card"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", srv.Port(), tt.path)) //nolint:noctx // test
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d, body %s", tt.path, resp.StatusCode, body)
			}
			var viaHTTP any
			if err := json.Unmarshal(body, &viaHTTP); err != nil {
				t.Fatalf("unmarshal http body %s: %v", body, err)
			}

			res := callTool(t, cs, tt.tool, tt.args)
			if res.IsError {
				t.Fatalf("%s failed: %+v", tt.tool, res.Content)
			}
			text := toolText(t, res)
			var viaMCP any
			if err := json.Unmarshal([]byte(text), &viaMCP); err != nil {
				t.Fatalf("unmarshal tool text %s: %v", text, err)
			}
			if !reflect.DeepEqual(viaHTTP, viaMCP) {
				t.Errorf("payloads differ\nhttp: %s\nmcp:  %s", body, text)
			}
			if tt.name == "info" {
				info, _ := viaMCP.(map[string]any)
				if info["version"] != "1.3.0" || info["installedVersion"] != "1.2.0" {
					t.Errorf("info should carry remote 1.3.0 + installed 1.2.0: %s", text)
				}
			}
		})
	}
}

// newMutationProject writes a bare project (bino.toml only, no lock, no store)
// pointing at registryURL, with HOME isolated like newPackagesProject. The
// process working directory is never changed: the MCP path must resolve the
// project from the State's root alone.
func newMutationProject(t *testing.T, registryURL string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("BINO_REGISTRY_TOKEN", "")
	t.Setenv("BINO_REGISTRY_URL", "")
	binoToml := "report-id = \"test\"\n\n[registry]\nurl = \"" + registryURL + "\"\n"
	if err := os.WriteFile(filepath.Join(root, "bino.toml"), []byte(binoToml), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func mutationResult(t *testing.T, res *mcpsdk.CallToolResult) mcp.RegistryMutationResult {
	t.Helper()
	var out mcp.RegistryMutationResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", toolText(t, res), err)
	}
	return out
}

func findChange(changes []mcp.RegistryChange, name string) *mcp.RegistryChange {
	for i := range changes {
		if changes[i].Name == name {
			return &changes[i]
		}
	}
	return nil
}

func readFileT(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// storeListing returns every file under .bino/registry, relative to it.
func storeListing(t *testing.T, root string) []string {
	t.Helper()
	store := filepath.Join(root, ".bino", "registry")
	var out []string
	err := filepath.WalkDir(store, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(store, path)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

func TestRegistryAddWritesTomlLockStoreAndIsIdempotent(t *testing.T) {
	greetingBody, greetingDigest := fakeDoc(t, "@acme/greeting", "Text")
	styleBody, styleDigest := fakeDoc(t, "@acme/style", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/greeting": {tag: "latest", version: "1.2.0", kind: "Text", dependencies: []string{"@acme/style"}, body: greetingBody, digest: greetingDigest},
		"@acme/style":    {tag: "latest", version: "2.0.0", kind: "ComponentStyle", body: styleBody, digest: styleDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	cs, _ := newPackagesClient(t, root)

	res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/greeting"}})
	if res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}
	out := mutationResult(t, res)
	if len(out.Changes) != 2 {
		t.Fatalf("changes = %+v, want 2", out.Changes)
	}
	if g := findChange(out.Changes, "@acme/greeting"); g == nil || g.Before != "" || g.After != "1.2.0" || g.Tag != "latest" || !g.Direct {
		t.Errorf("greeting change: %+v", g)
	}
	if s := findChange(out.Changes, "@acme/style"); s == nil || s.Before != "" || s.After != "2.0.0" || s.Direct {
		t.Errorf("style change: %+v", s)
	}

	tomlPath := filepath.Join(root, "bino.toml")
	lockPath := filepath.Join(root, "bino.lock")
	if data := readFileT(t, tomlPath); !strings.Contains(string(data), `"@acme/greeting" = "latest"`) {
		t.Errorf("bino.toml missing dependency:\n%s", data)
	}
	lock, err := registry.LoadLockfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packages) != 2 {
		t.Fatalf("lock has %d packages, want 2: %+v", len(lock.Packages), lock.Packages)
	}
	greetingPath := filepath.Join(root, ".bino", "registry", "acme", "greeting", "greeting.yml")
	if data := readFileT(t, greetingPath); string(data) != string(greetingBody) {
		t.Errorf("materialized greeting.yml differs from the published body")
	}

	tomlBefore := readFileT(t, tomlPath)
	lockBefore := readFileT(t, lockPath)
	storeBefore := storeListing(t, root)

	// A second identical add resolves the same closure and leaves bino.toml,
	// bino.lock and the store byte-identical.
	res = callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/greeting"}})
	if res.IsError {
		t.Fatalf("second registry_add failed: %s", toolText(t, res))
	}
	out = mutationResult(t, res)
	for _, c := range out.Changes {
		if c.Before != c.After {
			t.Errorf("second add should be a no-op, got %+v", c)
		}
	}
	if string(readFileT(t, tomlPath)) != string(tomlBefore) {
		t.Error("bino.toml changed on the second add")
	}
	if string(readFileT(t, lockPath)) != string(lockBefore) {
		t.Error("bino.lock changed on the second add")
	}
	if data := readFileT(t, greetingPath); string(data) != string(greetingBody) {
		t.Error("greeting.yml changed on the second add")
	}
	if got := storeListing(t, root); !reflect.DeepEqual(got, storeBefore) {
		t.Errorf("store listing changed on the second add:\nbefore %v\nafter  %v", storeBefore, got)
	}
}

func TestRegistryRemoveSweepsUnreachableKeepsShared(t *testing.T) {
	aBody, aDigest := fakeDoc(t, "@acme/a", "Text")
	bBody, bDigest := fakeDoc(t, "@acme/b", "Text")
	sharedBody, sharedDigest := fakeDoc(t, "@acme/shared", "ComponentStyle")
	onlyBody, onlyDigest := fakeDoc(t, "@acme/only", "ComponentStyle")
	packages := map[string]*fakePackage{
		"@acme/a":      {tag: "latest", version: "1.0.0", kind: "Text", dependencies: []string{"@acme/shared", "@acme/only"}, body: aBody, digest: aDigest},
		"@acme/b":      {tag: "latest", version: "1.0.0", kind: "Text", dependencies: []string{"@acme/shared"}, body: bBody, digest: bDigest},
		"@acme/shared": {tag: "latest", version: "1.0.0", kind: "ComponentStyle", body: sharedBody, digest: sharedDigest},
		"@acme/only":   {tag: "latest", version: "1.0.0", kind: "ComponentStyle", body: onlyBody, digest: onlyDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	cs, _ := newPackagesClient(t, root)

	if res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/a", "@acme/b"}}); res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}

	res := callTool(t, cs, "registry_remove", map[string]any{"packages": []string{"@acme/a"}})
	if res.IsError {
		t.Fatalf("registry_remove failed: %s", toolText(t, res))
	}
	out := mutationResult(t, res)
	if len(out.Changes) != 2 {
		t.Fatalf("changes = %+v, want exactly @acme/a and @acme/only", out.Changes)
	}
	for _, name := range []string{"@acme/a", "@acme/only"} {
		if c := findChange(out.Changes, name); c == nil || c.After != "" || c.Before != "1.0.0" {
			t.Errorf("%s should be reported removed: %+v", name, c)
		}
	}

	lock, err := registry.LoadLockfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Get("@acme/a") != nil || lock.Get("@acme/only") != nil {
		t.Errorf("swept packages still locked: %+v", lock.Packages)
	}
	if lock.Get("@acme/shared") == nil || lock.Get("@acme/b") == nil {
		t.Errorf("shared transitive or remaining root missing from lock: %+v", lock.Packages)
	}
	store := filepath.Join(root, ".bino", "registry", "acme")
	if _, err := os.Stat(filepath.Join(store, "shared", "shared.yml")); err != nil {
		t.Errorf("shared.yml should survive: %v", err)
	}
	for _, gone := range []string{filepath.Join(store, "a", "a.yml"), filepath.Join(store, "only", "only.yml")} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should be swept, stat err = %v", gone, err)
		}
	}
	toml := string(readFileT(t, filepath.Join(root, "bino.toml")))
	if strings.Contains(toml, "@acme/a\"") {
		t.Errorf("bino.toml still declares @acme/a:\n%s", toml)
	}
	if !strings.Contains(toml, `"@acme/b"`) {
		t.Errorf("bino.toml lost @acme/b:\n%s", toml)
	}

	res = callTool(t, cs, "registry_remove", map[string]any{"packages": []string{"@acme/nope"}})
	if !res.IsError || !strings.Contains(toolText(t, res), "not a declared dependency") {
		t.Errorf("removing an undeclared package should be a tool error: %+v", res.Content)
	}
}

func TestRegistryInstallRefusesDriftedLock(t *testing.T) {
	xBody, xDigest := fakeDoc(t, "@acme/x", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: xBody, digest: xDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	cs, _ := newPackagesClient(t, root)

	if res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/x"}}); res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}
	// Hand-edit the declaration so bino.toml and bino.lock disagree.
	if err := registry.SetDependency(root, "@acme/x", "9.9.9"); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(root, ".bino", "registry")
	if err := os.RemoveAll(store); err != nil {
		t.Fatal(err)
	}

	res := callTool(t, cs, "registry_install", nil)
	if !res.IsError {
		t.Fatalf("install on a drifted lock must fail: %s", toolText(t, res))
	}
	if text := toolText(t, res); !strings.Contains(text, "out of date") {
		t.Errorf("error = %q, want the drift message", text)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("drifted install must not materialize anything, stat err = %v", err)
	}

	// Back in sync, install replays the lock and is idempotent.
	if err := registry.SetDependency(root, "@acme/x", "latest"); err != nil {
		t.Fatal(err)
	}
	res = callTool(t, cs, "registry_install", nil)
	if res.IsError {
		t.Fatalf("registry_install failed: %s", toolText(t, res))
	}
	out := mutationResult(t, res)
	if len(out.Changes) != 1 || out.Changes[0].Before != "1.0.0" || out.Changes[0].After != "1.0.0" {
		t.Errorf("install changes = %+v", out.Changes)
	}
	xPath := filepath.Join(store, "acme", "x", "x.yml")
	if data := readFileT(t, xPath); string(data) != string(xBody) {
		t.Error("x.yml not re-materialized")
	}
	lockBefore := readFileT(t, filepath.Join(root, "bino.lock"))
	res = callTool(t, cs, "registry_install", nil)
	if res.IsError {
		t.Fatalf("second registry_install failed: %s", toolText(t, res))
	}
	if !reflect.DeepEqual(mutationResult(t, res), out) {
		t.Errorf("second install result differs: %s", toolText(t, res))
	}
	if string(readFileT(t, filepath.Join(root, "bino.lock"))) != string(lockBefore) {
		t.Error("bino.lock changed on the second install")
	}
}

func TestRegistryAddFailureMidDownloadWritesNothing(t *testing.T) {
	xBody, xDigest := fakeDoc(t, "@acme/x", "Text")
	aBody, aDigest := fakeDoc(t, "@acme/a", "ComponentStyle")
	badBody, _ := fakeDoc(t, "@acme/bad", "Text")
	packages := map[string]*fakePackage{
		"@acme/x": {tag: "latest", version: "1.0.0", kind: "Text", body: xBody, digest: xDigest},
		// Closure order is by name: @acme/a is downloaded and verified into
		// memory first, then @acme/bad fails verification.
		"@acme/a":   {tag: "latest", version: "1.0.0", kind: "ComponentStyle", body: aBody, digest: aDigest},
		"@acme/bad": {tag: "latest", version: "1.0.0", kind: "Text", dependencies: []string{"@acme/a"}, body: badBody, digest: "sha256:wrong"},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	cs, _ := newPackagesClient(t, root)

	if res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/x"}}); res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}
	tomlBefore := readFileT(t, filepath.Join(root, "bino.toml"))
	lockBefore := readFileT(t, filepath.Join(root, "bino.lock"))
	storeBefore := storeListing(t, root)

	res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/bad"}})
	if !res.IsError {
		t.Fatalf("adding a package with a wrong digest must fail: %s", toolText(t, res))
	}
	if text := toolText(t, res); !strings.Contains(text, "does not match") {
		t.Errorf("error = %q, want a digest mismatch", text)
	}
	if n := packages["@acme/a"].fileDownloads.Load(); n == 0 {
		t.Error("@acme/a should have been downloaded before the failure")
	}
	if string(readFileT(t, filepath.Join(root, "bino.toml"))) != string(tomlBefore) {
		t.Error("bino.toml changed after a failed add")
	}
	if string(readFileT(t, filepath.Join(root, "bino.lock"))) != string(lockBefore) {
		t.Error("bino.lock changed after a failed add")
	}
	if got := storeListing(t, root); !reflect.DeepEqual(got, storeBefore) {
		t.Errorf("store changed after a failed add:\nbefore %v\nafter  %v", storeBefore, got)
	}
}

// newPackagesClient builds on newTestState, a bare daemon.State with no
// ManagedState and no file watcher, so only the explicit reload after a write
// can make describe_project see the installed documents.
func TestRegistryAddRefreshesStateWithoutWatcher(t *testing.T) {
	greetingBody, greetingDigest := fakeDoc(t, "@acme/greeting", "Text")
	packages := map[string]*fakePackage{
		"@acme/greeting": {tag: "latest", version: "1.2.0", kind: "Text", body: greetingBody, digest: greetingDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	cs, _ := newPackagesClient(t, root)

	describe := func() daemon.IndexResult {
		res := callTool(t, cs, "describe_project", nil)
		if res.IsError {
			t.Fatalf("describe_project failed: %s", toolText(t, res))
		}
		var idx daemon.IndexResult
		if err := json.Unmarshal([]byte(toolText(t, res)), &idx); err != nil {
			t.Fatal(err)
		}
		return idx
	}
	findDoc := func(idx daemon.IndexResult) *daemon.IndexDocument {
		for i := range idx.Documents {
			if idx.Documents[i].Name == "@acme/greeting" {
				return &idx.Documents[i]
			}
		}
		return nil
	}

	if d := findDoc(describe()); d != nil {
		t.Fatalf("package document present before add: %+v", d)
	}
	if res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/greeting"}}); res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}
	d := findDoc(describe())
	if d == nil {
		t.Fatal("describe_project does not list @acme/greeting right after registry_add")
	}
	if d.Kind != "Text" || !strings.HasPrefix(d.File, filepath.Join(root, ".bino", "registry")) {
		t.Errorf("package document = %+v", d)
	}

	res := callTool(t, cs, "registry_packages", nil)
	if res.IsError {
		t.Fatalf("registry_packages failed: %s", toolText(t, res))
	}
	var body struct {
		Packages []daemon.RegistryPackage `json:"packages"`
	}
	if err := json.Unmarshal([]byte(toolText(t, res)), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Packages) != 1 || body.Packages[0].Name != "@acme/greeting" || !body.Packages[0].Installed {
		t.Errorf("registry_packages = %+v, want @acme/greeting installed", body.Packages)
	}

	if res := callTool(t, cs, "registry_remove", map[string]any{"packages": []string{"@acme/greeting"}}); res.IsError {
		t.Fatalf("registry_remove failed: %s", toolText(t, res))
	}
	if d := findDoc(describe()); d != nil {
		t.Errorf("describe_project still lists the removed package: %+v", d)
	}
}

func TestRegistryAddReportsNameCollision(t *testing.T) {
	greetingBody, greetingDigest := fakeDoc(t, "@acme/greeting", "Text")
	packages := map[string]*fakePackage{
		"@acme/greeting": {tag: "latest", version: "1.2.0", kind: "Text", body: greetingBody, digest: greetingDigest},
	}
	srv, _, _ := fakeRegistryServer(t, packages)
	root := newMutationProject(t, srv.URL)
	localFile := filepath.Join(root, "local.yml")
	local := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: \"@acme/greeting\"\nspec:\n  value: local\n"
	if err := os.WriteFile(localFile, []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	cs, _ := newPackagesClient(t, root)

	res := callTool(t, cs, "registry_add", map[string]any{"specs": []string{"@acme/greeting"}})
	if res.IsError {
		t.Fatalf("registry_add failed: %s", toolText(t, res))
	}
	out := mutationResult(t, res)
	if len(out.NameCollisions) != 1 {
		t.Fatalf("nameCollisions = %+v, want exactly one", out.NameCollisions)
	}
	c := out.NameCollisions[0]
	if c.Kind != "Text" || c.Name != "@acme/greeting" || c.Package != "@acme/greeting" || c.LocalFile != localFile {
		t.Errorf("collision = %+v", c)
	}
	if !strings.HasPrefix(c.PackageFile, filepath.Join(root, ".bino", "registry")) {
		t.Errorf("packageFile = %q, want a store path", c.PackageFile)
	}
	if !strings.Contains(c.Hint, "rename the local") {
		t.Errorf("hint = %q", c.Hint)
	}
}
