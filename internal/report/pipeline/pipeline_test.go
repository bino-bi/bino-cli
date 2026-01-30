package pipeline

import (
	"encoding/json"
	"testing"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// parseConstraints is a test helper that parses constraint strings.
// It panics on parse errors since test data should always be valid.
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

func TestFilterDocsByConstraints(t *testing.T) {
	// Create an artefact document
	artefactRaw, _ := json.Marshal(map[string]any{
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       "ReportArtefact",
		"metadata":   map[string]any{"name": "testArtefact", "labels": map[string]any{"env": "prod"}},
		"spec":       map[string]any{"format": "a4"},
	})

	artefact := config.Artefact{
		Document: config.Document{
			Kind: "ReportArtefact",
			Name: "testArtefact",
			Raw:  artefactRaw,
		},
		Labels: map[string]string{"env": "prod"},
	}

	t.Run("no constraints includes all", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "Asset", Name: "logo", Constraints: nil},
			{Kind: "DataSource", Name: "data", Constraints: nil},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 2 {
			t.Errorf("expected 2 documents, got %d", len(filtered))
		}
	})

	t.Run("mode constraint filters by build", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "Asset", Name: "logo_preview", Constraints: parseConstraints(t, "mode==preview")},
			{Kind: "Asset", Name: "logo_build", Constraints: parseConstraints(t, "mode==build")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document, got %d", len(filtered))
		}
		if filtered[0].Name != "logo_build" {
			t.Errorf("expected logo_build, got %s", filtered[0].Name)
		}
	})

	t.Run("mode constraint filters by preview", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "Asset", Name: "logo_preview", Constraints: parseConstraints(t, "mode==preview")},
			{Kind: "Asset", Name: "logo_build", Constraints: parseConstraints(t, "mode==build")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModePreview)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document, got %d", len(filtered))
		}
		if filtered[0].Name != "logo_preview" {
			t.Errorf("expected logo_preview, got %s", filtered[0].Name)
		}
	})

	t.Run("labels constraint matches", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "Asset", Name: "logo_prod", Constraints: parseConstraints(t, "labels.env==prod")},
			{Kind: "Asset", Name: "logo_dev", Constraints: parseConstraints(t, "labels.env==dev")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document, got %d", len(filtered))
		}
		if filtered[0].Name != "logo_prod" {
			t.Errorf("expected logo_prod, got %s", filtered[0].Name)
		}
	})

	t.Run("spec constraint matches", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "LayoutPage", Name: "page_a4", Constraints: parseConstraints(t, "spec.format==a4")},
			{Kind: "LayoutPage", Name: "page_xga", Constraints: parseConstraints(t, "spec.format==xga")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document, got %d", len(filtered))
		}
		if filtered[0].Name != "page_a4" {
			t.Errorf("expected page_a4, got %s", filtered[0].Name)
		}
	})

	t.Run("ReportArtefact never filtered", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "ReportArtefact", Name: "artefact1", Constraints: parseConstraints(t, "mode==preview")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document (ReportArtefacts should never be filtered), got %d", len(filtered))
		}
	})

	t.Run("multiple constraints all must match", func(t *testing.T) {
		docs := []config.Document{
			{Kind: "Asset", Name: "logo1", Constraints: parseConstraints(t, "mode==build", "labels.env==prod")},
			{Kind: "Asset", Name: "logo2", Constraints: parseConstraints(t, "mode==preview", "labels.env==prod")},
		}

		filtered, err := filterDocsByConstraints(docs, artefact, spec.ModeBuild)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filtered) != 1 {
			t.Errorf("expected 1 document, got %d", len(filtered))
		}
		if filtered[0].Name != "logo1" {
			t.Errorf("expected logo1, got %s", filtered[0].Name)
		}
	})
}

