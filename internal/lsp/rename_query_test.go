package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const srcDoc = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales
spec:
  type: csv
  path: sales.csv
`

func renameAt(s *Server, u uri.URI, line, char uint32, newName string) (*protocol.WorkspaceEdit, error) {
	return s.Rename(context.Background(), &protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: line, Character: char},
		},
		NewName: newName,
	})
}

func TestRename_DataSourceRewritesQueries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "q.sql"), []byte("SELECT * FROM sales\nJOIN sales_2024 USING (id)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dsDoc := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: by_region
spec:
  dependencies:
    - sales
  query: |
    SELECT region, sum(amount) FROM sales
    JOIN "sales" s ON s.id = sales.id
    WHERE note = 'sales' -- sales in a comment
    UNION ALL SELECT * FROM sales_2024, t.sales, presales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: plain
spec:
  query: SELECT * FROM sales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: from_file
spec:
  query:
    $file: ./q.sql
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: inline_table
spec:
  dataset:
    query: "SELECT x FROM sales"
`
	wantDS := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: by_region
spec:
  dependencies:
    - revenue
  query: |
    SELECT region, sum(amount) FROM revenue
    JOIN "revenue" s ON s.id = revenue.id
    WHERE note = 'sales' -- sales in a comment
    UNION ALL SELECT * FROM sales_2024, t.sales, presales
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: plain
spec:
  query: SELECT * FROM revenue
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: from_file
spec:
  query:
    $file: ./q.sql
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: inline_table
spec:
  dataset:
    query: "SELECT x FROM revenue"
`
	s := newTestServer()
	srcURI := uri.File(filepath.Join(dir, "src.yaml"))
	dsURI := uri.File(filepath.Join(dir, "ds.yaml"))
	s.docs.Set(srcURI, srcDoc, 1)
	s.docs.Set(dsURI, dsDoc, 1)

	edit, err := renameAt(s, srcURI, 3, 9, "revenue")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if edit == nil {
		t.Fatal("expected a workspace edit")
	}
	if got := applyEdits(srcDoc, edit.Changes[srcURI]); !strings.Contains(got, "name: revenue") {
		t.Errorf("definition not renamed:\n%s", got)
	}
	if got := applyEdits(dsDoc, edit.Changes[dsURI]); got != wantDS {
		t.Errorf("ds.yaml after rename:\n%s\nwant:\n%s", got, wantDS)
	}
	qURI := uri.File(filepath.Join(dir, "q.sql"))
	if got := applyEdits("SELECT * FROM sales\nJOIN sales_2024 USING (id)\n", edit.Changes[qURI]); got != "SELECT * FROM revenue\nJOIN sales_2024 USING (id)\n" {
		t.Errorf("q.sql after rename:\n%s", got)
	}
}

func TestRename_DataSetRewritesQueryInOtherDocument(t *testing.T) {
	s, dataURI, _ := navServer(t)
	other := uri.File("/proj/other.yaml")
	doc := "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: top\nspec:\n  query: |\n    SELECT * FROM sales LIMIT 5\n"
	s.docs.Set(other, doc, 1)
	edit, err := renameAt(s, dataURI, 2, 8, "revenue")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if got := applyEdits(doc, edit.Changes[other]); !strings.Contains(got, "SELECT * FROM revenue LIMIT 5") {
		t.Errorf("query not rewritten:\n%s", got)
	}
}

func TestRename_RejectsInvalidName(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/src.yaml")
	s.docs.Set(u, srcDoc, 1)
	for _, bad := range []string{"bad name", "-lead", "Sales-Total"} {
		edit, err := renameAt(s, u, 3, 9, bad)
		var rpcErr *jsonrpc2.Error
		if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc2.InvalidParams {
			t.Errorf("rename to %q: err = %v, want InvalidParams", bad, err)
		}
		if edit != nil {
			t.Errorf("rename to %q returned an edit", bad)
		}
	}
	// A hyphen is fine for a DataSet, which is not a DuckDB view name.
	s2, dataURI, _ := navServer(t)
	if _, err := renameAt(s2, dataURI, 2, 8, "sales-2024"); err != nil {
		t.Errorf("DataSet rename to sales-2024: %v", err)
	}
}

func TestRename_RefusesUneditableQueryScalar(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/src.yaml")
	s.docs.Set(u, srcDoc, 1)
	s.docs.Set(uri.File("/proj/ds.yaml"), "apiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: d\nspec:\n  query: \"SELECT * FROM \\\"sales\\\"\"\n", 1)
	_, err := renameAt(s, u, 3, 9, "revenue")
	if err == nil || !strings.Contains(err.Error(), "/proj/ds.yaml:6") {
		t.Fatalf("err = %v, want a refusal naming /proj/ds.yaml:6", err)
	}
}

func TestRename_KeepsColumnAliasesAndSharedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "q.sql"), []byte("SELECT * FROM sales\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dsDoc := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: totals
spec:
  query: |
    SELECT region, SUM(amount) AS sales FROM orders
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: d1
spec:
  query:
    $file: q.sql
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: d2
spec:
  query:
    $file: ./q.sql
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: d3
spec:
  query:
    $file: ${SQL_DIR}/x.sql
`
	s := newTestServer()
	srcURI := uri.File(filepath.Join(dir, "src.yaml"))
	dsURI := uri.File(filepath.Join(dir, "ds.yaml"))
	s.docs.Set(srcURI, srcDoc, 1)
	s.docs.Set(dsURI, dsDoc, 1)

	edit, err := renameAt(s, srcURI, 3, 9, "revenue")
	if err != nil {
		t.Fatalf("an unreadable query file must not block the rename: %v", err)
	}
	if got := applyEdits(dsDoc, edit.Changes[dsURI]); got != dsDoc {
		t.Errorf("the column alias was rewritten:\n%s", got)
	}
	if n := len(edit.Changes[uri.File(filepath.Join(dir, "q.sql"))]); n != 1 {
		t.Errorf("q.sql got %d edits, want 1", n)
	}
}
