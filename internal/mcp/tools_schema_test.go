package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/plugin"
)

// outlineSizeCeiling bounds the JSON size of every built-in outline. It guards
// the projection against a schema change that re-inflates it: describe_kind is
// ~88 KB for LayoutPage, the outline ~9 KB. The largest outlines today are
// Table and ChartBubble at ~14 KB, most of it the inline dataset spec under
// dataset.* and full field descriptions.
const outlineSizeCeiling = 16 * 1024

// kindNames returns every kind list_kinds reports.
func kindNames(t *testing.T, cs *mcpsdk.ClientSession) []string {
	t.Helper()
	var out listKindsOutput
	callToolJSON(t, cs, "list_kinds", map[string]any{}, &out)
	names := make([]string, 0, len(out.Kinds))
	for _, k := range out.Kinds {
		names = append(names, k.Name)
	}
	if len(names) == 0 {
		t.Fatal("list_kinds returned no kinds")
	}
	return names
}

func TestScaffoldKindEveryKind(t *testing.T) {
	cs := newTestClient(t)
	for _, k := range kindNames(t, cs) {
		var out scaffoldKindOutput
		callToolJSON(t, cs, "scaffold_kind", map[string]any{"kind": k}, &out)
		if !out.Found {
			t.Errorf("scaffold_kind(%s): not found", k)
			continue
		}
		if out.Description == "" {
			t.Errorf("scaffold_kind(%s): empty description", k)
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(out.YAML), &doc); err != nil {
			t.Errorf("scaffold_kind(%s): yaml does not parse: %v\n%s", k, err, out.YAML)
			continue
		}
		if doc["kind"] != k {
			t.Errorf("scaffold_kind(%s): kind = %v", k, doc["kind"])
		}
		if doc["apiVersion"] != "bino.bi/v1alpha1" {
			t.Errorf("scaffold_kind(%s): apiVersion = %v", k, doc["apiVersion"])
		}
		spec, _ := doc["spec"].(map[string]any)
		for _, r := range out.Required {
			if _, ok := spec[r]; !ok {
				t.Errorf("scaffold_kind(%s): required field %q missing from spec:\n%s", k, r, out.YAML)
			}
		}
	}
}

func TestOutlineKindEveryKind(t *testing.T) {
	cs := newTestClient(t)
	for _, k := range kindNames(t, cs) {
		var out outlineKindOutput
		callToolJSON(t, cs, "outline_kind", map[string]any{"kind": k}, &out)
		if !out.Found {
			t.Errorf("outline_kind(%s): not found", k)
			continue
		}
		if out.Description == "" {
			t.Errorf("outline_kind(%s): empty description", k)
		}
		if len(out.Fields) == 0 {
			t.Errorf("outline_kind(%s): no fields", k)
			continue
		}
		byPath := map[string]outlineField{}
		for i, f := range out.Fields {
			if i > 0 && out.Fields[i-1].Path >= f.Path {
				t.Errorf("outline_kind(%s): fields not strictly sorted at %q", k, f.Path)
			}
			if d := strings.Count(f.Path, ".") + strings.Count(f.Path, "[]") + 1; d > outlineMaxDepth {
				t.Errorf("outline_kind(%s): %q exceeds depth %d", k, f.Path, outlineMaxDepth)
			}
			byPath[f.Path] = f
		}
		for _, r := range out.Required {
			f, ok := byPath[r]
			if !ok {
				t.Errorf("outline_kind(%s): required %q is not a top-level field", k, r)
			} else if !f.Required {
				t.Errorf("outline_kind(%s): field %q not flagged required", k, r)
			}
		}
	}
}

func TestOutlineKindLayoutChildren(t *testing.T) {
	cs := newTestClient(t)
	cases := []struct {
		kind, slot string
		wantText   bool
	}{
		{"LayoutPage", "children", true},
		{"LayoutCard", "children", true},
		{"Grid", "children", true},
		{"Tree", "nodes", false},
	}
	for _, c := range cases {
		var out outlineKindOutput
		callToolJSON(t, cs, "outline_kind", map[string]any{"kind": c.kind}, &out)
		if !slices.Contains(out.ChildKinds, "Table") {
			t.Errorf("%s: childKinds %v lacks Table", c.kind, out.ChildKinds)
		}
		if slices.Contains(out.ChildKinds, "Text") != c.wantText {
			t.Errorf("%s: childKinds %v, Text presence want %v", c.kind, out.ChildKinds, c.wantText)
		}
		paths := fieldPaths(out)
		if !slices.Contains(paths, c.slot+"[].kind") || !slices.Contains(paths, c.slot+"[].spec") {
			t.Errorf("%s: slot keys missing from %v", c.kind, paths)
		}
		for _, p := range paths {
			if strings.HasPrefix(p, c.slot+"[].spec.") {
				t.Errorf("%s: child spec expanded: %q", c.kind, p)
			}
		}
	}

	var tree outlineKindOutput
	callToolJSON(t, cs, "outline_kind", map[string]any{"kind": "Tree"}, &tree)
	paths := fieldPaths(tree)
	if !slices.Contains(paths, "edges[].style") || slices.Contains(paths, "edges[].style.color") {
		t.Errorf("Tree: depth cap must keep edges[].style and drop edges[].style.color: %v", paths)
	}

	var table outlineKindOutput
	callToolJSON(t, cs, "outline_kind", map[string]any{"kind": "Table"}, &table)
	if paths := fieldPaths(table); !slices.Contains(paths, "attributes[].label") {
		t.Errorf("Table: attributes[].label missing: %v", paths)
	}
	if table.ChildKinds != nil {
		t.Errorf("Table: unexpected childKinds %v", table.ChildKinds)
	}

	var shot outlineKindOutput
	callToolJSON(t, cs, "outline_kind", map[string]any{"kind": "ScreenshotArtefact"}, &shot)
	if shot.ChildKinds != nil {
		t.Errorf("ScreenshotArtefact: refs[] is not a child slot, got childKinds %v", shot.ChildKinds)
	}
}

