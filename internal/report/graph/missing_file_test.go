package graph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// bino build calls Build with every document, before constraints are applied.
// A file DataSource that the artefact excludes may legitimately not exist on
// this machine; Build must not fail on it.
func TestBuildToleratesMissingDataSourceFile(t *testing.T) {
	for _, path := range []string{"./secure/sales.csv", "./secure/*.csv"} {
		t.Run(path, func(t *testing.T) {
			dir := t.TempDir()
			doc := makeDocAt("DataSource", "sales", filepath.Join(dir, "sources.yaml"), json.RawMessage(`{
				"apiVersion": "bino.bi/v1alpha1",
				"kind": "DataSource",
				"metadata": {"name": "sales", "constraints": ["labels.data==real"]},
				"spec": {"type": "csv", "path": "`+path+`"}
			}`))
			g, err := Build(context.Background(), []config.Document{doc}, nil)
			if err != nil {
				t.Fatalf("Build failed on a missing file of a DataSource: %v", err)
			}
			node := g.NodeByManifest("DataSource", "sales")
			if node == nil {
				t.Fatal("DataSource node missing from the graph")
			}
			if got := node.Attributes["sourcesMissing"]; got != path {
				t.Fatalf("sourcesMissing = %q, want %q", got, path)
			}
		})
	}
}

func TestBuildMissingFileHashFollowsPath(t *testing.T) {
	dir := t.TempDir()
	hash := func(path string) string {
		doc := makeDocAt("DataSource", "sales", filepath.Join(dir, "sources.yaml"), json.RawMessage(`{"spec": {"type": "csv", "path": "`+path+`"}}`))
		g, err := Build(context.Background(), []config.Document{doc}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return g.NodeByManifest("DataSource", "sales").Hash
	}
	if a, b := hash("./a.csv"), hash("./a.csv"); a != b {
		t.Fatalf("hash of a missing file is not stable: %s != %s", a, b)
	}
	if hash("./a.csv") == hash("./b.csv") {
		t.Fatal("hash does not change with the missing path")
	}
}

func TestBuildStillFailsOnUnreadableDataSourceFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads files regardless of permissions")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "locked"), 0o755) })
	doc := makeDocAt("DataSource", "sales", filepath.Join(dir, "sources.yaml"), json.RawMessage(`{"spec": {"type": "csv", "path": "./locked/sales.csv"}}`))
	if _, err := Build(context.Background(), []config.Document{doc}, nil); err == nil {
		t.Fatal("Build ignored a permission error")
	}
}
