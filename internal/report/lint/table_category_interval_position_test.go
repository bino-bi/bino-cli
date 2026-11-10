package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTableCategoryIntervalPositionUnused(t *testing.T) {
	tests := []struct {
		name        string
		spec        map[string]any
		wantMessage string // empty: no finding
	}{
		{"setname interval", map[string]any{"dataset": "d", "interval": "setname", "categoryIntervalPosition": 2}, ""},
		{"date interval", map[string]any{"dataset": "d", "interval": "month", "categoryIntervalPosition": 1}, ""},
		{"zero without interval", map[string]any{"dataset": "d", "categoryIntervalPosition": 0}, ""},
		{"not set", map[string]any{"dataset": "d"}, ""},
		{"no interval", map[string]any{"dataset": "d", "categoryIntervalPosition": 1}, "the Table has no 'interval'"},
		{"interval none", map[string]any{"dataset": "d", "interval": "none", "categoryIntervalPosition": 1}, "the Table has no 'interval'"},
		{
			"columnthereof wins",
			map[string]any{"dataset": "d", "interval": "setname", "categoryIntervalPosition": 1,
				"columnthereof": []any{map[string]any{"scenario": "ac1", "name": "DE"}}},
			"'columnthereof' turns the interval off",
		},
		{
			"empty columnthereof keeps the interval",
			map[string]any{"dataset": "d", "interval": "setname", "categoryIntervalPosition": 1, "columnthereof": []any{}},
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{File: "/p/t.yaml", Position: 1, Kind: "Table", Name: "t", Raw: rawDoc("Table", "t", tt.spec)}}
			findings := tableCategoryIntervalPositionUnused.Check(context.Background(), docs)
			if tt.wantMessage == "" {
				if len(findings) != 0 {
					t.Fatalf("findings = %+v, want none", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %+v, want one", findings)
			}
			f := findings[0]
			if f.RuleID != "table-category-interval-position-unused" || f.Path != "spec.categoryIntervalPosition" ||
				f.Severity != "" || !strings.Contains(f.Message, tt.wantMessage) {
				t.Fatalf("finding = %+v", f)
			}
		})
	}
}

// A child with a 'ref' inherits the interval of the referenced Table, and an
// inline child is checked inside its page.
func TestTableCategoryIntervalPositionUnused_Children(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{"kind": "Table", "ref": "by_set", "spec": {"categoryIntervalPosition": 1}},
				{"kind": "Table", "ref": "flat", "spec": {"categoryIntervalPosition": 1}},
				{"kind": "Table", "spec": {"dataset": "d", "categoryIntervalPosition": 2}}
			]
		}
	}`)
	docs := []Document{
		{File: "/p/page.yaml", Kind: "LayoutPage", Name: "page", Raw: page},
		{File: "/p/a.yaml", Kind: "Table", Name: "by_set", Raw: rawDoc("Table", "by_set", map[string]any{"dataset": "d", "interval": "setname"})},
		{File: "/p/b.yaml", Kind: "Table", Name: "flat", Raw: rawDoc("Table", "flat", map[string]any{"dataset": "d"})},
	}
	findings := tableCategoryIntervalPositionUnused.Check(context.Background(), docs)
	want := []string{"spec.children.1.spec.categoryIntervalPosition", "spec.children.2.spec.categoryIntervalPosition"}
	if len(findings) != len(want) {
		t.Fatalf("findings = %+v, want %v", findings, want)
	}
	for i, f := range findings {
		if f.Path != want[i] {
			t.Errorf("finding %d path = %q, want %q", i, f.Path, want[i])
		}
	}
}

func TestCategoryIntervalPositionFindings(t *testing.T) {
	data := map[string]json.RawMessage{
		"sales": json.RawMessage(`[{"setname":"Jan"},{"setname":"Feb"},{"setname":"YTD"},{"setname":"Jan"}]`),
		"more":  json.RawMessage(`[{"setname":"Q1"}]`),
	}
	rows := func(binding string) json.RawMessage { return data[binding] }

	tests := []struct {
		name string
		spec map[string]any
		want string // part of the message; empty: no finding
	}{
		{"too large", map[string]any{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 8}, "is 8 but there are only 3 interval group(s)"},
		{"equal to the group count", map[string]any{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 3}, ""},
		{"smaller", map[string]any{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 1}, ""},
		{"two datasets", map[string]any{"dataset": []any{"sales", "more"}, "interval": "setname", "categoryIntervalPosition": 5}, "only 4 interval group(s): dataset sales, more"},
		{"date interval is the engine's", map[string]any{"dataset": "sales", "interval": "month", "categoryIntervalPosition": 8}, ""},
		{"no interval is the static rule's", map[string]any{"dataset": "sales", "categoryIntervalPosition": 8}, ""},
		{
			"columnthereof wins",
			map[string]any{"dataset": "sales", "interval": "setname", "categoryIntervalPosition": 8,
				"columnthereof": []any{map[string]any{"scenario": "ac1", "name": "DE"}}},
			"",
		},
		{"rows not known", map[string]any{"dataset": "$source", "interval": "setname", "categoryIntervalPosition": 8}, ""},
		{"one of two datasets not known", map[string]any{"dataset": []any{"sales", "other"}, "interval": "setname", "categoryIntervalPosition": 8}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{File: "/p/t.yaml", Position: 1, Kind: "Table", Name: "t", Raw: rawDoc("Table", "t", tt.spec)}}
			findings := CategoryIntervalPositionFindings(docs, rows)
			if tt.want == "" {
				if len(findings) != 0 {
					t.Fatalf("findings = %+v, want none", findings)
				}
				return
			}
			if len(findings) != 1 {
				t.Fatalf("findings = %+v, want one", findings)
			}
			f := findings[0]
			if f.RuleID != "table-category-interval-position-too-large" || f.Severity != "warning" ||
				f.Path != "spec.categoryIntervalPosition" || !strings.Contains(f.Message, tt.want) {
				t.Fatalf("finding = %+v", f)
			}
		})
	}
}
