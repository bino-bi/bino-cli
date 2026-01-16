package lint

import (
	"context"
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/spec"
)

// parseConstraints is a test helper that parses constraint strings.
func parseConstraints(t *testing.T, strs ...string) []*spec.Constraint {
	t.Helper()
	if len(strs) == 0 {
		return nil
	}
	constraints := make([]*spec.Constraint, 0, len(strs))
	for _, s := range strs {
		c, err := spec.ParseConstraint(s)
		if err != nil {
			t.Fatalf("failed to parse constraint %q: %v", s, err)
		}
		constraints = append(constraints, c)
	}
	return constraints
}

// Helper to create a raw JSON document.
func rawDoc(kind, name string, specData map[string]any) json.RawMessage {
	doc := map[string]any{
		"apiVersion": "bino.bi/v1",
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
	}
	if specData != nil {
		doc["spec"] = specData
	}
	data, _ := json.Marshal(doc)
	return data
}

// Helper to create a raw JSON document with labels and constraints.
func rawDocWithMeta(kind, name string, labels map[string]string, constraints []string, specData map[string]any) json.RawMessage {
	metadata := map[string]any{
		"name": name,
	}
	if labels != nil {
		metadata["labels"] = labels
	}
	if constraints != nil {
		metadata["constraints"] = constraints
	}
	doc := map[string]any{
		"apiVersion": "bino.bi/v1",
		"kind":       kind,
		"metadata":   metadata,
	}
	if specData != nil {
		doc["spec"] = specData
	}
	data, _ := json.Marshal(doc)
	return data
}

func TestReportArtefactRequired_NoArtefact(t *testing.T) {
	docs := []Document{
		{File: "/test/data.yaml", Position: 1, Kind: "DataSet", Name: "sales", Raw: rawDoc("DataSet", "sales", nil)},
		{File: "/test/page.yaml", Position: 1, Kind: "LayoutPage", Name: "page1", Raw: rawDoc("LayoutPage", "page1", nil)},
	}

	findings := reportArtefactRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "report-artefact-required" {
		t.Errorf("expected rule ID 'report-artefact-required', got %q", findings[0].RuleID)
	}
}

func TestReportArtefactRequired_HasArtefact(t *testing.T) {
	docs := []Document{
		{File: "/test/report.yaml", Position: 1, Kind: "ReportArtefact", Name: "report", Raw: rawDoc("ReportArtefact", "report", nil)},
		{File: "/test/page.yaml", Position: 1, Kind: "LayoutPage", Name: "page1", Raw: rawDoc("LayoutPage", "page1", nil)},
	}

	findings := reportArtefactRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestReportArtefactRequired_EmptyDocs(t *testing.T) {
	findings := reportArtefactRequired.Check(context.Background(), nil)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for empty docs, got %d", len(findings))
	}
}

func TestArtefactLayoutPageRequired_NoMatchingPage(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Labels:   map[string]string{"env": "prod"},
			Raw:      rawDoc("ReportArtefact", "report", map[string]any{"format": "xga"}),
		},
		{
			File:        "/test/page.yaml",
			Position:    1,
			Kind:        "LayoutPage",
			Name:        "page1",
			Constraints: parseConstraints(t, "labels.env==dev"), // Won't match prod
			Raw:         rawDoc("LayoutPage", "page1", nil),
		},
	}

	findings := artefactLayoutPageRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "artefact-layoutpage-required" {
		t.Errorf("expected rule ID 'artefact-layoutpage-required', got %q", findings[0].RuleID)
	}
}

func TestArtefactLayoutPageRequired_HasMatchingPage(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Labels:   map[string]string{"env": "prod"},
			Raw:      rawDoc("ReportArtefact", "report", map[string]any{"format": "xga"}),
		},
		{
			File:        "/test/page.yaml",
			Position:    1,
			Kind:        "LayoutPage",
			Name:        "page1",
			Constraints: parseConstraints(t, "labels.env==prod"),
			Raw:         rawDoc("LayoutPage", "page1", map[string]any{"pageFormat": "xga"}),
		},
	}

	findings := artefactLayoutPageRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestArtefactLayoutPageRequired_PageNoConstraints(t *testing.T) {
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
			Raw:      rawDoc("LayoutPage", "page1", nil), // No constraints = always matches
		},
	}

	findings := artefactLayoutPageRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings (no constraints = always matches), got %d", len(findings))
	}
}

func TestArtefactLayoutPageRequired_FormatMismatch(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", map[string]any{"format": "a4"}),
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw:      rawDoc("LayoutPage", "page1", map[string]any{"pageFormat": "xga"}), // Format mismatch
		},
	}

	findings := artefactLayoutPageRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for format mismatch, got %d", len(findings))
	}
}

