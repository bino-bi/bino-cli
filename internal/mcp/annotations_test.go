package mcp

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/logx"
)

// readTools are the tools that change nothing, so they carry ReadOnlyHint.
var readTools = []string{
	"list_kinds", "describe_kind", "describe_project", "describe_document", "validate_project",
	"get_columns", "get_rows", "graph_deps", "validate_draft", "introspect_source",
	"outline_kind", "scaffold_kind", "registry_packages", "registry_search", "registry_info",
	"registry_auth_status", "registry_publish_check",
}

func fullDeps(t *testing.T) Deps {
	t.Helper()
	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	return Deps{
		State:        newTestState(t, root),
		Authoring:    fakeAuthoring{},
		Packages:     &fakePackages{},
		AllowPublish: true,
		Preview:      daemon.NewPreviewProcess(root, logx.Nop()),
	}
}

func TestEveryToolHasTitleAndHints(t *testing.T) {
	tools, err := ListTools(context.Background(), NewServer(fullDeps(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) < 33 {
		t.Fatalf("only %d tools listed", len(tools))
	}
	for _, tool := range tools {
		if tool.Title == "" || tool.Annotations == nil || tool.Annotations.Title != tool.Title {
			t.Errorf("%s: missing title or annotations", tool.Name)
			continue
		}
		ro := slices.Contains(readTools, tool.Name)
		if tool.Annotations.ReadOnlyHint != ro {
			t.Errorf("%s: ReadOnlyHint=%v, want %v", tool.Name, tool.Annotations.ReadOnlyHint, ro)
		}
		if !ro && tool.Annotations.DestructiveHint == nil {
			t.Errorf("%s: write tool without DestructiveHint", tool.Name)
		}
	}
}

func TestReadOnlyServesOnlyReadTools(t *testing.T) {
	deps := fullDeps(t)
	deps.ReadOnly = true
	tools, err := ListTools(context.Background(), NewServer(deps))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only server lists %s", tool.Name)
		}
	}
	for _, want := range []string{"list_kinds", "describe_kind", "outline_kind", "describe_project", "describe_document", "validate_project", "validate_draft", "get_columns", "get_rows", "graph_deps"} {
		if !slices.Contains(names, want) {
			t.Errorf("read-only server misses %s", want)
		}
	}
	for _, gone := range []string{"introspect_source", "registry_search", "registry_publish_check", "build", "get_layout_state"} {
		if slices.Contains(names, gone) {
			t.Errorf("read-only server lists %s", gone)
		}
	}
}

type fakeAuthoring struct{ Authoring }
