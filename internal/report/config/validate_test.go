package config

import (
	"strings"
	"testing"
)

func TestValidateDocumentsAllowsUnique(t *testing.T) {
	docs := []Document{
		{File: "a.yaml", Position: 1, Kind: "DataSource", Name: "source_a"},
		{File: "b.yaml", Position: 1, Kind: "DataSet", Name: "dataset_b"},
		{File: "c.yaml", Position: 1, Kind: "Asset", Name: "asset"},
		{File: "styles.yaml", Position: 1, Kind: "ComponentStyle", Name: "_default"},
	}

	if err := ValidateDocuments(docs); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateDocumentsAllowsDuplicatesAcrossKinds(t *testing.T) {
	// With per-artefact uniqueness, duplicate names across kinds are now allowed
	// at the global level. They will be validated per-artefact after constraint filtering.
	docs := []Document{
		{File: "source.yaml", Position: 1, Kind: "DataSource", Name: "shared_name"},
		{File: "dataset.yaml", Position: 2, Kind: "DataSet", Name: "shared_name"},
	}

	err := ValidateDocuments(docs)
	if err != nil {
		t.Fatalf("expected no error for duplicate names across kinds, got %v", err)
	}
}

func TestValidateDocumentsAllowsDuplicateAssetsAndLayouts(t *testing.T) {
	// With per-artefact uniqueness, duplicate names are allowed at global level
	docs := []Document{
		{File: "asset.yaml", Position: 1, Kind: "Asset", Name: "logo"},
		{File: "another.yaml", Position: 3, Kind: "LayoutPage", Name: "logo"},
	}

	err := ValidateDocuments(docs)
	if err != nil {
		t.Fatalf("expected no error for duplicate names with constraints, got %v", err)
	}
}

func TestValidateDocumentsDetectsReportArtefactConflicts(t *testing.T) {
	// ReportArtefact names must still be globally unique
	docs := []Document{
		{File: "report1.yaml", Position: 1, Kind: "ReportArtefact", Name: "main_report"},
		{File: "report2.yaml", Position: 2, Kind: "ReportArtefact", Name: "main_report"},
	}

	err := ValidateDocuments(docs)
	if err == nil {
		t.Fatal("expected validation error for duplicate ReportArtefact names")
	}
	msg := err.Error()
	if !strings.Contains(msg, "main_report") {
		t.Fatalf("expected duplicate name reference, got %v", err)
	}
}

func TestValidateArtefactNamesDetectsConflicts(t *testing.T) {
	// Per-artefact name validation catches duplicates within the same kind
	docs := []Document{
		{File: "source1.yaml", Position: 1, Kind: "DataSource", Name: "shared_name"},
		{File: "source2.yaml", Position: 2, Kind: "DataSource", Name: "shared_name"},
	}

	err := ValidateArtefactNames("testArtefact", docs)
	if err == nil {
		t.Fatal("expected validation error for duplicate names within artefact")
	}
	msg := err.Error()
	if !strings.Contains(msg, "testArtefact") || !strings.Contains(msg, "shared_name") {
		t.Fatalf("expected artefact and duplicate name reference, got %v", err)
	}
}

func TestValidateArtefactNamesAllowsDifferentKinds(t *testing.T) {
	// Same name across different kinds is allowed
	docs := []Document{
		{File: "source.yaml", Position: 1, Kind: "DataSource", Name: "data"},
		{File: "dataset.yaml", Position: 2, Kind: "DataSet", Name: "data"},
	}

	err := ValidateArtefactNames("testArtefact", docs)
	if err != nil {
		t.Fatalf("expected no error for same name across kinds, got %v", err)
	}
}

func TestValidateLiveArtefact(t *testing.T) {
	artefacts := []Artefact{
		{Document: Document{Name: "main-report"}},
		{Document: Document{Name: "sales-report"}},
	}
	layoutPageNames := make(map[string]struct{})

	t.Run("valid live artefact", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/":      {Artefact: "main-report"},
					"/sales": {Artefact: "sales-report"},
				},
			},
		}
		if err := ValidateLiveArtefact(live, artefacts, layoutPageNames); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("missing root route", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/sales": {Artefact: "sales-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for missing root route")
		}
		if !strings.Contains(err.Error(), "missing mandatory root route") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("route missing leading slash", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/":     {Artefact: "main-report"},
					"sales": {Artefact: "sales-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for route without leading slash")
		}
		if !strings.Contains(err.Error(), "must start with") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown artefact reference", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {Artefact: "unknown-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for unknown artefact")
		}
		if !strings.Contains(err.Error(), "unknown ReportArtefact") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("duplicate query param names", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {
						Artefact: "main-report",
						QueryParams: []LiveQueryParamSpec{
							{Name: "YEAR"},
							{Name: "YEAR"},
						},
					},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for duplicate query param names")
		}
		if !strings.Contains(err.Error(), "duplicate query param") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid layoutPages route", func(t *testing.T) {
		lpNames := map[string]struct{}{
			"page1": {},
			"page2": {},
		}
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {LayoutPages: []string{"page1", "page2"}},
				},
			},
		}
		if err := ValidateLiveArtefact(live, artefacts, lpNames); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("unknown layoutPage reference", func(t *testing.T) {
		lpNames := map[string]struct{}{
			"page1": {},
		}
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {LayoutPages: []string{"page1", "unknown-page"}},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, lpNames)
		if err == nil {
			t.Fatal("expected error for unknown layoutPage")
		}
		if !strings.Contains(err.Error(), "unknown LayoutPage") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("route with both artefact and layoutPages", func(t *testing.T) {
		lpNames := map[string]struct{}{
			"page1": {},
		}
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {Artefact: "main-report", LayoutPages: []string{"page1"}},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, lpNames)
		if err == nil {
			t.Fatal("expected error for route with both artefact and layoutPages")
		}
		if !strings.Contains(err.Error(), "both artefact and layoutPages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("route with neither artefact nor layoutPages", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {},
				},
			},
		}
		err := ValidateLiveArtefact(live, artefacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for route with neither artefact nor layoutPages")
		}
		if !strings.Contains(err.Error(), "must have either artefact or layoutPages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
