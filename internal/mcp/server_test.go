package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	embedkinds "bino.bi/bino/internal/report/embed"
	"bino.bi/bino/pkg/duckdb"
)

// newTestClient builds a managed State over the sample project, constructs the
// MCP server, and returns a connected in-memory client session.
func newTestClient(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	state := newTestState(t, root)
	server := NewServer(Deps{State: state})

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
	return cs
}

// newTestState builds a daemon State over root without eagerly installing
// DuckDB extensions (the sample/temp projects use only inline + CSV data, which
// need none). Eager extension install hits the network and is exercised by the
// stdio smoke test instead.
func newTestState(t *testing.T, root string) *daemon.State {
	t.Helper()
	ctx := context.Background()
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Fatalf("duckdb options: %v", err)
	}
	session, err := duckdb.OpenSession(ctx, opts)
	if err != nil {
		t.Fatalf("open session: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	state, err := daemon.NewState(root, session, logx.Nop())
	if err != nil {
		t.Fatalf("new state: %v", err)
	}
	t.Cleanup(state.Close)
	if err := state.Refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	return state
}

func readResourceText(t *testing.T, cs *mcpsdk.ClientSession, uri string) string {
	t.Helper()
	res, err := cs.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("read %s: %v", uri, err)
	}
	if len(res.Contents) == 0 {
		t.Fatalf("read %s: no contents", uri)
	}
	return res.Contents[0].Text
}

// callToolJSON calls a tool and unmarshals its text content into out.
func callToolJSON(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s: tool error: %+v", name, res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatalf("call %s: no content", name)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("call %s: content[0] is %T, want TextContent", name, res.Content[0])
	}
	if out != nil {
		if err := json.Unmarshal([]byte(tc.Text), out); err != nil {
			t.Fatalf("call %s: unmarshal %q: %v", name, tc.Text, err)
		}
	}
}

func TestResourcesReadable(t *testing.T) {
	cs := newTestClient(t)

	// bino://schema — full merged schema.
	var merged map[string]any
	if err := json.Unmarshal([]byte(readResourceText(t, cs, "bino://schema")), &merged); err != nil {
		t.Fatalf("schema not JSON: %v", err)
	}
	if _, ok := merged["properties"]; !ok {
		t.Error("merged schema missing properties")
	}

	// bino://schema/Table — built-in kind, must be self-contained (carry $defs).
	var tableSchema map[string]any
	if err := json.Unmarshal([]byte(readResourceText(t, cs, "bino://schema/Table")), &tableSchema); err != nil {
		t.Fatalf("table schema not JSON: %v", err)
	}
	if _, ok := tableSchema["$defs"]; !ok {
		t.Error("Table schema not self-contained: missing $defs")
	}

	// bino://documents — the project index.
	var docs daemon.IndexResult
	if err := json.Unmarshal([]byte(readResourceText(t, cs, "bino://documents")), &docs); err != nil {
		t.Fatalf("documents not JSON: %v", err)
	}
	if !hasDocument(docs.Documents, "DataSet", "revenue_by_region") {
		t.Errorf("project index missing DataSet revenue_by_region; got %+v", docs.Documents)
	}
}

func TestListKindsCategories(t *testing.T) {
	cs := newTestClient(t)

	var out listKindsOutput
	callToolJSON(t, cs, "list_kinds", map[string]any{}, &out)

	want := map[string]string{
		"Table":          "embeddable",
		"DataSet":        "data",
		"LayoutPage":     "layout",
		"ReportArtefact": "artefact",
	}
	got := make(map[string]string, len(out.Kinds))
	for _, k := range out.Kinds {
		got[k.Name] = k.Category
	}
	for name, cat := range want {
		if got[name] != cat {
			t.Errorf("kind %s: category = %q, want %q", name, got[name], cat)
		}
	}
}

