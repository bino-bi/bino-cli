package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
)

// newAuthoringClient builds an MCP server (with authoring enabled) over a fresh
// writable temp project and returns a connected in-memory client + the root.
func newAuthoringClient(t *testing.T) (*mcpsdk.ClientSession, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()

	managed, err := daemon.NewManagedState(ctx, daemon.ManagedStateConfig{ProjectRoot: root, Logger: logx.Nop()})
	if err != nil {
		t.Fatalf("managed state: %v", err)
	}
	t.Cleanup(managed.Close)
	if err := managed.State.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	server := mcp.NewServer(mcp.Deps{State: managed.State, Authoring: newCLIAuthoring(root)})
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
