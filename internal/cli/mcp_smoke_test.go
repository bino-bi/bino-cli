package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPStdioSmoke drives the real `bino mcp` binary over stdio end to end,
// exercising the stdio entrypoint and the build subprocess that the in-process
// tests cannot. It is skipped unless BINO_MCP_SMOKE points at a built binary:
//
//	BINO_MCP_SMOKE=$PWD/dist/bino_darwin_arm64_v8.0/bino go test ./internal/cli/ -run TestMCPStdioSmoke -v
func TestMCPStdioSmoke(t *testing.T) {
	bin := os.Getenv("BINO_MCP_SMOKE")
	if bin == "" {
		t.Skip("set BINO_MCP_SMOKE to the bino binary path to run the stdio smoke test")
	}

	// Work on a writable copy of the sample so create/edit/build don't dirty the repo.
	src, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	copyTree(t, src, root)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "smoke", Version: "v0"}, nil)
	transport := &mcpsdk.CommandTransport{Command: exec.CommandContext(ctx, bin, "mcp", "-w", root)} //nolint:gosec // test-controlled binary path
	cs, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect bino mcp: %v", err)
	}
	defer func() { _ = cs.Close() }()

	// Tools are advertised.
	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	t.Logf("advertised %d tools", len(tools.Tools))

	// A resource reads.
	if rr, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "bino://schema/Table"}); err != nil {
		t.Errorf("read bino://schema/Table: %v", err)
	} else if len(rr.Contents) == 0 || !strings.Contains(rr.Contents[0].Text, "$defs") {
		t.Errorf("schema/Table looks wrong over stdio")
	}

	// validate_draft rejects a bad draft.
	vd := smokeCall(ctx, t, cs, "validate_draft", map[string]any{"yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: broken\n"})
	if !strings.Contains(vd, `"valid":false`) {
		t.Errorf("validate_draft should be invalid: %s", vd)
	}

	// create_manifest writes a Text manifest.
	cm := smokeCallResult(ctx, t, cs, "create_manifest", map[string]any{
		"kind": "Text", "name": "smoke_note", "spec": map[string]any{"value": "hi"},
	})
	if cm.IsError {
		t.Fatalf("create_manifest failed: %+v", cm.Content)
	}

	// build runs the real pipeline (needs Chrome; accept success or a structured failure).
	b := smokeCallResult(ctx, t, cs, "build", map[string]any{})
	if b.IsError {
		t.Fatalf("build returned a protocol error: %+v", b.Content)
	}
	t.Logf("build output: %s", smokeText(b))
}

func smokeCallResult(ctx context.Context, t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func smokeCall(ctx context.Context, t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	return smokeText(smokeCallResult(ctx, t, cs, name, args))
}

func smokeText(res *mcpsdk.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcpsdk.TextContent); ok {
		return tc.Text
	}
	b, _ := json.Marshal(res.StructuredContent)
	return string(b)
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy tree: %v", err)
	}
}
