package explorer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func TestResolvedDatasetSQL(t *testing.T) {
	t.Run("inline query", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "totals",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT region, SUM(amount) AS total FROM sales GROUP BY region"
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "SELECT region, SUM(amount) AS total FROM sales GROUP BY region"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("source pass-through", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "alias",
			Raw:  json.RawMessage(`{"spec": {"source": "sales"}}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := `SELECT * FROM "sales"`
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("file query", func(t *testing.T) {
		dir := t.TempDir()
		queryFile := filepath.Join(dir, "q.sql")
		if err := os.WriteFile(queryFile, []byte("SELECT 1"), 0o644); err != nil {
			t.Fatal(err)
		}
		doc := config.Document{
			Kind: "DataSet",
			Name: "fromfile",
			File: filepath.Join(dir, "manifest.yaml"),
			Raw:  json.RawMessage(`{"spec": {"query": {"$file": "q.sql"}}}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "SELECT 1" {
			t.Errorf("got %q, want %q", got, "SELECT 1")
		}
	})

	t.Run("inline refs rewritten", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "withinline",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT * FROM @inline(0)",
					"dependencies": ["_inline_ds_abc123"]
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, `"_inline_ds_abc123"`) {
			t.Errorf("expected inline ref rewritten in %q", got)
		}
		if strings.Contains(got, "@inline") {
			t.Errorf("expected @inline removed from %q", got)
		}
	})

	t.Run("missing query is an error", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "broken",
			Raw:  json.RawMessage(`{"spec": {}}`),
		}
		if _, err := resolvedDatasetSQL(doc); err == nil {
			t.Fatal("expected error for dataset with no query/source/prql")
		}
	})

	t.Run("inline ref without dependencies is an error", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "broken-inline",
			Raw:  json.RawMessage(`{"spec": {"query": "SELECT * FROM @inline(0)"}}`),
		}
		if _, err := resolvedDatasetSQL(doc); err == nil {
			t.Fatal("expected error for unresolvable @inline ref")
		}
	})
}