func TestArtefactLayoutPageRequired_ModeConstraintEitherPasses(t *testing.T) {
	// Test that a page constrained to "mode==preview" still matches
	// because we check BOTH modes
	docs := []Document{
		{
			File:     "/test/report.yaml",
			Position: 1,
			Kind:     "ReportArtefact",
			Name:     "report",
			Raw:      rawDoc("ReportArtefact", "report", nil),
		},
		{
			File:        "/test/page.yaml",
			Position:    1,
			Kind:        "LayoutPage",
			Name:        "page1",
			Constraints: parseConstraints(t, "mode==preview"), // Only matches preview mode
			Raw:         rawDoc("LayoutPage", "page1", nil),
		},
	}

	findings := artefactLayoutPageRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings (mode==preview should match in preview mode), got %d", len(findings))
	}
}

func TestTextContentRequired_EmptyValue(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "header",
			Raw:      rawDoc("Text", "header", map[string]any{"value": ""}),
		},
	}

	findings := textContentRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "text-content-required" {
		t.Errorf("expected rule ID 'text-content-required', got %q", findings[0].RuleID)
	}
	if findings[0].Path != "spec.value" {
		t.Errorf("expected path 'spec.value', got %q", findings[0].Path)
	}
}

func TestTextContentRequired_MissingValue(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "header",
			Raw:      rawDoc("Text", "header", nil), // No spec at all
		},
	}

	findings := textContentRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for missing value, got %d", len(findings))
	}
}

func TestTextContentRequired_HasValue(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "header",
			Raw:      rawDoc("Text", "header", map[string]any{"value": "Hello World"}),
		},
	}

	findings := textContentRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestTextContentRequired_WhitespaceOnly(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "header",
			Raw:      rawDoc("Text", "header", map[string]any{"value": "   \n\t  "}),
		},
	}

	findings := textContentRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for whitespace-only value, got %d", len(findings))
	}
}

func TestDatasetRequired_TableMissingDataset(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/table.yaml",
			Position: 1,
			Kind:     "Table",
			Name:     "sales-table",
			Raw:      rawDoc("Table", "sales-table", nil),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "dataset-required" {
		t.Errorf("expected rule ID 'dataset-required', got %q", findings[0].RuleID)
	}
}

func TestDatasetRequired_TableHasDataset(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/table.yaml",
			Position: 1,
			Kind:     "Table",
			Name:     "sales-table",
			Raw:      rawDoc("Table", "sales-table", map[string]any{"dataset": "sales"}),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
}

func TestDatasetRequired_ChartStructureMissingDataset(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/chart.yaml",
			Position: 1,
			Kind:     "ChartStructure",
			Name:     "sales-chart",
			Raw:      rawDoc("ChartStructure", "sales-chart", nil),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
}

func TestDatasetRequired_ChartTimeHasDataset(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/chart.yaml",
			Position: 1,
			Kind:     "ChartTime",
			Name:     "time-chart",
			Raw:      rawDoc("ChartTime", "time-chart", map[string]any{"dataset": []string{"sales", "costs"}}),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for array dataset, got %d", len(findings))
	}
}

func TestDatasetRequired_EmptyStringDataset(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/table.yaml",
			Position: 1,
			Kind:     "Table",
			Name:     "sales-table",
			Raw:      rawDoc("Table", "sales-table", map[string]any{"dataset": ""}),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding for empty string dataset, got %d", len(findings))
	}
}

func TestDatasetRequired_IgnoresOtherKinds(t *testing.T) {
	docs := []Document{
		{
			File:     "/test/text.yaml",
			Position: 1,
			Kind:     "Text",
			Name:     "header",
			Raw:      rawDoc("Text", "header", nil), // Text doesn't require dataset
		},
		{
			File:     "/test/page.yaml",
			Position: 1,
			Kind:     "LayoutPage",
			Name:     "page1",
			Raw:      rawDoc("LayoutPage", "page1", nil),
		},
	}

	findings := datasetRequired.Check(context.Background(), docs)

	if len(findings) != 0 {
		t.Fatalf("expected 0 findings for non-table/chart kinds, got %d", len(findings))
	}
}

func TestDefaultRulesIncludesAllRules(t *testing.T) {
	rules := DefaultRules()

	expectedIDs := []string{
		"report-artefact-required",
		"artefact-layoutpage-required",
		"text-content-required",
		"dataset-required",
		"page-layout-slots-used",
		"card-layout-slots-used",
	}

	if len(rules) != len(expectedIDs) {
		t.Fatalf("expected %d rules, got %d", len(expectedIDs), len(rules))
	}

	ruleIDs := make(map[string]bool)
	for _, r := range rules {
		ruleIDs[r.ID] = true
	}

	for _, id := range expectedIDs {
		if !ruleIDs[id] {
			t.Errorf("expected rule %q in DefaultRules", id)
		}
	}
}

func TestLayoutPageMatchesFormat(t *testing.T) {
	tests := []struct {
		name       string
		pageFormat string
		target     string
		want       bool
	}{
		{"exact match", "xga", "xga", true},
		{"case insensitive", "XGA", "xga", true},
		{"default matches xga", "", "xga", true},
		{"a4 vs xga", "a4", "xga", false},
		{"empty target matches all", "a4", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := layoutPageMatchesFormat(tt.pageFormat, tt.target)
			if got != tt.want {
				t.Errorf("layoutPageMatchesFormat(%q, %q) = %v, want %v", tt.pageFormat, tt.target, got, tt.want)
			}
		})
	}
}
