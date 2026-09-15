package lint

import (
	"context"
	"testing"
)

func TestDatasetDeriveConflict(t *testing.T) {
	decl := map[string]any{"from": "ac1", "shift": "1 year", "grain": "month"}

	t.Run("slot in both maps is reported once with its path", func(t *testing.T) {
		docs := []Document{{
			File: "ds.yaml", Kind: "DataSet", Name: "sales",
			Raw: rawDoc("DataSet", "sales", map[string]any{
				"query":  "SELECT 1",
				"derive": map[string]any{"pp1": decl, "pp2": decl},
				"assert": map[string]any{"pp2": decl, "pp3": decl},
			}),
		}}
		findings := datasetDeriveConflict.Check(context.Background(), docs)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		f := findings[0]
		if f.RuleID != "dataset-derive-conflict" || f.Path != "spec.assert.pp2" {
			t.Errorf("unexpected finding %+v", f)
		}
		want := "slot pp2 is declared in both 'derive' and 'assert'; derive it or assert it, not both"
		if f.Message != want {
			t.Errorf("message = %q, want %q", f.Message, want)
		}
	})

	t.Run("disjoint maps are fine", func(t *testing.T) {
		docs := []Document{{
			Kind: "DataSet", Name: "sales",
			Raw: rawDoc("DataSet", "sales", map[string]any{
				"query":  "SELECT 1",
				"derive": map[string]any{"pp1": decl},
				"assert": map[string]any{"pp2": decl},
			}),
		}}
		if findings := datasetDeriveConflict.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})
}

func TestReservedNamePrefix(t *testing.T) {
	docs := []Document{
		{Kind: "DataSet", Name: "bino_sales", Raw: rawDoc("DataSet", "bino_sales", map[string]any{"query": "SELECT 1"})},
		{Kind: "DataSource", Name: "_bino_ds_x", Raw: rawDoc("DataSource", "_bino_ds_x", map[string]any{"type": "csv"})},
		{Kind: "DataSet", Name: "sales", Raw: rawDoc("DataSet", "sales", map[string]any{"query": "SELECT 1"})},
		{Kind: "Table", Name: "bino_table", Raw: rawDoc("Table", "bino_table", nil)},
		{Kind: "DataSet", Name: "_bino_generated", Labels: map[string]string{"bino.bi/generated": "true"},
			Raw: rawDoc("DataSet", "_bino_generated", map[string]any{"query": "SELECT 1"})},
	}
	findings := reservedNamePrefix.Check(context.Background(), docs)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d: %+v", len(findings), findings)
	}
	if findings[0].Message != `DataSet name "bino_sales" starts with the reserved prefix "bino_"; choose another name` {
		t.Errorf("unexpected message %q", findings[0].Message)
	}
	if findings[1].Path != "metadata.name" || findings[1].RuleID != "reserved-name-prefix" {
		t.Errorf("unexpected finding %+v", findings[1])
	}
}
