package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTableThereofPositionUnused(t *testing.T) {
	thereof := []any{map[string]any{"rowGroup": "Costs", "category": "Operating expenses", "subCategory": "Rent"}}
	tests := []struct {
		name string
		spec map[string]any
		want bool
	}{
		{"above with thereof", map[string]any{"dataset": "d", "thereofPosition": "above", "thereof": thereof}, false},
		{"above with thereof as JSON string", map[string]any{"dataset": "d", "thereofPosition": "above", "thereof": `[{"rowGroup":"Costs","category":"Opex","subCategory":"Rent"}]`}, false},
		{"below without thereof", map[string]any{"dataset": "d", "thereofPosition": "below"}, false},
		{"not set", map[string]any{"dataset": "d"}, false},
		{"above without thereof", map[string]any{"dataset": "d", "thereofPosition": "above"}, true},
		{"above with empty thereof", map[string]any{"dataset": "d", "thereofPosition": "above", "thereof": []any{}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{File: "/p/t.yaml", Position: 1, Kind: "Table", Name: "t", Raw: rawDoc("Table", "t", tt.spec)}}
			findings := tableThereofPositionUnused.Check(context.Background(), docs)
			if !tt.want {
				if len(findings) != 0 {
					t.Fatalf("findings = %+v, want none", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %+v, want one", findings)
			}
			f := findings[0]
			if f.RuleID != "table-thereof-position-unused" || f.Path != "spec.thereofPosition" ||
				f.Severity != "" || !strings.Contains(f.Message, "sets no 'thereof' list") {
				t.Fatalf("finding = %+v", f)
			}
		})
	}
}

func TestTableVarianceWaterfallUnused(t *testing.T) {
	tests := []struct {
		name string
		spec map[string]any
		want bool
	}{
		{"absolute variance bar", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"dac1_pp1_pos"}}, false},
		{"absolute among others", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"ac1", "drac1_pp1_pos", "dac1_pp1_pos"}}, false},
		{"comma string", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": "ac1,dac1_pp1_pos"}, false},
		{"off", map[string]any{"dataset": "d", "varianceWaterfall": false}, false},
		{"not set", map[string]any{"dataset": "d", "barColumns": []any{"ac1"}}, false},
		{"no barColumns", map[string]any{"dataset": "d", "varianceWaterfall": true}, true},
		{"only relative variance", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"drac1_pp1_pos"}}, true},
		{"only scenarios", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"ac1", "pp1"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{File: "/p/t.yaml", Position: 1, Kind: "Table", Name: "t", Raw: rawDoc("Table", "t", tt.spec)}}
			findings := tableVarianceWaterfallUnused.Check(context.Background(), docs)
			if !tt.want {
				if len(findings) != 0 {
					t.Fatalf("findings = %+v, want none", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %+v, want one", findings)
			}
			f := findings[0]
			if f.RuleID != "table-variance-waterfall-unused" || f.Path != "spec.varianceWaterfall" ||
				f.Severity != "" || !strings.Contains(f.Message, "lists no absolute variance column") {
				t.Fatalf("finding = %+v", f)
			}
		})
	}
}

// A child with a 'ref' inherits the fields of the referenced Table, and an
// inline child is checked inside its page.
func TestTableCalculationSchemeUnused_Children(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "p"},
		"spec": {"children": [
			{"kind": "Table", "ref": "withBars", "spec": {"varianceWaterfall": true}},
			{"kind": "Table", "ref": "waterfallNoBars"},
			{"kind": "Table", "ref": "withThereof", "spec": {"thereofPosition": "above"}},
			{"kind": "Table", "metadata": {"name": "inline"}, "spec": {"dataset": "d", "thereofPosition": "above", "varianceWaterfall": true}}
		]}
	}`)
	docs := []Document{
		{File: "/p/a.yaml", Position: 1, Kind: "Table", Name: "withBars", Raw: rawDoc("Table", "withBars", map[string]any{"dataset": "d", "barColumns": []any{"dac1_pp1_pos"}})},
		{File: "/p/b.yaml", Position: 1, Kind: "Table", Name: "waterfallNoBars", Raw: rawDoc("Table", "waterfallNoBars", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"dac1_pp1_pos"}})},
		{File: "/p/c.yaml", Position: 1, Kind: "Table", Name: "withThereof", Raw: rawDoc("Table", "withThereof", map[string]any{"dataset": "d", "thereof": []any{map[string]any{"rowGroup": "g", "category": "c", "subCategory": "s"}}})},
		{File: "/p/p.yaml", Position: 1, Kind: "LayoutPage", Name: "p", Raw: page},
	}
	waterfall := tableVarianceWaterfallUnused.Check(context.Background(), docs)
	if len(waterfall) != 1 || waterfall[0].File != "/p/p.yaml" || waterfall[0].Path != "spec.children.3.spec.varianceWaterfall" {
		t.Fatalf("waterfall findings = %+v", waterfall)
	}
	thereof := tableThereofPositionUnused.Check(context.Background(), docs)
	if len(thereof) != 1 || thereof[0].File != "/p/p.yaml" || thereof[0].Path != "spec.children.3.spec.thereofPosition" {
		t.Fatalf("thereof findings = %+v", thereof)
	}
}

// A ref child that inherits the field and overrides the list away is
// reported at the child.
func TestTableVarianceWaterfallUnused_InheritedField(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "p"},
		"spec": {"children": [{"kind": "Table", "ref": "base", "spec": {"barColumns": ["ac1"]}}]}
	}`)
	docs := []Document{
		{File: "/p/a.yaml", Position: 1, Kind: "Table", Name: "base", Raw: rawDoc("Table", "base", map[string]any{"dataset": "d", "varianceWaterfall": true, "barColumns": []any{"dac1_pp1_pos"}})},
		{File: "/p/p.yaml", Position: 1, Kind: "LayoutPage", Name: "p", Raw: page},
	}
	findings := tableVarianceWaterfallUnused.Check(context.Background(), docs)
	if len(findings) != 1 || findings[0].Path != "spec.children.0" {
		t.Fatalf("findings = %+v", findings)
	}
}
