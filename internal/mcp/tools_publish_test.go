package mcp

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// registry_publish mints an immutable, possibly public, version, so it only
// exists when the human opted in; a server without the opt-in must not list it.
func TestPublishToolRequiresOptIn(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	state := newTestState(t, root)

	for _, allow := range []bool{false, true} {
		server := NewServer(Deps{State: state, AllowPublish: allow})
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

		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatalf("list tools: %v", err)
		}
		names := make([]string, 0, len(tools.Tools))
		for _, tool := range tools.Tools {
			names = append(names, tool.Name)
		}
		if slices.Contains(names, "registry_publish") != allow {
			t.Errorf("AllowPublish=%v: registry_publish listed=%v: %v", allow, !allow, names)
		}
	}
}
