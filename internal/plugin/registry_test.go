package plugin

import (
	"context"
	"testing"
)

// mockPlugin implements Plugin for testing.
type mockPlugin struct {
	manifest PluginManifest
}

func (m *mockPlugin) Manifest() PluginManifest { return m.manifest }

func (m *mockPlugin) GetSchemas(context.Context) (map[string][]byte, error) {
	return nil, nil
}

func (m *mockPlugin) CollectDataSource(context.Context, string, []byte, map[string]string, string) (*CollectResult, error) {
	return nil, nil
}

func (m *mockPlugin) Lint(context.Context, []DocumentPayload, *LintOptions) ([]LintFinding, error) {
	return nil, nil
}

func (m *mockPlugin) GetAssets(context.Context, string) ([]AssetFile, []AssetFile, error) {
	return nil, nil, nil
}

func (m *mockPlugin) ListCommands(context.Context) ([]CommandDescriptor, error) {
	return nil, nil
}

func (m *mockPlugin) ExecCommand(context.Context, string, []string, map[string]string, string, func([]byte, []byte)) (int, error) {
	return 0, nil
}

func (m *mockPlugin) OnHook(context.Context, string, *HookPayload) (*HookResult, error) {
	return nil, nil
}

func (m *mockPlugin) RenderComponent(context.Context, string, string, []byte, string) (string, error) {
	return "", nil
}

func (m *mockPlugin) Shutdown(context.Context) error { return nil }

func newMock(name string, opts ...func(*PluginManifest)) *mockPlugin {
	m := &PluginManifest{Name: name}
	for _, o := range opts {
		o(m)
	}
	return &mockPlugin{manifest: *m}
}

func withKinds(kinds ...KindRegistration) func(*PluginManifest) {
	return func(m *PluginManifest) { m.Kinds = kinds }
}

func withLinter() func(*PluginManifest) {
	return func(m *PluginManifest) { m.ProvidesLinter = true }
}

func withAssets() func(*PluginManifest) {
	return func(m *PluginManifest) { m.ProvidesAssets = true }
}

func withHooks(hooks ...string) func(*PluginManifest) {
	return func(m *PluginManifest) { m.Hooks = hooks }
}

func withDuckDBExtensions(exts ...string) func(*PluginManifest) {
	return func(m *PluginManifest) { m.DuckDBExtensions = exts }
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	p := newMock("alpha")
	r.Register(p)

	got, ok := r.Get("alpha")
	if !ok {
		t.Fatal("expected to find plugin 'alpha'")
	}
	if got.Manifest().Name != "alpha" {
		t.Fatalf("got name %q, want %q", got.Manifest().Name, "alpha")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Fatal("expected not to find 'nonexistent'")
	}
}

func TestRegistry_GetKind(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("sf", withKinds(
		KindRegistration{KindName: "SalesforceDataSource", Category: KindCategoryDataSource, DataSourceType: "sf_soql"},
		KindRegistration{KindName: "SalesforceConfig", Category: KindCategoryConfig},
	)))

	k, ok := r.GetKindRegistration("SalesforceDataSource")
	if !ok {
		t.Fatal("expected to find kind")
	}
	if k.PluginName != "sf" {
		t.Fatalf("got plugin name %q, want %q", k.PluginName, "sf")
	}
	if k.DataSourceType != "sf_soql" {
		t.Fatalf("got datasource type %q, want %q", k.DataSourceType, "sf_soql")
	}

	_, ok = r.GetKindRegistration("Unknown")
	if ok {
		t.Fatal("expected not to find 'Unknown'")
	}
}

func TestRegistry_DataSourcePlugin(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("sf", withKinds(
		KindRegistration{KindName: "SalesforceDataSource", Category: KindCategoryDataSource, DataSourceType: "sf_soql"},
		KindRegistration{KindName: "SalesforceConfig", Category: KindCategoryConfig},
	)))

	p, ok := r.DataSourcePlugin("SalesforceDataSource")
	if !ok || p.Manifest().Name != "sf" {
		t.Fatal("expected to find datasource plugin for SalesforceDataSource")
	}

	// Config kind is not a datasource.
	_, ok = r.DataSourcePlugin("SalesforceConfig")
	if ok {
		t.Fatal("SalesforceConfig should not be a datasource plugin")
	}

	// Unknown kind.
	_, ok = r.DataSourcePlugin("Unknown")
	if ok {
		t.Fatal("Unknown kind should not match")
	}
}

