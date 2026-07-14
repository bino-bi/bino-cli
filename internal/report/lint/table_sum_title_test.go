package lint

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// runSumTitleRule runs the rule and returns the paths of the findings it produced.
func runSumTitleRule(t *testing.T, docs []Document) []string {
	t.Helper()
	findings := tableSumTitleUnused.Check(context.Background(), docs)
	paths := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.RuleID != "table-sum-title-unused" {
			t.Errorf("RuleID = %q, want table-sum-title-unused", f.RuleID)
		}
		paths = append(paths, f.Path)
	}
	return paths
}

func TestTableSumTitleUnused_StandaloneTable(t *testing.T) {
	tests := []struct {
		name      string
		spec      map[string]any
		wantPaths []string
	}{
		{
			name:      "type sum renders a total row",
			spec:      map[string]any{"dataset": "$d", "type": "sum", "sumTitle": "Total"},
			wantPaths: nil,
		},
		{
			name:      "type opt renders a total row",
			spec:      map[string]any{"dataset": "$d", "type": "opt", "sumTitle": "Total"},
			wantPaths: nil,
		},
		{
			name:      "type list drops the label",
			spec:      map[string]any{"dataset": "$d", "type": "list", "sumTitle": "Total"},
			wantPaths: []string{"spec.sumTitle"},
		},
		{
			name:      "absent type defaults to list and drops the label",
			spec:      map[string]any{"dataset": "$d", "sumTitle": "Total"},
			wantPaths: []string{"spec.sumTitle"},
		},
		{
			name:      "type sumnototal drops the label",
			spec:      map[string]any{"dataset": "$d", "type": "sumnototal", "sumTitle": "Total"},
			wantPaths: []string{"spec.sumTitle"},
		},
		{
			name:      "type optnototal drops the label",
			spec:      map[string]any{"dataset": "$d", "type": "optnototal", "sumTitle": "Total"},
			wantPaths: []string{"spec.sumTitle"},
		},
		{
			name:      "no sumTitle",
			spec:      map[string]any{"dataset": "$d", "type": "list"},
			wantPaths: nil,
		},
		{
			name:      "blank sumTitle",
			spec:      map[string]any{"dataset": "$d", "type": "list", "sumTitle": "   "},
			wantPaths: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs := []Document{{
				File:     "/p/table.yaml",
				Position: 1,
				Kind:     "Table",
				Name:     "t",
				Raw:      rawDoc("Table", "t", tt.spec),
			}}

			got := runSumTitleRule(t, docs)
			if len(got) != len(tt.wantPaths) {
				t.Fatalf("got %d findings %v, want %d %v", len(got), got, len(tt.wantPaths), tt.wantPaths)
			}
			for i, want := range tt.wantPaths {
				if got[i] != want {
					t.Errorf("Path[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestTableSumTitleUnused_MessageNamesTheType(t *testing.T) {
	docs := []Document{{
		File: "/p/table.yaml",
		Kind: "Table",
		Name: "t",
		Raw:  rawDoc("Table", "t", map[string]any{"dataset": "$d", "sumTitle": "Total"}),
	}}

	findings := tableSumTitleUnused.Check(context.Background(), docs)
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	// An absent type must be reported as the effective default, not as an empty string.
	if want := "'type: list' renders no grand-total row"; !strings.Contains(findings[0].Message, want) {
		t.Errorf("Message = %q, want it to contain %q", findings[0].Message, want)
	}
}

// Inline Table children live inside their parent's Raw — they are never
// materialized as documents — so the rule has to find them by descending.
func TestTableSumTitleUnused_InlineChildren(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{"kind": "Text", "spec": {"value": "hi"}},
				{"kind": "Table", "spec": {"dataset": "$d", "type": "list", "sumTitle": "Total"}},
				{"kind": "Table", "spec": {"dataset": "$d", "type": "sum", "sumTitle": "Total"}},
				{"kind": "LayoutCard", "spec": {
					"children": [
						{"kind": "Table", "spec": {"dataset": "$d", "sumTitle": "Nested"}}
					]
				}}
			]
		}
	}`)

	docs := []Document{{File: "/p/page.yaml", Position: 1, Kind: "LayoutPage", Name: "page", Raw: page}}

	got := runSumTitleRule(t, docs)
	want := []string{
		"spec.children.1.spec.sumTitle",
		"spec.children.3.spec.children.0.spec.sumTitle",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Path[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTableSumTitleUnused_TreeNode(t *testing.T) {
	tree := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "Tree",
		"metadata": {"name": "tree"},
		"spec": {
			"nodes": [
				{"kind": "Table", "spec": {"dataset": "$d", "sumTitle": "Total"}}
			]
		}
	}`)

	docs := []Document{{File: "/p/tree.yaml", Kind: "Tree", Name: "tree", Raw: tree}}

	got := runSumTitleRule(t, docs)
	if len(got) != 1 || got[0] != "spec.nodes.0.spec.sumTitle" {
		t.Fatalf("got %v, want [spec.nodes.0.spec.sumTitle]", got)
	}
}

// A child that only carries a ref inherits the referenced Table's spec, so the
// referenced document is what gets checked — reporting the child too would
// double-report the same mistake.
func TestTableSumTitleUnused_RefChildWithoutOverride(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {"children": [{"kind": "Table", "ref": "base"}]}
	}`)

	docs := []Document{
		{File: "/p/page.yaml", Kind: "LayoutPage", Name: "page", Raw: page},
		{File: "/p/t.yaml", Kind: "Table", Name: "base",
			Raw: rawDoc("Table", "base", map[string]any{"dataset": "$d", "type": "list", "sumTitle": "Total"})},
	}

	got := runSumTitleRule(t, docs)
	if len(got) != 1 || got[0] != "spec.sumTitle" {
		t.Fatalf("got %v, want a single finding on the referenced document (spec.sumTitle)", got)
	}
}

// A ref child's inline spec is merged ON TOP of the referenced Table's spec, so
// an override that supplies only sumTitle still renders the referenced type.
// Reporting it would be a false positive.
func TestTableSumTitleUnused_RefChildInheritsType(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{"kind": "Table", "ref": "sum_base", "spec": {"sumTitle": "Segment A"}},
				{"kind": "Table", "ref": "list_base", "spec": {"sumTitle": "Segment B"}}
			]
		}
	}`)

	docs := []Document{
		{File: "/p/page.yaml", Kind: "LayoutPage", Name: "page", Raw: page},
		{File: "/p/a.yaml", Kind: "Table", Name: "sum_base",
			Raw: rawDoc("Table", "sum_base", map[string]any{"dataset": "$d", "type": "sum"})},
		{File: "/p/b.yaml", Kind: "Table", Name: "list_base",
			Raw: rawDoc("Table", "list_base", map[string]any{"dataset": "$d", "type": "list"})},
	}

	got := runSumTitleRule(t, docs)
	// Only the child inheriting type: list is a real finding; the one inheriting
	// type: sum renders its label just fine.
	want := []string{"spec.children.1.spec.sumTitle"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// A child's own type overrides the referenced document's.
func TestTableSumTitleUnused_RefChildOverridesType(t *testing.T) {
	page := json.RawMessage(`{
		"apiVersion": "bino.bi/v1",
		"kind": "LayoutPage",
		"metadata": {"name": "page"},
		"spec": {
			"children": [
				{"kind": "Table", "ref": "sum_base", "spec": {"type": "list", "sumTitle": "Dropped"}}
			]
		}
	}`)

	docs := []Document{
		{File: "/p/page.yaml", Kind: "LayoutPage", Name: "page", Raw: page},
		{File: "/p/a.yaml", Kind: "Table", Name: "sum_base",
			Raw: rawDoc("Table", "sum_base", map[string]any{"dataset": "$d", "type": "sum"})},
	}

	got := runSumTitleRule(t, docs)
	if len(got) != 1 || got[0] != "spec.children.0.spec.sumTitle" {
		t.Fatalf("got %v, want [spec.children.0.spec.sumTitle]", got)
	}
}

func TestTableSumTitleUnused_IgnoresOtherKinds(t *testing.T) {
	docs := []Document{{
		File: "/p/chart.yaml",
		Kind: "ChartStructure",
		Name: "c",
		Raw:  rawDoc("ChartStructure", "c", map[string]any{"dataset": "$d", "chartTitle": "Revenue"}),
	}}

	if got := runSumTitleRule(t, docs); len(got) != 0 {
		t.Fatalf("got %v, want no findings", got)
	}
}
