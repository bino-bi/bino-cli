package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/schema"
)

// pluginKinds stands in for a plugin registry with a merged-schema validator.
type pluginKinds struct{ v *schema.Validator }

func (pluginKinds) PluginKindNames() []string { return []string{"Gauge"} }

func (pluginKinds) GetKind(name string) (config.KindInfo, bool) {
	return config.KindInfo{KindName: name, Category: config.KindCategoryComponent}, name == "Gauge"
}

func (p pluginKinds) DocumentValidator(context.Context) (*schema.Validator, error) { return p.v, nil }

func TestWriteManifestUsesPluginValidator(t *testing.T) {
	page := "apiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: page\nspec:\n  children:\n    - kind: Gauge\n      spec:\n        title: x\n"
	in := mcp.WriteManifestInput{File: "page.yaml", YAML: page}

	if _, err := newCLIAuthoring(t.TempDir(), nil).WriteManifest(context.Background(), in); err == nil {
		t.Fatal("without plugins a Gauge child must be rejected")
	}

	v, err := schema.CompileValidator([]byte(`{"type":"object"}`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, err := newCLIAuthoring(root, pluginKinds{v}).WriteManifest(context.Background(), in); err != nil {
		t.Fatalf("with the plugin validator the page must be written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "page.yaml")); err != nil {
		t.Errorf("page not written: %v", err)
	}
}
