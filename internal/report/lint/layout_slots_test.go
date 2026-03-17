package lint

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExpectedSlots_PredefinedLayouts(t *testing.T) {
	tests := []struct {
		layout string
		want   int
	}{
		{"full", 1},
		{"split-horizontal", 2},
		{"split-vertical", 2},
		{"2x2", 4},
		{"3x3", 9},
		{"4x4", 16},
		{"1-over-2", 3},
		{"1-over-3", 4},
		{"2-over-1", 3},
		{"3-over-1", 4},
		// Case insensitive
		{"Full", 1},
		{"SPLIT-HORIZONTAL", 2},
		{"2X2", 4},
		// Unknown defaults to 1
		{"unknown", 1},
		{"", 1},
	}

	for _, tt := range tests {
		t.Run(tt.layout, func(t *testing.T) {
			got := expectedSlots(tt.layout, "")
			if got != tt.want {
				t.Errorf("expectedSlots(%q, \"\") = %d, want %d", tt.layout, got, tt.want)
			}
		})
	}
}

func TestExpectedSlots_CustomTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     int
	}{
		{
			name:     "3 areas: a b c",
			template: `"a a" "b c"`,
			want:     3,
		},
		{
			name:     "2 areas: aa bb",
			template: `"aa aa" "bb bb"`,
			want:     2,
		},
		{
			name:     "4 areas: header sidebar main footer",
			template: `"header header" "sidebar main" "footer footer"`,
			want:     4,
		},
		{
			name: "multiline with backslash continuation",
			template: `"a a" \
"b c"`,
			want: 3,
		},
		{
			name: "multiline with newlines",
			template: `"a a"
"b c"`,
			want: 3,
		},
		{
			name:     "single area",
			template: `"main"`,
			want:     1,
		},
		{
			name:     "empty template",
			template: "",
			want:     0,
		},
		{
			name:     "whitespace only",
			template: "   \n\t  ",
			want:     0,
		},
		{
			name:     "with dot (empty cell)",
			template: `"a ." ". b"`,
			want:     2, // a and b, not counting .
		},
		{
			name:     "complex grid",
			template: `"header header header" "nav content sidebar" "footer footer footer"`,
			want:     5, // header, nav, content, sidebar, footer
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expectedSlots("custom-template", tt.template)
			if got != tt.want {
				t.Errorf("expectedSlots(custom-template, %q) = %d, want %d", tt.template, got, tt.want)
			}
		})
	}
}

func TestCountDistinctAreaTokens(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     int
	}{
		{"simple", `"a b c"`, 3},
		{"duplicates", `"a a b b"`, 2},
		{"underscores", `"main_area sub_area"`, 2},
		{"alphanumeric", `"area1 area2"`, 2},
		{"single quotes", `'a b c'`, 3},
		{"mixed quotes", `"a b" 'c d'`, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countDistinctAreaTokens(tt.template)
			if got != tt.want {
				t.Errorf("countDistinctAreaTokens(%q) = %d, want %d", tt.template, got, tt.want)
			}
		})
	}
}

// Helper to create a LayoutPage document with children.
func rawLayoutPage(layout string, customTemplate string, children []map[string]any) json.RawMessage {
	spec := map[string]any{
		"pageLayout": layout,
	}
	if customTemplate != "" {
		spec["pageCustomTemplate"] = customTemplate
	}
	if children != nil {
		spec["children"] = children
	}
	return rawDoc("LayoutPage", "page1", spec)
}

// Helper to create a LayoutCard document with children.
func rawLayoutCard(name string, layout string, customTemplate string, children []map[string]any) json.RawMessage {
	spec := map[string]any{}
	if layout != "" {
		spec["cardLayout"] = layout
	}
	if customTemplate != "" {
		spec["cardCustomTemplate"] = customTemplate
	}
	if children != nil {
		spec["children"] = children
	}
	return rawDoc("LayoutCard", name, spec)
}

