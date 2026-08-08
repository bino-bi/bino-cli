package plugin

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
)

type failingSchemaPlugin struct{ *mockPlugin }

func (f *failingSchemaPlugin) GetSchemas(context.Context) (map[string][]byte, error) {
	return nil, errors.New("rpc broke")
}

type kindsPlugin struct {
	*mockPlugin
	kinds map[string][]byte
}

func (p *kindsPlugin) GetSchemas(context.Context) (map[string][]byte, error) {
	return p.kinds, nil
}

type failingAssetsPlugin struct{ *mockPlugin }

func (f *failingAssetsPlugin) GetAssets(context.Context, string) ([]AssetFile, []AssetFile, error) {
	return nil, nil, errors.New("rpc broke")
}

type assetsPlugin struct {
	*mockPlugin
	scripts []AssetFile
}

func (p *assetsPlugin) GetAssets(context.Context, string) ([]AssetFile, []AssetFile, error) {
	return p.scripts, nil, nil
}

func captureContext() (context.Context, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	log := logx.NewTerminalWithColor(buf, buf, false, true)
	return logx.WithLogger(context.Background(), log), buf
}

// Regression: a plugin whose GetSchemas RPC failed was skipped with no log at
// any level — the user's manifests then failed validation pointing at their
// YAML instead of the plugin. The failure must be logged with the plugin
// name, and healthy plugins must still merge.
func TestSchemaAggregatorLogsPluginFailure(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&failingSchemaPlugin{newMock("broken")})
	reg.Register(&kindsPlugin{newMock("healthy"), map[string][]byte{
		"CustomKind": []byte(`{"type":"object"}`),
	}})

	ctx, buf := captureContext()
	agg := NewSchemaAggregator(reg)
	if err := agg.Build(ctx); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := agg.SchemaForKind("CustomKind"); !ok {
		t.Error("healthy plugin's kind missing from the merged schema")
	}
	logged := buf.String()
	if !strings.Contains(logged, "broken") || !strings.Contains(logged, "rpc broke") {
		t.Errorf("schema RPC failure not logged with plugin name and cause; logged: %q", logged)
	}
}

// Same for assets: a failing GetAssets silently dropped the plugin's CSS/JS
// from the rendered report with no diagnostic anywhere.
func TestAssetCacheLogsPluginFailure(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&failingAssetsPlugin{newMock("broken", withAssets())})
	reg.Register(&assetsPlugin{newMock("healthy", withAssets()), []AssetFile{
		{URLPath: "/plugin/healthy/app.js"},
	}})

	ctx, buf := captureContext()
	cache := BuildAssetCache(ctx, reg, "preview")
	if _, ok := cache.Get("/plugin/healthy/app.js"); !ok {
		t.Error("healthy plugin's asset missing from the cache")
	}
	logged := buf.String()
	if !strings.Contains(logged, "broken") || !strings.Contains(logged, "rpc broke") {
		t.Errorf("asset RPC failure not logged with plugin name and cause; logged: %q", logged)
	}
}
