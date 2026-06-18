package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/mcp"
)

// newAuthoringClient builds an MCP server (with authoring enabled) over a fresh
// writable temp project and returns a connected in-memory client + the root.
func newAuthoringClient(t *testing.T) (*mcpsdk.ClientSession, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()

	state := newTestState(t, root)
	server := mcp.NewServer(mcp.Deps{State: state, Authoring: newCLIAuthoring(root)})
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
	return cs, root
}

func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func TestCreateManifest(t *testing.T) {
	cs, root := newAuthoringClient(t)

	res := callTool(t, cs, "create_manifest", map[string]any{
		"kind": "Text",
		"name": "hello",
		"spec": map[string]any{"value": "Hello world"},
	})
	if res.IsError {
		t.Fatalf("create_manifest failed: %+v", res.Content)
	}

	// The file was written and validates as a Text manifest.
	written := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(path, ".yaml") {
			data, _ := os.ReadFile(path)
			if strings.Contains(string(data), "kind: Text") && strings.Contains(string(data), "name: hello") {
				written = true
			}
		}
		return nil
	})
	if !written {
		t.Error("create_manifest did not write a Text manifest file")
	}

	// A duplicate name is rejected as a tool error.
	dup := callTool(t, cs, "create_manifest", map[string]any{
		"kind": "Text",
		"name": "hello",
		"spec": map[string]any{"value": "again"},
	})
	if !dup.IsError {
		t.Error("duplicate create_manifest should be an error")
	}
}

func TestEditManifestEndToEnd(t *testing.T) {
	cs, root := newAuthoringClient(t)

	// Seed a manifest file with a comment to prove fidelity.
	original := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: note # keep me\nspec:\n  value: old\n"
	path := filepath.Join(root, "note.yaml")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	res := callTool(t, cs, "edit_manifest", map[string]any{
		"file":  "note.yaml",
		"patch": map[string]any{"spec.value": "new"},
	})
	if res.IsError {
		t.Fatalf("edit_manifest failed: %+v", res.Content)
	}

	data, _ := os.ReadFile(path)
	got := string(data)
	if !strings.Contains(got, "value: new") || strings.Contains(got, "value: old") {
		t.Errorf("edit not applied:\n%s", got)
	}
	if !strings.Contains(got, "# keep me") {
		t.Errorf("comment not preserved:\n%s", got)
	}

	// An edit that violates the schema is rejected and the file is untouched.
	bad := callTool(t, cs, "edit_manifest", map[string]any{
		"file":  "note.yaml",
		"patch": map[string]any{"spec.value": map[string]any{"not": "a string"}},
	})
	if !bad.IsError {
		t.Error("schema-violating edit should be an error")
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "value: new") {
		t.Errorf("file changed after rejected edit:\n%s", string(after))
	}
}

func TestScaffoldSource(t *testing.T) {
	cs, root := newAuthoringClient(t)

	res := callTool(t, cs, "scaffold_source", map[string]any{
		"dataSource": map[string]any{"name": "sales_src", "type": "csv", "path": "sales.csv"},
		"dataSet":    map[string]any{"name": "sales_ds", "sql": "SELECT * FROM sales_src"},
	})
	if res.IsError {
		t.Fatalf("scaffold_source failed: %+v", res.Content)
	}

	var out mcp.ScaffoldResult
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); ok {
		_ = json.Unmarshal([]byte(tc.Text), &out)
	}
	if !out.OK {
		t.Fatalf("scaffold not OK: %+v", out)
	}
	if len(out.Files) != 2 {
		t.Errorf("expected 2 files (DataSource + DataSet), got %+v", out.Files)
	}
	// Both manifests landed on disk.
	for _, f := range out.Files {
		if _, err := os.Stat(filepath.Join(root, f.Path)); err != nil {
			t.Errorf("scaffolded file %s missing: %v", f.Path, err)
		}
	}
}

func TestInitBundle(t *testing.T) {
	cs, _ := newAuthoringClient(t)
	dir := filepath.Join(t.TempDir(), "newbundle")

	res := callTool(t, cs, "init_bundle", map[string]any{"directory": dir})
	if res.IsError {
		t.Fatalf("init_bundle failed: %+v", res.Content)
	}

	var out mcp.InitResult
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); ok {
		_ = json.Unmarshal([]byte(tc.Text), &out)
	}
	if len(out.Files) == 0 {
		t.Fatal("init_bundle created no files")
	}
	if _, err := os.Stat(filepath.Join(dir, "bino.toml")); err != nil {
		t.Errorf("bundle missing bino.toml: %v", err)
	}
}

func TestInitBundleInPlaceDefault(t *testing.T) {
	cs, root := newAuthoringClient(t)

	// No directory given: scaffold in place at the project root.
	res := callTool(t, cs, "init_bundle", map[string]any{})
	if res.IsError {
		t.Fatalf("init_bundle failed: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "bino.toml")); err != nil {
		t.Errorf("init_bundle did not scaffold in place: bino.toml missing at root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "rainbow-report", "bino.toml")); err == nil {
		t.Error("init_bundle created a ./rainbow-report subfolder instead of scaffolding in place")
	}
}

func TestInitBundleRelativeDirUnderRoot(t *testing.T) {
	cs, root := newAuthoringClient(t)

	// A relative directory resolves against the project root, not the process cwd.
	res := callTool(t, cs, "init_bundle", map[string]any{"directory": "q3"})
	if res.IsError {
		t.Fatalf("init_bundle failed: %+v", res.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "q3", "bino.toml")); err != nil {
		t.Errorf("relative directory not resolved under the project root: %v", err)
	}
}

func TestWriteManifestValidation(t *testing.T) {
	cs, root := newAuthoringClient(t)

	// An invalid manifest is rejected before writing.
	bad := callTool(t, cs, "write_manifest", map[string]any{
		"file": "bad.yaml",
		"yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: x\n",
	})
	if !bad.IsError {
		t.Error("invalid write_manifest should be an error")
	}
	if _, err := os.Stat(filepath.Join(root, "bad.yaml")); err == nil {
		t.Error("invalid manifest must not be written")
	}

	// A valid manifest is written.
	good := callTool(t, cs, "write_manifest", map[string]any{
		"file": "good.yaml",
		"yaml": "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: ok\nspec:\n  value: hi\n",
	})
	if good.IsError {
		t.Fatalf("valid write_manifest failed: %+v", good.Content)
	}
	if _, err := os.Stat(filepath.Join(root, "good.yaml")); err != nil {
		t.Errorf("good.yaml not written: %v", err)
	}
}