// TestListKindsEmbeddable asserts the served `embeddable` flag matches the
// single render-embeddable authority (internal/report/embed) for every kind, so
// bino://kinds, the preview, and the extension never diverge. It also pins the
// Asset/Image decision: Asset is in the embeddable category but is not a
// standalone component, and Image is never a manifest kind.
func TestListKindsEmbeddable(t *testing.T) {
	cs := newTestClient(t)

	var out listKindsOutput
	callToolJSON(t, cs, "list_kinds", map[string]any{}, &out)

	if len(out.Kinds) == 0 {
		t.Fatal("list_kinds returned no kinds")
	}
	for _, k := range out.Kinds {
		if want := embedkinds.IsEmbeddable(k.Name); k.Embeddable != want {
			t.Errorf("kind %s: embeddable = %v, want %v (authority set)", k.Name, k.Embeddable, want)
		}
		if k.Name == "Image" {
			t.Errorf("Image is a layout-child kind, not a manifest kind; it must not appear in bino://kinds")
		}
	}

	got := make(map[string]KindInfo, len(out.Kinds))
	for _, k := range out.Kinds {
		got[k.Name] = k
	}
	for _, name := range []string{"Table", "ChartStructure", "ChartTime", "Text", "Tree", "Grid"} {
		if !got[name].Embeddable {
			t.Errorf("component kind %s: embeddable = false, want true", name)
		}
	}
	// Asset is categorized embeddable (palette grouping) but is a resource, not a
	// standalone-rendering component.
	if asset, ok := got["Asset"]; ok && asset.Embeddable {
		t.Errorf("Asset: embeddable = true, want false (Asset is a resource, not a standalone component)")
	}
}

func TestDescribeKindBuiltin(t *testing.T) {
	cs := newTestClient(t)

	var out describeKindOutput
	callToolJSON(t, cs, "describe_kind", map[string]any{"kind": "Table"}, &out)
	if !out.Found {
		t.Fatal("describe_kind(Table): not found")
	}
	if len(out.Schema) == 0 {
		t.Fatal("describe_kind(Table): empty schema")
	}

	var missing describeKindOutput
	callToolJSON(t, cs, "describe_kind", map[string]any{"kind": "NotAKind"}, &missing)
	if missing.Found {
		t.Error("describe_kind(NotAKind): unexpectedly found")
	}
}

func TestGetColumnsReturnsResult(t *testing.T) {
	cs := newTestClient(t)

	var out daemon.ColumnsResult
	callToolJSON(t, cs, "get_columns", map[string]any{"name": "revenue_by_region"}, &out)
	if out.Name != "revenue_by_region" {
		t.Errorf("name = %q", out.Name)
	}
	// Either columns resolved or a structured error was returned — both prove the
	// tool plumbing works without a protocol-level failure.
	if len(out.Columns) == 0 && out.Error == "" {
		t.Error("get_columns returned neither columns nor error")
	}
}

func TestValidateDraft(t *testing.T) {
	cs := newTestClient(t)

	// A structurally valid Text manifest.
	valid := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: hello\nspec:\n  value: Hello world\n"
	var okRes daemon.ValidateResult
	callToolJSON(t, cs, "validate_draft", map[string]any{"yaml": valid}, &okRes)
	if !okRes.Valid {
		t.Errorf("valid draft reported invalid: %+v", okRes.Diagnostics)
	}

	// Missing required spec → schema-validation diagnostics, not valid.
	invalid := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: broken\n"
	var badRes daemon.ValidateResult
	callToolJSON(t, cs, "validate_draft", map[string]any{"yaml": invalid}, &badRes)
	if badRes.Valid {
		t.Error("invalid draft (missing spec) reported valid")
	}
	if len(badRes.Diagnostics) == 0 {
		t.Error("invalid draft produced no diagnostics")
	}
}

func TestIntrospectSource(t *testing.T) {
	cs := newTestClient(t)

	csvPath := filepath.Join(t.TempDir(), "sales.csv")
	if err := os.WriteFile(csvPath, []byte("category,ac1\nDACH,4250\nNordics,2870\n"), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}

	spec := map[string]any{"type": "csv", "path": csvPath}
	var out introspectSourceOutput
	callToolJSON(t, cs, "introspect_source", map[string]any{"spec": spec}, &out)
	if out.Error != "" {
		t.Fatalf("introspect_source error: %s", out.Error)
	}
	names := make([]string, len(out.Columns))
	for i, c := range out.Columns {
		names[i] = c.Name
	}
	if !slices.Contains(names, "category") || !slices.Contains(names, "ac1") {
		t.Errorf("introspect_source columns = %v, want category + ac1", names)
	}
	if len(out.SampleRows) == 0 {
		t.Error("introspect_source returned no sample rows")
	}
}

func hasDocument(docs []daemon.IndexDocument, kind, name string) bool {
	for _, d := range docs {
		if d.Kind == kind && d.Name == name {
			return true
		}
	}
	return false
}
