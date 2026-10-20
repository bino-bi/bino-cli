package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/plugin"
)

// newLazyClient serves the standalone deps over root in-process and counts how
// often the project state (DuckDB session, plugins, watcher) is opened.
func newLazyClient(t *testing.T, root string) (*mcpsdk.ClientSession, *atomic.Int32, *atomic.Pointer[context.Context]) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	project := newLazyProject(ctx, logx.Nop(), root)
	var opens atomic.Int32
	var openCtx atomic.Pointer[context.Context]
	project.open = func(ctx context.Context, logger logx.Logger, root string) (*daemon.ManagedState, *plugin.PluginRegistry, func(), error) {
		opens.Add(1)
		openCtx.Store(&ctx)
		return loadStandaloneState(ctx, logger, root)
	}
	t.Cleanup(project.close)

	server := mcp.NewServer(newStandaloneDeps(project, false))
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, &opens, &openCtx
}

func describeProject(t *testing.T, cs *mcpsdk.ClientSession) daemon.IndexResult {
	t.Helper()
	res := callTool(t, cs, "describe_project", map[string]any{})
	if res.IsError {
		t.Fatalf("describe_project failed: %s", toolText(t, res))
	}
	var idx daemon.IndexResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &idx); err != nil {
		t.Fatalf("decode describe_project: %v", err)
	}
	return idx
}

func TestStandaloneMCPWithoutProjectOpensNothing(t *testing.T) {
	root := t.TempDir()
	cs, opens, _ := newLazyClient(t, root)
	ctx := context.Background()

	if _, err := cs.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if _, err := cs.ListResources(ctx, nil); err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if res := callTool(t, cs, "list_kinds", map[string]any{}); res.IsError {
		t.Fatalf("list_kinds failed: %s", toolText(t, res))
	}
	// Drafts need no project.
	res := callTool(t, cs, "validate_draft", map[string]any{"yaml": "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: t\nspec:\n  value: hi\n"})
	if res.IsError || !strings.Contains(toolText(t, res), `"valid":true`) {
		t.Fatalf("validate_draft without bino.toml: want a valid draft, got %s", toolText(t, res))
	}
	res = callTool(t, cs, "describe_project", map[string]any{})
	if !res.IsError || !strings.Contains(toolText(t, res), "init_bundle") {
		t.Fatalf("describe_project without bino.toml: want an error naming init_bundle, got %+v", res.Content)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("project state opened %d times, want 0", n)
	}

	// A bundle in a subdirectory leaves the root uninitialized.
	res = callTool(t, cs, "init_bundle", map[string]any{"directory": "sub"})
	if res.IsError {
		t.Fatalf("init_bundle sub failed: %s", toolText(t, res))
	}
	var out mcp.InitResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &out); err != nil {
		t.Fatalf("decode init_bundle: %v", err)
	}
	if !strings.Contains(out.Note, "new session") {
		t.Errorf("init_bundle into a subdirectory: note = %q, want a hint to open a new session", out.Note)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("project state opened %d times after init_bundle in a subdirectory, want 0", n)
	}

	// A bundle in the root makes the next call open the project and see it.
	res = callTool(t, cs, "init_bundle", map[string]any{})
	if res.IsError {
		t.Fatalf("init_bundle failed: %s", toolText(t, res))
	}
	if strings.Contains(toolText(t, res), `"note"`) {
		t.Errorf("init_bundle in the root should carry no note: %s", toolText(t, res))
	}
	idx := describeProject(t, cs)
	if len(idx.Documents) == 0 {
		t.Fatal("describe_project after init_bundle returned no documents")
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("project state opened %d times, want 1", n)
	}
}

func TestStandaloneMCPOpensProjectOnceOnFirstCall(t *testing.T) {
	root := t.TempDir()
	if _, err := newCLIAuthoring(root, nil).InitBundle(context.Background(), mcp.InitBundleInput{}); err != nil {
		t.Fatalf("init bundle: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "bino.toml")); err != nil {
		t.Fatalf("bino.toml missing: %v", err)
	}
	cs, opens, openCtx := newLazyClient(t, root)

	if _, err := cs.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if _, err := cs.ListResources(context.Background(), nil); err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("tools/list and resources/list opened the project state %d times, want 0", n)
	}

	var wg sync.WaitGroup
	results := make([]*mcpsdk.CallToolResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "describe_project", Arguments: map[string]any{}})
		})
	}
	wg.Wait()
	for i, res := range results {
		if errs[i] != nil || res.IsError {
			t.Fatalf("concurrent describe_project %d failed: %v %+v", i, errs[i], res)
		}
	}
	if !strings.Contains(toolText(t, results[0]), "ReportArtefact") {
		t.Errorf("describe_project did not list the bundle: %s", toolText(t, results[0]))
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("project state opened %d times, want 1", n)
	}
	// Plugins live as long as the open ctx, so it must outlive the request.
	if err := (*openCtx.Load()).Err(); err != nil {
		t.Errorf("project state opened with a ctx that ended with the request: %v", err)
	}
}
