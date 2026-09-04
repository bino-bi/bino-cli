package mcp

import (
	"context"
	"slices"
	"testing"
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
	for _, want := range []string{"registry_packages", "registry_search", "registry_info", "registry_auth_status"} {
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