func TestSelectLayoutPagesByPatterns(t *testing.T) {
	// Create test documents
	docs := []config.Document{
		{Kind: "DataSource", Name: "sales-data"},
		{Kind: "DataSet", Name: "sales-query"},
		{Kind: "LayoutPage", Name: "cover"},
		{Kind: "LayoutPage", Name: "sales-summary"},
		{Kind: "LayoutPage", Name: "sales-detail"},
		{Kind: "LayoutPage", Name: "chapter-1"},
		{Kind: "LayoutPage", Name: "chapter-2"},
		{Kind: "LayoutPage", Name: "appendix-a"},
		{Kind: "LayoutPage", Name: "appendix-b"},
	}

	t.Run("default pattern selects all", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"*"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != len(docs) {
			t.Errorf("expected %d docs, got %d", len(docs), len(result))
		}
	})

	t.Run("empty patterns selects all", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != len(docs) {
			t.Errorf("expected %d docs, got %d", len(docs), len(result))
		}
	})

	t.Run("explicit names selection", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"cover", "sales-summary"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have 2 non-LayoutPage + 2 selected LayoutPages
		if len(result) != 4 {
			t.Errorf("expected 4 docs, got %d", len(result))
		}
		// Non-LayoutPage docs should be first
		if result[0].Kind != "DataSource" || result[1].Kind != "DataSet" {
			t.Errorf("non-LayoutPage docs should be first")
		}
		// LayoutPages should be in pattern order
		if result[2].Name != "cover" {
			t.Errorf("expected cover, got %s", result[2].Name)
		}
		if result[3].Name != "sales-summary" {
			t.Errorf("expected sales-summary, got %s", result[3].Name)
		}
	})

	t.Run("glob pattern selection", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"sales-*"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have 2 non-LayoutPage + 2 sales-* pages (sorted alphabetically)
		if len(result) != 4 {
			t.Errorf("expected 4 docs, got %d", len(result))
		}
		// Matches should be sorted alphabetically: sales-detail, sales-summary
		if result[2].Name != "sales-detail" {
			t.Errorf("expected sales-detail, got %s", result[2].Name)
		}
		if result[3].Name != "sales-summary" {
			t.Errorf("expected sales-summary, got %s", result[3].Name)
		}
	})

	t.Run("multiple patterns with ordering", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"cover", "chapter-*", "appendix-*"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// 2 non-LayoutPage + cover + 2 chapter + 2 appendix = 7
		if len(result) != 7 {
			t.Errorf("expected 7 docs, got %d", len(result))
		}
		// Verify order: cover, chapter-1, chapter-2, appendix-a, appendix-b
		expectedOrder := []string{"cover", "chapter-1", "chapter-2", "appendix-a", "appendix-b"}
		for i, expected := range expectedOrder {
			if result[i+2].Name != expected {
				t.Errorf("expected %s at index %d, got %s", expected, i+2, result[i+2].Name)
			}
		}
	})

	t.Run("deduplication across patterns", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"sales-*", "sales-summary"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// sales-summary should only appear once (from first pattern)
		// 2 non-LayoutPage + 2 sales pages = 4
		if len(result) != 4 {
			t.Errorf("expected 4 docs (no duplicates), got %d", len(result))
		}
	})

	t.Run("question mark wildcard", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"chapter-?"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should match chapter-1, chapter-2
		layoutPages := 0
		for _, doc := range result {
			if doc.Kind == "LayoutPage" {
				layoutPages++
			}
		}
		if layoutPages != 2 {
			t.Errorf("expected 2 chapter pages, got %d", layoutPages)
		}
	})

	t.Run("invalid pattern syntax", func(t *testing.T) {
		_, err := selectLayoutPagesByPatterns(docs, []string{"[invalid"})
		if err == nil {
			t.Errorf("expected error for invalid pattern syntax")
		}
	})

	t.Run("no matches for pattern", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"nonexistent-*"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have only non-LayoutPage docs
		if len(result) != 2 {
			t.Errorf("expected 2 docs (no matching LayoutPages), got %d", len(result))
		}
	})

	t.Run("preserves non-LayoutPage document order", func(t *testing.T) {
		result, err := selectLayoutPagesByPatterns(docs, []string{"cover"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// First two should be DataSource, DataSet in original order
		if result[0].Name != "sales-data" || result[0].Kind != "DataSource" {
			t.Errorf("expected DataSource sales-data first")
		}
		if result[1].Name != "sales-query" || result[1].Kind != "DataSet" {
			t.Errorf("expected DataSet sales-query second")
		}
	})
}
