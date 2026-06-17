package cli

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/pkg/duckdb"
)

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

// startMCPDaemon brings up a daemon HTTP server with /mcp mounted over the
// sample project and returns a connected upstream MCP client session.
func startMCPDaemon(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	state := newTestState(t, root)

	deps := mcp.Deps{State: state}
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return mcp.NewServer(deps)
	}, nil)

	srv, err := daemon.NewServer(daemon.ServerConfig{ListenAddr: "127.0.0.1:0", State: state, MCPHandler: handler})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	srvCtx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	go func() { _ = srv.Start(srvCtx) }()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/mcp", srv.Port())
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	upstream, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect %s: %v", endpoint, err)
	}
	t.Cleanup(func() { _ = upstream.Close() })
	return upstream
}

func callListKinds(t *testing.T, cs *mcpsdk.ClientSession) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: "list_kinds", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("call list_kinds: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_kinds error: %+v", res.Content)
	}
	tc, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T", res.Content[0])
	}
	return tc.Text
}

// TestDaemonMCPMount verifies the daemon serves the MCP over Streamable HTTP.
func TestDaemonMCPMount(t *testing.T) {
	upstream := startMCPDaemon(t)

	if got := callListKinds(t, upstream); !strings.Contains(got, "Table") {
		t.Errorf("list_kinds over HTTP missing Table: %s", got)
	}

	rr, err := upstream.ReadResource(context.Background(), &mcpsdk.ReadResourceParams{URI: "bino://schema"})
	if err != nil {
		t.Fatalf("read bino://schema: %v", err)
	}
	if len(rr.Contents) == 0 || !strings.Contains(rr.Contents[0].Text, "properties") {
		t.Errorf("bino://schema over HTTP looks wrong: %+v", rr.Contents)
	}
}

// TestProxyForwarding verifies the stdio->daemon forwarding proxy: a second
// client talking to the mirrored local server reaches the daemon two hops away.
func TestProxyForwarding(t *testing.T) {
	ctx := context.Background()
	upstream := startMCPDaemon(t)

	local := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "bino", Version: "v0"}, nil)
	if err := mirrorUpstream(ctx, local, upstream); err != nil {
		t.Fatalf("mirror upstream: %v", err)
	}

	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	ls, err := local.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("local connect: %v", err)
	}
	t.Cleanup(func() { _ = ls.Close() })

	proxyClient := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "proxytest", Version: "v0"}, nil)
	pcs, err := proxyClient.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("proxy client connect: %v", err)
	}
	t.Cleanup(func() { _ = pcs.Close() })

	// Tool call forwarded through: proxyClient -> local -> upstream -> daemon HTTP.
	if got := callListKinds(t, pcs); !strings.Contains(got, "DataSet") {
		t.Errorf("list_kinds through proxy missing DataSet: %s", got)
	}

	// Resource template forwarded through both hops.
	rr, err := pcs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: "bino://schema/Table"})
	if err != nil {
		t.Fatalf("read bino://schema/Table through proxy: %v", err)
	}
	if len(rr.Contents) == 0 || !strings.Contains(rr.Contents[0].Text, "$defs") {
		t.Errorf("templated resource through proxy looks wrong: %+v", rr.Contents)
	}
}
