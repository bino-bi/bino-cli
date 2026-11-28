package datasource

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestAccessFor(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "parts"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := func(name, spec string) config.Document {
		return config.Document{Kind: "DataSource", Name: name, File: filepath.Join(root, "sources.yaml"), Raw: []byte(`{"spec":` + spec + `}`)}
	}
	acc := AccessFor([]config.Document{
		doc("file", `{"type":"csv","path":"./data/sales.csv"}`),
		doc("dir", `{"type":"parquet","path":"data/parts"}`),
		doc("glob", `{"type":"csv","path":"data/parts/**/*.csv"}`),
		doc("url", `{"type":"csv","path":"https://example.com/exports/2024/*.csv"}`),
		doc("s3", `{"type":"parquet","path":"s3://bucket/sales.parquet"}`),
		doc("inline", `{"type":"inline","inline":{"content":[{"a":1}]}}`),
		{Kind: "DataSet", Name: "ds", Raw: []byte(`{"spec":{"query":"SELECT 1"}}`)},
	})
	base := filepath.ToSlash(root)
	for _, want := range []string{base + "/data/", base + "/data/parts/", "https://example.com/exports/2024/", "s3://bucket/"} {
		if !slices.Contains(acc.Prefixes, want) {
			t.Errorf("prefixes %v miss %s", acc.Prefixes, want)
		}
	}
	if !acc.Remote {
		t.Error("Remote = false with URL sources")
	}
	if AccessFor([]config.Document{doc("file", `{"type":"csv","path":"./data/sales.csv"}`)}).Remote {
		t.Error("Remote = true without URL sources")
	}
}