func TestRegistry_PluginsForHook(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("alpha", withHooks("post-load", "pre-render-html")))
	r.Register(newMock("beta", withHooks("post-load")))
	r.Register(newMock("gamma"))

	postLoad := r.PluginsForHook("post-load")
	if len(postLoad) != 2 {
		t.Fatalf("expected 2 plugins for post-load, got %d", len(postLoad))
	}
	if postLoad[0].Manifest().Name != "alpha" || postLoad[1].Manifest().Name != "beta" {
		t.Fatal("expected declaration order: alpha, beta")
	}

	preRender := r.PluginsForHook("pre-render-html")
	if len(preRender) != 1 || preRender[0].Manifest().Name != "alpha" {
		t.Fatal("expected only alpha for pre-render-html")
	}

	none := r.PluginsForHook("nonexistent")
	if len(none) != 0 {
		t.Fatal("expected no plugins for nonexistent hook")
	}
}

func TestRegistry_CategorizeKind(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("sf", withKinds(
		KindRegistration{KindName: "SalesforceDataSource", Category: KindCategoryDataSource},
	)))

	tests := []struct {
		kind string
		want KindCategory
	}{
		// Registry lookup.
		{"SalesforceDataSource", KindCategoryDataSource},
		// Suffix-based fallback.
		{"CustomDataSource", KindCategoryDataSource},
		{"ReportArtefact", KindCategoryArtifact},
		{"ReportArtifact", KindCategoryArtifact},
		// Default fallback.
		{"LayoutPage", KindCategoryComponent},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := r.CategorizeKind(tt.kind)
			if got != tt.want {
				t.Fatalf("CategorizeKind(%q) = %d, want %d", tt.kind, got, tt.want)
			}
		})
	}
}

func TestRegistry_DuckDBExtensions_Deduplication(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("alpha", withDuckDBExtensions("httpfs", "spatial")))
	r.Register(newMock("beta", withDuckDBExtensions("spatial", "json")))

	exts := r.DuckDBExtensions()
	if len(exts) != 3 {
		t.Fatalf("expected 3 extensions, got %d: %v", len(exts), exts)
	}
	// Order: httpfs, spatial (from alpha), json (from beta). spatial not duplicated.
	want := []string{"httpfs", "spatial", "json"}
	for i, w := range want {
		if exts[i] != w {
			t.Fatalf("exts[%d] = %q, want %q", i, exts[i], w)
		}
	}
}

func TestRegistry_DeclarationOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("charlie"))
	r.Register(newMock("alpha"))
	r.Register(newMock("bravo"))

	all := r.AllPlugins()
	if len(all) != 3 {
		t.Fatalf("expected 3 plugins, got %d", len(all))
	}
	names := []string{all[0].Manifest().Name, all[1].Manifest().Name, all[2].Manifest().Name}
	want := []string{"charlie", "alpha", "bravo"}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("AllPlugins()[%d] = %q, want %q", i, names[i], w)
		}
	}
}

func TestRegistry_PluginsWithLinter(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("alpha", withLinter()))
	r.Register(newMock("beta"))
	r.Register(newMock("gamma", withLinter()))

	linters := r.PluginsWithLinter()
	if len(linters) != 2 {
		t.Fatalf("expected 2 linter plugins, got %d", len(linters))
	}
	if linters[0].Manifest().Name != "alpha" || linters[1].Manifest().Name != "gamma" {
		t.Fatal("expected linter plugins in declaration order: alpha, gamma")
	}
}

func TestRegistry_PluginsWithAssets(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("alpha"))
	r.Register(newMock("beta", withAssets()))

	assets := r.PluginsWithAssets()
	if len(assets) != 1 || assets[0].Manifest().Name != "beta" {
		t.Fatal("expected only beta to provide assets")
	}
}

func TestRegistry_PluginKindNames(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("sf", withKinds(
		KindRegistration{KindName: "SfDataSource"},
		KindRegistration{KindName: "SfConfig"},
	)))

	names := r.PluginKindNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 kind names, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["SfDataSource"] || !nameSet["SfConfig"] {
		t.Fatalf("missing expected kind names in %v", names)
	}
}

func TestRegistry_AllKinds(t *testing.T) {
	r := NewRegistry()
	r.Register(newMock("alpha", withKinds(
		KindRegistration{KindName: "AlphaDS", Category: KindCategoryDataSource},
	)))
	r.Register(newMock("beta", withKinds(
		KindRegistration{KindName: "BetaCfg", Category: KindCategoryConfig},
	)))

	kinds := r.AllKinds()
	if len(kinds) != 2 {
		t.Fatalf("expected 2 kinds, got %d", len(kinds))
	}
}
