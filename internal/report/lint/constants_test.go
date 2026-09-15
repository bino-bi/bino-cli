package lint

import (
	"context"
	"testing"
)

func constantsDoc(specConstants map[string]any) []Document {
	return []Document{{
		File: "ds.yaml", Kind: "DataSet", Name: "sales", Position: 1,
		Raw: rawDoc("DataSet", "sales", map[string]any{
			"query":     "SELECT 1",
			"constants": map[string]any{"unit": "kEUR", "spec": specConstants},
		}),
	}}
}

func TestDatasetConstantsSpec(t *testing.T) {
	t.Run("unknown kind token", func(t *testing.T) {
		findings := datasetConstantsSpec.Check(context.Background(), constantsDoc(map[string]any{
			"tabel": map[string]any{"barColumns": "ac1,pl1"},
		}))
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		f := findings[0]
		if f.RuleID != "dataset-constants-spec" || f.Path != "spec.constants.spec.tabel" || f.DocIdx != 1 {
			t.Errorf("unexpected finding %+v", f)
		}
		want := "constants.spec.tabel: unknown kind token; expected one of chartbubble, chartbullet, chartscatter, chartstructure, charttime, table, text, any"
		if f.Message != want {
			t.Errorf("message = %q, want %q", f.Message, want)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		findings := datasetConstantsSpec.Check(context.Background(), constantsDoc(map[string]any{
			"table": map[string]any{"barColumnz": "ac1,pl1"},
		}))
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		f := findings[0]
		if f.Path != "spec.constants.spec.table.barColumnz" {
			t.Errorf("unexpected finding %+v", f)
		}
		if want := `constants.spec.table.barColumnz: Table has no spec field "barColumnz"`; f.Message != want {
			t.Errorf("message = %q, want %q", f.Message, want)
		}
	})

	t.Run("any needs the field on at least one kind", func(t *testing.T) {
		findings := datasetConstantsSpec.Check(context.Background(), constantsDoc(map[string]any{
			"any": map[string]any{"scenarios": "ac1,pl1", "nope": 1},
		}))
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		if want := `constants.spec.any.nope: no kind has a spec field "nope"`; findings[0].Message != want {
			t.Errorf("message = %q, want %q", findings[0].Message, want)
		}
	})

	t.Run("known token and fields are fine", func(t *testing.T) {
		findings := datasetConstantsSpec.Check(context.Background(), constantsDoc(map[string]any{
			"table":          map[string]any{"barColumns": "ac1,pl1", "thereof": []any{map[string]any{"rowGroup": "Revenue"}}},
			"chartstructure": map[string]any{"level": "category"},
			"any":            map[string]any{"scenarios": []any{"ac1", "pl1"}},
		}))
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("no constants or no spec key is fine", func(t *testing.T) {
		docs := []Document{
			{Kind: "DataSet", Name: "a", Raw: rawDoc("DataSet", "a", map[string]any{"query": "SELECT 1"})},
			{Kind: "DataSet", Name: "b", Raw: rawDoc("DataSet", "b", map[string]any{"query": "SELECT 1", "constants": map[string]any{"unit": "kEUR"}})},
			{Kind: "DataSource", Name: "c", Raw: rawDoc("DataSource", "c", map[string]any{"type": "csv"})},
		}
		if findings := datasetConstantsSpec.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})
}
