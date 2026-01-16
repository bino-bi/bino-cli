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