func TestPageLayoutSlotsUsed_ExactMatch(t *testing.T) {
	// 2x2 layout with exactly 4 children
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("2x2", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
				{"kind": "Text", "spec": map[string]any{"value": "c"}},
				{"kind": "Text", "spec": map[string]any{"value": "d"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for exact match, got %d: %v", len(findings), findings)
	}
}

func TestPageLayoutSlotsUsed_TooFewChildren(t *testing.T) {
	// 2x2 layout with only 2 children
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("2x2", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "page-layout-slots-used")
	if len(slotFindings) != 1 {
		t.Fatalf("expected 1 slot finding, got %d: %v", len(slotFindings), slotFindings)
	}
}

func TestPageLayoutSlotsUsed_TooManyChildren(t *testing.T) {
	// full layout with 3 children (expects 1)
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("full", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
				{"kind": "Text", "spec": map[string]any{"value": "c"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "page-layout-slots-used")
	if len(slotFindings) != 1 {
		t.Fatalf("expected 1 slot finding for too many children, got %d", len(slotFindings))
	}
}

func TestPageLayoutSlotsUsed_CustomTemplate(t *testing.T) {
	// custom-template with 3 areas and 3 children
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("custom-template", `"a a" "b c"`, []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "1"}},
				{"kind": "Text", "spec": map[string]any{"value": "2"}},
				{"kind": "Text", "spec": map[string]any{"value": "3"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for custom-template match, got %d: %v", len(findings), findings)
	}
}

func TestPageLayoutSlotsUsed_CustomTemplateMismatch(t *testing.T) {
	// custom-template with 2 areas but 3 children
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("custom-template", `"aa aa" "bb bb"`, []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "1"}},
				{"kind": "Text", "spec": map[string]any{"value": "2"}},
				{"kind": "Text", "spec": map[string]any{"value": "3"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "page-layout-slots-used")
	if len(slotFindings) != 1 {
		t.Fatalf("expected 1 slot finding for custom-template mismatch, got %d", len(slotFindings))
	}
}

func TestPageLayoutSlotsUsed_MissingRefDoesNotCount(t *testing.T) {
	// 2x2 layout with 4 children, but 2 have missing refs
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("2x2", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
				{"kind": "Text", "ref": "missing-text-1"}, // Missing ref
				{"kind": "Text", "ref": "missing-text-2"}, // Missing ref
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	// Should have warnings for missing refs AND a slot mismatch (only 2 effective children)
	missingRefFindings := 0
	slotMismatchFindings := 0
	for _, f := range findings {
		if contains(f.Message, "not found") {
			missingRefFindings++
		}
		if contains(f.Message, "expects") && contains(f.Message, "effective children") {
			slotMismatchFindings++
		}
	}

	if missingRefFindings != 2 {
		t.Errorf("expected 2 missing ref warnings, got %d", missingRefFindings)
	}
	if slotMismatchFindings != 1 {
		t.Errorf("expected 1 slot mismatch finding, got %d", slotMismatchFindings)
	}
}

func TestPageLayoutSlotsUsed_ValidRefCounts(t *testing.T) {
	// 2x2 layout with 4 children using valid refs
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/text1.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "text1",
			Raw:      rawDoc("Text", "text1", map[string]any{"value": "Hello"}),
		},
		{
			File:     "/test/text2.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "text2",
			Raw:      rawDoc("Text", "text2", map[string]any{"value": "World"}),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("split-horizontal", "", []map[string]any{
				{"kind": "Text", "ref": "text1"},
				{"kind": "Text", "ref": "text2"},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "page-layout-slots-used")
	if len(slotFindings) != 0 {
		t.Fatalf("expected 0 slot findings for valid refs, got %d: %v", len(slotFindings), slotFindings)
	}
}

func TestPageLayoutSlotsUsed_ConstrainedChildSkipped(t *testing.T) {
	// 2x2 layout with 4 children, but 2 have constraints that don't match
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Labels:   map[string]string{"env": "prod"},
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("2x2", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
				{"kind": "Text", "metadata": map[string]any{"constraints": []string{"labels.env==dev"}}, "spec": map[string]any{"value": "c"}},
				{"kind": "Text", "metadata": map[string]any{"constraints": []string{"labels.env==dev"}}, "spec": map[string]any{"value": "d"}},
			}),
		},
	}

	findings := pageLayoutSlotsUsed.Check(context.Background(), docs)

	// Only 2 effective children (constraints don't match), but 2x2 expects 4
	slotFindings := filterByRuleID(findings, "page-layout-slots-used")
	if len(slotFindings) != 1 {
		t.Fatalf("expected 1 slot finding for constrained children, got %d: %v", len(slotFindings), slotFindings)
	}
}

func TestCardLayoutSlotsUsed_ExactMatch(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/card.yaml",
			Position: 1,
			Kind:     "LayoutCard",
			Name:     "card1",
			Raw: rawLayoutCard("card1", "split-horizontal", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
			}),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("full", "", []map[string]any{
				{"kind": "LayoutCard", "ref": "card1"},
			}),
		},
	}

	findings := cardLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "card-layout-slots-used")
	if len(slotFindings) != 0 {
		t.Fatalf("expected 0 card slot findings, got %d: %v", len(slotFindings), slotFindings)
	}
}

func TestCardLayoutSlotsUsed_TooFewChildren(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/card.yaml",
			Position: 1,
			Kind:     "LayoutCard",
			Name:     "card1",
			Raw: rawLayoutCard("card1", "2x2", "", []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "a"}},
				{"kind": "Text", "spec": map[string]any{"value": "b"}},
			}),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("full", "", []map[string]any{
				{"kind": "LayoutCard", "ref": "card1"},
			}),
		},
	}

	findings := cardLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "card-layout-slots-used")
	if len(slotFindings) != 1 {
		t.Fatalf("expected 1 card slot finding, got %d: %v", len(slotFindings), slotFindings)
	}
}

func TestCardLayoutSlotsUsed_CustomTemplate(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:     "/test/card.yaml",
			Position: 1,
			Kind:     "LayoutCard",
			Name:     "card1",
			Raw: rawLayoutCard("card1", "custom-template", `"a a" "b c"`, []map[string]any{
				{"kind": "Text", "spec": map[string]any{"value": "1"}},
				{"kind": "Text", "spec": map[string]any{"value": "2"}},
				{"kind": "Text", "spec": map[string]any{"value": "3"}},
			}),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw: rawLayoutPage("full", "", []map[string]any{
				{"kind": "LayoutCard", "ref": "card1"},
			}),
		},
	}

	findings := cardLayoutSlotsUsed.Check(context.Background(), docs)

	slotFindings := filterByRuleID(findings, "card-layout-slots-used")
	if len(slotFindings) != 0 {
		t.Fatalf("expected 0 card slot findings for custom-template, got %d: %v", len(slotFindings), slotFindings)
	}
}

// Helper to filter findings by rule ID.
func filterByRuleID(findings []Finding, ruleID string) []Finding {
	var result []Finding
	for _, f := range findings {
		if f.RuleID == ruleID {
			result = append(result, f)
		}
	}
	return result
}

// Helper to check if string contains substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
