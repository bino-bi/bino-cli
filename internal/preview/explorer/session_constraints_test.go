package explorer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

// The explorer must register the DataSource the artefact's label selects, not
// whichever duplicate comes last.
func TestRefreshHonoursArtefactConstraints(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"synthetic.csv": "v\n1\n",
		"real.csv":      "v\n999\n",
		"sources.yaml": `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==synthetic]
spec:
  type: csv
  path: ./synthetic.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
  constraints: [labels.data==real]
spec:
  type: csv
  path: ./real.csv
`,
		"report.yaml": `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: monthly
  labels:
    data: synthetic
spec:
  filename: monthly.pdf
  title: Monthly
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	docs, err := config.LoadDirWithOptions(ctx, dir, config.LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := NewSession(ctx, logx.Nop())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.Refresh(ctx, docs); err != nil {
		t.Fatal(err)
	}

	var v int
	if err := sess.WithDB(func(db *sql.DB) error {
		return db.QueryRowContext(ctx, `SELECT v FROM "sales"`).Scan(&v)
	}); err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("sales.v = %d from the constraint-excluded source, want 1", v)
	}
}
