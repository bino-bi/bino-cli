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
	for _, want := range []string{"registry_packages", "registry_search", "registry_info", "registry_auth_status"} {
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
