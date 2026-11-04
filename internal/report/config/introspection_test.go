package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	syntheticSales = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==synthetic]
spec:
  type: csv
  path: ./synthetic.csv
`
	realSales = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==real]
spec:
  type: csv
  path: ./real.csv
`
)

func artefactYAML(name, data string) string {
	return `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: ` + name + `
  labels:
    data: ` + data + `
spec:
  filename: ` + name + `.pdf
  title: Report
`
}

func loadFixture(t *testing.T, files map[string]string) []Document {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := LoadDirWithOptions(context.Background(), dir, LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func dataSourceFiles(docs []Document, name string) []string {
	var files []string
	for _, d := range docs {
		if d.Kind == "DataSource" && d.Name == name {
			files = append(files, filepath.Base(d.File))
		}
	}
	return files
}

func TestResolveForIntrospection(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string
		wantFiles []string
		ambiguous bool
	}{
		{
			name:      "artefact selects the synthetic source",
			files:     map[string]string{"a.yaml": syntheticSales, "b.yaml": realSales, "r.yaml": artefactYAML("monthly", "synthetic")},
			wantFiles: []string{"a.yaml"},
		},
		{
			name:      "document order does not matter",
			files:     map[string]string{"a.yaml": realSales, "b.yaml": syntheticSales, "r.yaml": artefactYAML("monthly", "synthetic")},
			wantFiles: []string{"b.yaml"},
		},
		{
			name: "artefacts that agree resolve",
			files: map[string]string{"a.yaml": syntheticSales, "b.yaml": realSales,
				"r.yaml": artefactYAML("monthly", "synthetic"), "s.yaml": artefactYAML("weekly", "synthetic")},
			wantFiles: []string{"a.yaml"},
		},
		{
			name: "artefacts that disagree are ambiguous",
			files: map[string]string{"a.yaml": syntheticSales, "b.yaml": realSales,
				"r.yaml": artefactYAML("monthly", "synthetic"), "s.yaml": artefactYAML("weekly", "real")},
			ambiguous: true,
		},
		{
			name:      "no artefact selects either duplicate",
			files:     map[string]string{"a.yaml": syntheticSales, "b.yaml": realSales},
			ambiguous: true,
		},
		{
			name:      "a single definition is kept even if excluded",
			files:     map[string]string{"b.yaml": realSales, "r.yaml": artefactYAML("monthly", "synthetic")},
			wantFiles: []string{"b.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kept, ambiguous := ResolveForIntrospection(loadFixture(t, tt.files))
			got := dataSourceFiles(kept, "sales")
			if tt.ambiguous {
				if len(got) != 0 || len(ambiguous) != 1 {
					t.Fatalf("kept %v, ambiguities %v; want none kept and one ambiguity", got, ambiguous)
				}
				err := ambiguous.Lookup(kept, "$sales")
				if err == nil || !strings.Contains(err.Error(), `DataSource "sales" is defined 2 times`) {
					t.Fatalf("Lookup($sales) = %v", err)
				}
				return
			}
			if len(ambiguous) != 0 {
				t.Fatalf("unexpected ambiguities: %v", ambiguous)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFiles, ",") {
				t.Fatalf("kept sales from %v, want %v", got, tt.wantFiles)
			}
		})
	}
}

func TestResolveForIntrospectionKeepsNonDataKinds(t *testing.T) {
	page := func(lang string) string {
		return `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: cover
  constraints: [labels.data==` + lang + `]
spec:
  children: []
`
	}
	docs := loadFixture(t, map[string]string{"p1.yaml": page("synthetic"), "p2.yaml": page("real"),
		"r.yaml": artefactYAML("monthly", "synthetic"), "s.yaml": artefactYAML("weekly", "real")})
	pages := 0
	for _, d := range docs {
		if d.Kind == "LayoutPage" {
			pages++
		}
	}
	if pages != 2 {
		t.Fatalf("fixture loaded %d LayoutPages, want 2", pages)
	}
	kept, ambiguous := ResolveForIntrospection(docs)
	if len(kept) != len(docs) || len(ambiguous) != 0 {
		t.Fatalf("kept %d of %d documents, ambiguities %v; LayoutPages must pass unchanged", len(kept), len(docs), ambiguous)
	}
}

func TestAmbiguitiesLookupPrefersUniqueDataSet(t *testing.T) {
	docs := loadFixture(t, map[string]string{"a.yaml": syntheticSales, "b.yaml": realSales,
		"r.yaml": artefactYAML("monthly", "synthetic"), "s.yaml": artefactYAML("weekly", "real"),
		"ds.yaml": "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: sales\nspec:\n  query: SELECT 1 AS v\n"})
	kept, ambiguous := ResolveForIntrospection(docs)
	if err := ambiguous.Lookup(kept, "sales"); err != nil {
		t.Fatalf("bare name with a unique DataSet: %v", err)
	}
	if err := ambiguous.Lookup(kept, "$sales"); err == nil {
		t.Fatal("$sales must report the ambiguous DataSource")
	}
}

func TestAmbiguitiesExplainOnlyNamedSources(t *testing.T) {
	as := Ambiguities{{Kind: "DataSource", Name: "sales", Files: []string{"a #1", "b #1"}}}
	if got := as.Explain(errors.New("syntax error near SELEC")).Error(); strings.Contains(got, "sales") {
		t.Fatalf("unrelated error got the ambiguity: %q", got)
	}
	if got := as.Explain(errors.New("Table with name sales does not exist")).Error(); !strings.Contains(got, "defined 2 times") {
		t.Fatalf("missing-table error lacks the ambiguity: %q", got)
	}
}
