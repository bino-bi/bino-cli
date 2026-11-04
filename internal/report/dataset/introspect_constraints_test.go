package dataset

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// sourcesFixture defines two DataSources named "sales" that a label switches
// between. The real one has an extra column, so columns show which one answered.
func sourcesFixture(t *testing.T, artefacts ...string) []config.Document {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"synthetic.csv": "v\n1\n",
		"real.csv":      "v,real_only\n999,1\n",
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
	}
	for i, data := range artefacts {
		files["report"+string(rune('a'+i))+".yaml"] = `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report` + string(rune('a'+i)) + `
  labels:
    data: ` + data + `
spec:
  filename: r.pdf
  title: Report
`
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := config.LoadDirWithOptions(context.Background(), dir, config.LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestIntrospectColumns_HonoursArtefactConstraints(t *testing.T) {
	cols, err := IntrospectColumns(context.Background(), sourcesFixture(t, "synthetic"), "$sales")
	if err != nil {
		t.Fatalf("IntrospectColumns: %v", err)
	}
	if got := strings.Join(cols, ","); got != "v" {
		t.Fatalf("columns = %q, want v from the synthetic source", got)
	}
}

func TestIntrospectColumns_RefusesAmbiguousSource(t *testing.T) {
	_, err := IntrospectColumns(context.Background(), sourcesFixture(t, "synthetic", "real"), "$sales")
	if err == nil || !strings.Contains(err.Error(), "do not select exactly one") {
		t.Fatalf("err = %v, want an ambiguity error", err)
	}
}
