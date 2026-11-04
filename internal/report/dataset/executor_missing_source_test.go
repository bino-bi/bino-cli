package dataset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// A DataSet on a DataSource whose file is missing fails with a missing-table
// error from DuckDB; the error must also name the missing file.
func TestExecute_NamesFailedDataSourceView(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"source.yaml":  "apiVersion: bino.bi/v1alpha1\nkind: DataSource\nmetadata:\n  name: sales\nspec:\n  type: csv\n  path: ./secure/sales.csv\n",
		"dataset.yaml": datasetYAML("  query: SELECT * FROM sales\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Execute(context.Background(), dir, docs, warnOpts(false))
	if err == nil || !strings.Contains(err.Error(), "datasource sales: stat") || !strings.Contains(err.Error(), "sales.csv") {
		t.Fatalf("err = %v, want the missing file named", err)
	}
}
