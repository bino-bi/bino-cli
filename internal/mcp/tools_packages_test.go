package mcp

import (
	"context"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/registry"
)

// The registry tools and the bino://packages resource only exist when the CLI
// layer supplies Deps.Packages; a bare server must not advertise them.
func TestPackagesToolsAbsentWithoutPackages(t *testing.T) {
	cs := newTestClient(t)
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
		if slices.Contains(names, want) {
			t.Errorf("tool %s advertised without Deps.Packages: %v", want, names)
		}
	}

	resources, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	for _, r := range resources.Resources {
		if r.URI == "bino://packages" {
			t.Error("bino://packages advertised without Deps.Packages")
		}
	}
}

// fakePackages is a Packages whose Add reports when it starts running.
type fakePackages struct{ started chan struct{} }

func (f *fakePackages) Packages(context.Context) ([]daemon.RegistryPackage, error) { return nil, nil }
func (f *fakePackages) Search(context.Context, registry.SearchParams) (registry.SearchResult, error) {
	return registry.SearchResult{}, nil
}
func (f *fakePackages) Info(context.Context, registry.Spec) (daemon.RegistryInfoResult, error) {
	return daemon.RegistryInfoResult{}, nil
}
func (f *fakePackages) AuthStatus(context.Context) (RegistryAuthStatus, error) {
	return RegistryAuthStatus{}, nil
}
func (f *fakePackages) Add(context.Context, []string) (RegistryMutationResult, error) {
	close(f.started)
	return RegistryMutationResult{Changes: []RegistryChange{{Name: "@acme/x", After: "1.0.0"}}}, nil
}
func (f *fakePackages) Update(context.Context, []string) (RegistryMutationResult, error) {
	return RegistryMutationResult{}, nil
}
func (f *fakePackages) Remove(context.Context, []string) (RegistryMutationResult, error) {
	return RegistryMutationResult{}, nil
}
func (f *fakePackages) Install(context.Context) (RegistryMutationResult, error) {
	return RegistryMutationResult{}, nil
}

// A registry write shares buildMu with the build tool, so it cannot start
// while a build holds the mutex (and vice versa); on success it fires
// Deps.RegistryChanged. The SDK runs every request in its own goroutine, so
// the CallTool below really runs concurrently with the test holding the lock.
func TestRegistryWriteWaitsForBuildAndNotifies(t *testing.T) {
	ctx := context.Background()
	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakePackages{started: make(chan struct{})}
	var notified atomic.Int32
	server := NewServer(Deps{State: newTestState(t, root), Packages: fake, RegistryChanged: func() { notified.Add(1) }})

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

	// This is what runBuild holds for the whole `bino build` subprocess.
	buildMu.Lock()
	results := make(chan *mcpsdk.CallToolResult, 1)
	go func() {
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "registry_add", Arguments: map[string]any{"specs": []string{"@acme/x"}}})
		if err != nil {
			t.Errorf("call registry_add: %v", err)
		}
		results <- res
	}()

	select {
	case <-fake.started:
		buildMu.Unlock()
		t.Fatal("registry_add ran while the build mutex was held")
	case <-time.After(200 * time.Millisecond):
	}
	if n := notified.Load(); n != 0 {
		buildMu.Unlock()
		t.Fatalf("RegistryChanged fired %d times before the write ran", n)
	}
	buildMu.Unlock()

	select {
	case <-fake.started:
	case <-time.After(2 * time.Second):
		t.Fatal("registry_add did not run after the build mutex was released")
	}
	var res *mcpsdk.CallToolResult
	select {
	case res = <-results:
	case <-time.After(2 * time.Second):
		t.Fatal("registry_add did not return")
	}
	if res == nil || res.IsError {
		t.Fatalf("registry_add failed: %+v", res)
	}
	if n := notified.Load(); n != 1 {
		t.Errorf("RegistryChanged fired %d times, want 1", n)
	}
}
