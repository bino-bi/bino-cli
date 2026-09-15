package lint

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// dependencyDocs returns the DataSources the dependency rule tests read, plus
// the given DataSet named sales.
func dependencyDocs(t *testing.T, labels map[string]string, specData map[string]any) []Document {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "reads.sql"), []byte("SELECT * FROM sales_csv"), 0o644); err != nil {
		t.Fatalf("write query file: %v", err)
	}

	docs := make([]Document, 0, 5)
	for _, name := range []string{"sales_csv", "regions", "actuals_csv"} {
		docs = append(docs, Document{
			File: filepath.Join(dir, "sources.yaml"), Position: len(docs) + 1, Kind: "DataSource", Name: name,
			Raw: rawDoc("DataSource", name, map[string]any{"type": "csv", "path": "./" + name + ".csv"}),
		})
	}
	docs = append(docs,
		Document{
			File: filepath.Join(dir, "sources.yaml"), Position: -1, Kind: "DataSource", Name: "_inline_datasource_abc",
			Labels: map[string]string{"bino.bi/generated": "true"},
			Raw:    rawDoc("DataSource", "_inline_datasource_abc", map[string]any{"type": "csv", "path": "./inline.csv"}),
		},
		Document{
			File: filepath.Join(dir, "dataset.yaml"), Position: 1, Kind: "DataSet", Name: "sales", Labels: labels,
			Raw: rawDoc("DataSet", "sales", specData),
		},
	)
	return docs
}

func TestDatasetDependencyUndeclared(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		labels map[string]string
		spec   map[string]any
		want   []string
	}{
		{
			name: "plain query",
			spec: map[string]any{"query": "SELECT * FROM sales_csv JOIN regions USING (region)", "dependencies": []string{"regions"}},
			want: []string{"sales_csv"},
		},
		{
			name: "file query",
			spec: map[string]any{"query": map[string]any{"$file": "reads.sql"}},
			want: []string{"sales_csv"},
		},
		{
			name: "read spelled in upper case",
			spec: map[string]any{"query": "SELECT * FROM SALES_CSV"},
			want: []string{"sales_csv"},
		},
		{
			name: "dependency spelled in upper case",
			spec: map[string]any{"query": "SELECT * FROM sales_csv", "dependencies": []string{"SALES_CSV"}},
			want: []string{"sales_csv"},
		},
		{
			name: "cte reading a source",
			spec: map[string]any{"query": "WITH s AS (SELECT * FROM sales_csv) SELECT * FROM s"},
			want: []string{"sales_csv"},
		},
		{
			name: "shift macro",
			spec: map[string]any{"query": "SELECT * FROM bino_shift('actuals_csv', 'ac1', '1 year', 'month')"},
			want: []string{"actuals_csv"},
		},
		{
			name: "declared read",
			spec: map[string]any{"query": "SELECT * FROM sales_csv", "dependencies": []string{"sales_csv"}},
		},
		{
			name: "inline reference",
			spec: map[string]any{"query": "SELECT * FROM @inline(0)", "dependencies": []string{"_inline_datasource_abc"}},
		},
		{
			name: "read that is not a DataSource",
			spec: map[string]any{"query": "SELECT * FROM other_dataset"},
		},
		{
			name: "source pass-through",
			spec: map[string]any{"source": "sales_csv"},
		},
		{
			name: "prql",
			spec: map[string]any{"prql": "from sales_csv"},
		},
		{
			name: "unparseable query",
			spec: map[string]any{"query": "SELEC * FROM sales_csv"},
		},
		{
			name:   "generated dataset",
			labels: map[string]string{"bino.bi/generated": "true"},
			spec:   map[string]any{"query": "SELECT * FROM sales_csv"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			findings := datasetDependencyUndeclared.Check(context.Background(), dependencyDocs(t, tt.labels, tt.spec))

			var got []string
			for _, f := range findings {
				if f.RuleID != "dataset-dependency-undeclared" || f.Path != "spec.query" || f.DocIdx != 1 || f.Severity != "" {
					t.Errorf("unexpected finding %+v", f)
				}
				got = append(got, f.Message)
			}
			var wantMessages []string
			for _, name := range tt.want {
				wantMessages = append(wantMessages, `query reads DataSource "`+name+`" but spec.dependencies does not list it; the dataset cache will not notice when it changes`)
			}
			if !slices.Equal(got, wantMessages) {
				t.Errorf("messages = %q, want %q", got, wantMessages)
			}
		})
	}
}

func TestDatasetDependencyUnused(t *testing.T) {
	t.Parallel()

	t.Run("unused entry is reported with its index", func(t *testing.T) {
		t.Parallel()

		docs := dependencyDocs(t, nil, map[string]any{
			"query":        "SELECT * FROM sales_csv",
			"dependencies": []string{"sales_csv", "regions"},
		})
		findings := datasetDependencyUnused.Check(context.Background(), docs)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		f := findings[0]
		if f.RuleID != "dataset-dependency-unused" || f.Path != "spec.dependencies.1" || f.Severity != "info" || f.DocIdx != 1 {
			t.Errorf("unexpected finding %+v", f)
		}
		if want := `spec.dependencies lists "regions" but the query never reads it`; f.Message != want {
			t.Errorf("message = %q, want %q", f.Message, want)
		}
	})

	tests := []struct {
		name string
		spec map[string]any
	}{
		{
			name: "inline reference",
			spec: map[string]any{"query": "SELECT * FROM @inline(0)", "dependencies": []string{"_inline_datasource_abc"}},
		},
		{
			name: "shift macro",
			spec: map[string]any{"query": "SELECT * FROM bino_shift('actuals_csv', 'ac1', '1 year', 'month')", "dependencies": []string{"actuals_csv"}},
		},
		{
			name: "cte named like the source it reads",
			spec: map[string]any{"query": "WITH sales_csv AS (SELECT * FROM main.sales_csv) SELECT * FROM sales_csv", "dependencies": []string{"sales_csv"}},
		},
		{
			name: "dependency spelled in upper case",
			spec: map[string]any{"query": "SELECT * FROM sales_csv", "dependencies": []string{"SALES_CSV"}},
		},
		{
			name: "dependency that is not a DataSource",
			spec: map[string]any{"query": "SELECT 1", "dependencies": []string{"other_dataset"}},
		},
		{
			name: "unparseable query",
			spec: map[string]any{"query": "SELEC * FROM sales_csv", "dependencies": []string{"regions"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if findings := datasetDependencyUnused.Check(context.Background(), dependencyDocs(t, nil, tt.spec)); len(findings) != 0 {
				t.Errorf("expected no findings, got %+v", findings)
			}
		})
	}
}
