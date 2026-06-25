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

	t.Run("filter inlines literals into a WHERE", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "filtered",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT region, amount FROM sales",
					"filter": {"conditions": [{"column": "region", "op": "equal", "value": "EMEA"}]}
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Base query is wrapped and a WHERE with an inline literal is applied.
		if !strings.Contains(got, "(SELECT region, amount FROM sales)") {
			t.Errorf("expected base query wrapped as subquery in %q", got)
		}
		if !strings.Contains(got, `WHERE (_bn_base."region" = 'EMEA')`) {
			t.Errorf("expected inlined WHERE literal in %q", got)
		}
		// Inline mode must not leave a bound parameter placeholder.
		if strings.Contains(got, "?") {
			t.Errorf("expected no '?' placeholder (inline mode) in %q", got)
		}
	})

	t.Run("groupBy yields GROUP BY and aggregates", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "grouped",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT region, amount FROM sales",
					"groupBy": {
						"columns": ["region"],
						"aggregates": [{"fn": "sum", "column": "amount", "as": "total"}]
					}
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, `sum(_bn_base."amount") AS "total"`) {
			t.Errorf("expected aggregate expression in %q", got)
		}
		if !strings.Contains(got, `GROUP BY _bn_base."region"`) {
			t.Errorf("expected GROUP BY clause in %q", got)
		}
	})

	t.Run("indexColumns yield window and hash columns", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "indexed",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT region, amount FROM sales",
					"indexColumns": [
						{"column": "_rownum", "fn": "rowNumber", "over": "amount"},
						{"column": "_h", "fn": "hash", "of": "region"}
					]
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, `row_number() OVER (ORDER BY _bn_base."amount") AS "_rownum"`) {
			t.Errorf("expected window column in %q", got)
		}
		if !strings.Contains(got, `hash(_bn_base."region") AS "_h"`) {
			t.Errorf("expected hash column in %q", got)
		}
	})

	t.Run("filter+groupBy+indexColumns combined", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "combined",
			Raw: json.RawMessage(`{
				"spec": {
					"query": "SELECT region, amount, sku FROM sales",
					"filter": {"conditions": [{"column": "region", "op": "in", "value": ["EMEA", "APAC"]}]},
					"groupBy": {
						"columns": ["region"],
						"aggregates": [{"fn": "sum", "column": "amount", "as": "total"}]
					},
					"indexColumns": [
						{"column": "categoryIndex", "fn": "denseRank", "over": "region"},
						{"column": "rowGroupIndex", "fn": "hash", "of": "region"}
					]
				}
			}`),
		}
		got, err := resolvedDatasetSQL(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// WHERE with inlined list literals (no bound-parameter placeholder).
		if !strings.Contains(got, `WHERE (_bn_base."region" IN ('EMEA', 'APAC'))`) {
			t.Errorf("expected inlined WHERE list literal in %q", got)
		}
		if strings.Contains(got, "?") {
			t.Errorf("expected no '?' placeholder (inline mode) in %q", got)
		}
		// GROUP BY + aggregate.
		if !strings.Contains(got, `sum(_bn_base."amount") AS "total"`) {
			t.Errorf("expected aggregate expression in %q", got)
		}
		if !strings.Contains(got, `GROUP BY _bn_base."region"`) {
			t.Errorf("expected GROUP BY clause in %q", got)
		}
		// Window + hash index columns projected over the grouped result.
		if !strings.Contains(got, `dense_rank() OVER (ORDER BY _bn_grouped."region") AS "categoryIndex"`) {
			t.Errorf("expected window index column in %q", got)
		}
		if !strings.Contains(got, `hash(_bn_grouped."region") AS "rowGroupIndex"`) {
			t.Errorf("expected hash index column in %q", got)
		}
	})

	t.Run("prql with transforms is an error", func(t *testing.T) {
		doc := config.Document{
			Kind: "DataSet",
			Name: "prql-transform",
			Raw: json.RawMessage(`{
				"spec": {
					"prql": "from sales",
					"groupBy": {
						"columns": ["region"],
						"aggregates": [{"fn": "sum", "column": "amount", "as": "total"}]
					}
				}
			}`),
		}
		if _, err := resolvedDatasetSQL(doc); err == nil {
			t.Fatal("expected error for transforms on a prql dataset")
		}
	})
}