func TestOutlineKindSizeCeiling(t *testing.T) {
	cs := newTestClient(t)
	for _, k := range kindNames(t, cs) {
		var out outlineKindOutput
		callToolJSON(t, cs, "outline_kind", map[string]any{"kind": k}, &out)
		b, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) >= outlineSizeCeiling {
			t.Errorf("outline_kind(%s): %d bytes, ceiling %d", k, len(b), outlineSizeCeiling)
		}
		if k == "LayoutPage" || k == "Table" {
			t.Logf("outline_kind(%s): %d bytes, %d fields", k, len(b), len(out.Fields))
		}
	}
}

func TestOutlineKindUnknown(t *testing.T) {
	cs := newTestClient(t)
	var out outlineKindOutput
	callToolJSON(t, cs, "outline_kind", map[string]any{"kind": "NotAKind"}, &out)
	if out.Found || len(out.Fields) != 0 || out.ChildKinds != nil {
		t.Errorf("outline_kind(NotAKind) = %+v, want found=false and no fields", out)
	}
	var sc scaffoldKindOutput
	callToolJSON(t, cs, "scaffold_kind", map[string]any{"kind": "NotAKind"}, &sc)
	if sc.Found || sc.YAML != "" {
		t.Errorf("scaffold_kind(NotAKind) = %+v, want found=false and no yaml", sc)
	}
}

func TestOutlineKindPluginKind(t *testing.T) {
	reg := plugin.NewRegistry()
	reg.Register(&stubPlugin{})
	cs := newTestClientWithRegistry(t, reg)

	if !slices.Contains(kindNames(t, cs), "AcmeSource") {
		t.Fatal("list_kinds: plugin kind AcmeSource missing")
	}

	var out outlineKindOutput
	callToolJSON(t, cs, "outline_kind", map[string]any{"kind": "AcmeSource"}, &out)
	if !out.Found || out.Description != "Acme source." {
		t.Fatalf("outline_kind(AcmeSource) = %+v", out)
	}
	if !slices.Equal(out.Required, []string{"endpoint"}) {
		t.Errorf("required = %v, want [endpoint]", out.Required)
	}
	got := map[string]outlineField{}
	for _, f := range out.Fields {
		got[f.Path] = f
	}
	if f := got["endpoint"]; !f.Required || f.Type != "string" || f.Description != "Base URL." {
		t.Errorf("endpoint = %+v", f)
	}
	if f := got["options.retries"]; f.Type != "integer" || f.Default != float64(3) {
		t.Errorf("options.retries = %+v", f)
	}
	if f, ok := got["rows[].name"]; !ok || f.Type != "string" {
		t.Errorf("rows[].name = %+v (present %v)", f, ok)
	}

	var sc scaffoldKindOutput
	callToolJSON(t, cs, "scaffold_kind", map[string]any{"kind": "AcmeSource"}, &sc)
	if !sc.Found || !strings.Contains(sc.YAML, "\n  endpoint:") {
		t.Errorf("scaffold_kind(AcmeSource) = %+v", sc)
	}
}

func fieldPaths(out outlineKindOutput) []string {
	paths := make([]string, 0, len(out.Fields))
	for _, f := range out.Fields {
		paths = append(paths, f.Path)
	}
	return paths
}

// stubPlugin provides one data-source kind with a nested spec schema.
type stubPlugin struct{}

func (*stubPlugin) Manifest() plugin.PluginManifest {
	return plugin.PluginManifest{
		Name:  "acme",
		Kinds: []plugin.KindRegistration{{KindName: "AcmeSource", Category: plugin.KindCategoryDataSource}},
	}
}

func (*stubPlugin) GetSchemas(context.Context) (map[string][]byte, error) {
	return map[string][]byte{"AcmeSource": []byte(`{
		"type": "object",
		"description": "Acme source.",
		"required": ["endpoint"],
		"properties": {
			"endpoint": {"type": "string", "description": "Base URL."},
			"options": {"type": "object", "properties": {"retries": {"type": "integer", "default": 3}}},
			"rows": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}}}}
		}
	}`)}, nil
}

func (*stubPlugin) CollectDataSource(context.Context, string, []byte, map[string]string, string) (*plugin.CollectResult, error) {
	return nil, nil
}

func (*stubPlugin) Lint(context.Context, []plugin.DocumentPayload, *plugin.LintOptions) ([]plugin.LintFinding, error) {
	return nil, nil
}

func (*stubPlugin) GetAssets(context.Context, string) ([]plugin.AssetFile, []plugin.AssetFile, error) {
	return nil, nil, nil
}

func (*stubPlugin) ListCommands(context.Context) ([]plugin.CommandDescriptor, error) {
	return nil, nil
}

func (*stubPlugin) ExecCommand(context.Context, string, []string, map[string]string, string, func([]byte, []byte)) (int, error) {
	return 0, nil
}

func (*stubPlugin) OnHook(context.Context, string, *plugin.HookPayload) (*plugin.HookResult, error) {
	return nil, nil
}

func (*stubPlugin) RenderComponent(context.Context, string, string, []byte, string) (string, error) {
	return "", nil
}

func (*stubPlugin) Shutdown(context.Context) error { return nil }
