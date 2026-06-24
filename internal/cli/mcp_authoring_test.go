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

	// A dry-run edit returns the rewritten file in `content` and writes nothing.
	dry := callTool(t, cs, "edit_manifest", map[string]any{
		"file":   "note.yaml",
		"patch":  map[string]any{"spec.value": "dryval"},
		"dryRun": true,
	})
	if dry.IsError {
		t.Fatalf("dry-run edit_manifest failed: %+v", dry.Content)
	}
	var wr mcp.WriteResult
	if tc, ok := dry.Content[0].(*mcpsdk.TextContent); ok {
		_ = json.Unmarshal([]byte(tc.Text), &wr)
	}
	if wr.Action != "computed" {
		t.Errorf("dry-run action = %q, want computed", wr.Action)
	}
	if !strings.Contains(wr.Content, "value: dryval") || !strings.Contains(wr.Content, "# keep me") {
		t.Errorf("dry-run content did not apply the edit or dropped the comment:\n%s", wr.Content)
	}
	unchanged, _ := os.ReadFile(path)
	if strings.Contains(string(unchanged), "dryval") {
		t.Errorf("dry-run wrote to disk:\n%s", unchanged)
	}
}

func TestRemoveAndReorderManifestEndToEnd(t *testing.T) {
	cs, root := newAuthoringClient(t)

	// Seed a DataSet whose spec.dependencies is a reorderable/removable sequence.
	original := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: ds # keep me\nspec:\n  query: SELECT 1\n  dependencies:\n    - $a\n    - $b\n    - $c\n"
	path := filepath.Join(root, "ds.yaml")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	// remove_manifest_fields deletes the middle element, keeping the comment.
	rm := callTool(t, cs, "remove_manifest_fields", map[string]any{
		"file":  "ds.yaml",
		"paths": []any{"spec.dependencies[1]"},
	})
	if rm.IsError {
		t.Fatalf("remove_manifest_fields failed: %+v", rm.Content)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), "$b") {
		t.Errorf("remove did not delete the element:\n%s", got)
	}
	if !strings.Contains(string(got), "# keep me") {
		t.Errorf("remove dropped the comment:\n%s", got)
	}

	// reorder_manifest_sequence moves the first survivor to the end.
	ro := callTool(t, cs, "reorder_manifest_sequence", map[string]any{
		"file": "ds.yaml",
		"path": "spec.dependencies",
		"from": 0,
		"to":   1,
	})
	if ro.IsError {
		t.Fatalf("reorder_manifest_sequence failed: %+v", ro.Content)
	}
	reordered, _ := os.ReadFile(path)
	if idxA, idxC := strings.Index(string(reordered), "$a"), strings.Index(string(reordered), "$c"); idxA < idxC {
		t.Errorf("reorder did not move $a after $c:\n%s", reordered)
	}

	// A dry-run reorder returns content and writes nothing.
	dry := callTool(t, cs, "reorder_manifest_sequence", map[string]any{
		"file":   "ds.yaml",
		"path":   "spec.dependencies",
		"from":   0,
		"to":     1,
		"dryRun": true,
	})
	if dry.IsError {
		t.Fatalf("dry-run reorder failed: %+v", dry.Content)
	}
	var wr mcp.WriteResult
	if tc, ok := dry.Content[0].(*mcpsdk.TextContent); ok {
		_ = json.Unmarshal([]byte(tc.Text), &wr)
	}
	if wr.Action != "computed" || wr.Content == "" {
		t.Errorf("dry-run reorder = %+v, want computed with content", wr)
	}
	if afterDry, _ := os.ReadFile(path); string(afterDry) != string(reordered) {
		t.Errorf("dry-run reorder wrote to disk")
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

func TestInitBundleBareIsBuiltinMinimal(t *testing.T) {
	root := t.TempDir()
	a := newCLIAuthoring(root)
	dir := filepath.Join(root, "bundle")
	res, err := a.InitBundle(context.Background(), mcp.InitBundleInput{Directory: dir})
	if err != nil {
		t.Fatalf("InitBundle: %v", err)
	}
	if res.Template != "builtin:minimal" {
		t.Errorf("Template = %q, want builtin:minimal", res.Template)
	}
	if res.ResolvedSHA != "" {
		t.Errorf("built-in should have no resolved SHA, got %q", res.ResolvedSHA)
	}
}

func TestInitBundleUncuratedRequiresTrust(t *testing.T) {
	a := newCLIAuthoring(t.TempDir())
	_, err := a.InitBundle(context.Background(), mcp.InitBundleInput{
		Directory: "out",
		Source:    "someowner/somerepo",
	})
	if err == nil || !strings.Contains(err.Error(), "without trust") {
		t.Fatalf("expected uncurated-trust error, got %v", err)
	}
}

func TestInitBundleHonorsContextCancellation(t *testing.T) {
	a := newCLIAuthoring(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before the call: the fetch must not proceed
	_, err := a.InitBundle(ctx, mcp.InitBundleInput{
		Directory: "out",
		Source:    "someowner/somerepo",
		Trust:     true,
	})
	if err == nil {
		t.Fatal("expected error from canceled context")
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
