package daemon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/watchers"
	"bino.bi/bino/pkg/duckdb"
)

const watchFilterManifests = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
spec:
  type: csv
  path: ./data/sales.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: incoming
spec:
  type: csv
  path: ./incoming/*.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: totals
spec:
  dependencies: [sales]
  query:
    $file: ./queries/totals.sql
---
apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata:
  name: handbook
spec:
  format: a4
  title: Handbook
  sources:
    - docs/*.md
  stylesheet: ./docs/style.css
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: later
spec:
  type: csv
  path: ./later
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: lower_exports
spec:
  type: csv
  path: ./exports/[!A-Z]*.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: umsatz
spec:
  type: csv
  path: ./daten/Umsätze.csv
---
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./assets/logo.png
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  messageImage: ./assets/banner.png
  children: []
`

func writeWatchFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newWatchFilterProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeWatchFile(t, root, "report.yaml", watchFilterManifests)
	writeWatchFile(t, root, "data/sales.csv", "region,amount\nnorth,1\n")
	writeWatchFile(t, root, "incoming/jan.csv", "region,amount\nsouth,2\n")
	writeWatchFile(t, root, "queries/totals.sql", "SELECT * FROM sales\n")
	writeWatchFile(t, root, "docs/intro.md", "# Intro\n")
	writeWatchFile(t, root, ".git/index", "x")
	writeWatchFile(t, root, "notes.txt", "x")
	return root
}

func TestWatchRefsRelevant(t *testing.T) {
	root := newWatchFilterProject(t)
	docs, err := config.LoadDirWithOptions(context.Background(), root, config.LoadOptions{Lenient: true})
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(docs))
	for _, d := range docs {
		kinds = append(kinds, d.Kind)
	}
	for _, k := range []string{"DataSource", "DataSet", "DocumentArtefact"} {
		if !slices.Contains(kinds, k) {
			t.Fatalf("fixture did not load a %s, got %v", k, kinds)
		}
	}
	refs := collectWatchRefs(docs)

	cases := map[string]bool{
		".git/index":                    false,
		"notes.txt":                     false,
		"data/other.csv":                false,
		"docs/notes.txt":                false,
		"report.yaml":                   true,
		"reports/new.YML":               true,
		"bino.toml":                     true,
		"bino.lock":                     true,
		"data/sales.csv":                true,
		"incoming/new.csv":              true,
		"queries/totals.sql":            true,
		"docs/intro.md":                 true,
		"docs/chapter2.md":              true,
		"docs/style.css":                true,
		".bino/registry/@s/p/data.csv":  true,
		".bino/registry/@s/p/README.md": true,
		"DATA/Sales.CSV":                true,
		"later":                         true,
		"later/jan.csv":                 true,
		"assets/logo.png":               true,
		"assets/banner.png":             true,
		"assets/other.png":              false,
		"exports/jan.csv":               true,
		"exports/Feb.csv":               false,
		"daten/Umsa\u0308tze.csv":       true, // NFD name on disk, NFC in the manifest
	}
	for rel, want := range cases {
		evt := watchers.Event{Path: filepath.Join(root, rel), RelativePath: rel}
		if got := refs.relevant(evt); got != want {
			t.Errorf("relevant(%s) = %v, want %v", rel, got, want)
		}
	}
}

func TestManagedWatchFiltersEvents(t *testing.T) {
	root := newWatchFilterProject(t)
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
	m := &ManagedState{State: st, root: root, logger: logx.Nop()}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := m.State.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	batches := make(chan []string, 16)
	if err := m.Watch(ctx, func(_ *State, reasons []string) { batches <- reasons }); err != nil {
		t.Fatal(err)
	}

	writeWatchFile(t, root, ".git/index", "y")
	writeWatchFile(t, root, "notes.txt", "y")
	select {
	case b := <-batches:
		t.Fatalf("unexpected refresh: %v", b)
	case <-time.After(time.Second):
	}

	steps := []struct{ rel, content string }{
		{"report.yaml", watchFilterManifests + "\n"},
		{"data/sales.csv", "region,amount\nnorth,3\n"},
		{"incoming/feb.csv", "region,amount\neast,4\n"},
	}
	for _, s := range steps {
		writeWatchFile(t, root, s.rel, s.content)
		want := "change " + filepath.FromSlash(s.rel)
		deadline := time.After(5 * time.Second)
	wait:
		for {
			select {
			case b := <-batches:
				if slices.Contains(b, want) {
					break wait
				}
			case <-deadline:
				t.Fatalf("no refresh for %s", s.rel)
			}
		}
	}
}
