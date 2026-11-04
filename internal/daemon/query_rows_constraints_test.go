package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/pkg/duckdb"
)

// Two DataSources share the name "sales"; the artefact label selects one. The
// introspection tools must answer from that one, never from the excluded one.

const (
	syntheticSource = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==synthetic]
spec:
  type: csv
  path: ./synthetic.csv
`
	realSource = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==real]
spec:
  type: csv
  path: ./real.csv
`
	revenueDataSet = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: revenue
spec:
  query: SELECT v FROM sales
`
)

func reportArtefact(name, data string) string {
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

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func newRefreshedState(t *testing.T, root string) *State {
	t.Helper()
	opts, err := duckdb.DefaultOptions()
	if err != nil {
		t.Fatal(err)
	}
	session, err := duckdb.OpenSession(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	st, err := NewState(root, session, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestQueryRowsHonoursArtefactConstraints(t *testing.T) {
	for _, tt := range []struct {
		name, first, second, wantFile string
	}{
		{"synthetic first", syntheticSource, realSource, "a.yaml"},
		{"synthetic last", realSource, syntheticSource, "b.yaml"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, map[string]string{
				"synthetic.csv": "v\n1\n",
				"real.csv":      "v,real_only\n999,1\n",
				"a.yaml":        tt.first,
				"b.yaml":        tt.second,
				"dataset.yaml":  revenueDataSet,
				"report.yaml":   reportArtefact("monthly", "synthetic"),
			})
			st := newRefreshedState(t, root)
			ctx := context.Background()

			res := st.Rows(ctx, "$sales", 10)
			if res.Error != "" {
				t.Fatalf("rows error: %s", res.Error)
			}
			if len(res.Rows) != 1 || res.Rows[0]["v"] != int64(1) {
				t.Fatalf("got rows %v from the constraint-excluded source, want the synthetic row v=1", res.Rows)
			}
			if filepath.Base(res.File) != tt.wantFile {
				t.Errorf("rows file = %q, want %s", res.File, tt.wantFile)
			}

			ds := st.Rows(ctx, "revenue", 10)
			if ds.Error != "" || len(ds.Rows) != 1 || ds.Rows[0]["v"] != int64(1) {
				t.Fatalf("dataset rows = %v (error %q), want v=1 from the synthetic source", ds.Rows, ds.Error)
			}

			cols := st.Columns(ctx, "$sales")
			if cols.Error != "" || strings.Join(cols.Columns, ",") != "v" {
				t.Fatalf("columns = %v (error %q), want [v] from the synthetic source", cols.Columns, cols.Error)
			}
			if filepath.Base(cols.File) != tt.wantFile {
				t.Errorf("columns file = %q, want %s", cols.File, tt.wantFile)
			}
		})
	}
}

func TestQueryRowsRefusesAmbiguousSource(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"synthetic.csv": "v\n1\n",
		"real.csv":      "v\n999\n",
		"a.yaml":        syntheticSource,
		"b.yaml":        realSource,
		"dataset.yaml":  revenueDataSet,
		"report.yaml":   reportArtefact("monthly", "synthetic"),
	})
	st := newRefreshedState(t, root)
	ctx := context.Background()
	if res := st.Rows(ctx, "$sales", 10); res.Error != "" {
		t.Fatalf("before the second artefact: %s", res.Error)
	}

	// A second artefact selects the real source: the tools cannot know which
	// one is meant, and the view from the first refresh must not survive.
	writeFiles(t, root, map[string]string{"weekly.yaml": reportArtefact("weekly", "real")})
	if err := st.Refresh(ctx); err != nil {
		t.Fatal(err)
	}

	res := st.Rows(ctx, "$sales", 10)
	if len(res.Rows) != 0 || !strings.Contains(res.Error, "do not select exactly one") {
		t.Fatalf("rows = %v, error %q; want no rows and an ambiguity error", res.Rows, res.Error)
	}
	ds := st.Rows(ctx, "revenue", 10)
	if len(ds.Rows) != 0 || !strings.Contains(ds.Error, `DataSource "sales" is defined 2 times`) {
		t.Fatalf("dataset rows = %v, error %q; want no rows and the ambiguity named", ds.Rows, ds.Error)
	}
	if cols := st.Columns(ctx, "$sales"); cols.Error == "" {
		t.Fatalf("columns = %v, want an ambiguity error", cols.Columns)
	}
}

func TestQueryRowsDropsViewWhenNewSelectionFails(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"synthetic.csv": "v\n1\n",
		"a.yaml":        syntheticSource,
		"b.yaml":        realSource,
		"report.yaml":   reportArtefact("monthly", "synthetic"),
	})
	st := newRefreshedState(t, root)
	ctx := context.Background()
	if res := st.Rows(ctx, "$sales", 10); res.Error != "" {
		t.Fatalf("synthetic selection: %s", res.Error)
	}

	// The label now selects the real source, whose file is missing here. The
	// synthetic view from the first refresh must not answer for it.
	writeFiles(t, root, map[string]string{"report.yaml": reportArtefact("monthly", "real")})
	if err := st.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	res := st.Rows(ctx, "$sales", 10)
	if len(res.Rows) != 0 || res.Error == "" {
		t.Fatalf("rows = %v, error %q; want no rows and an error", res.Rows, res.Error)
	}
	if filepath.Base(res.File) != "b.yaml" {
		t.Errorf("file = %q, want b.yaml", res.File)
	}
}
