package config

import (
	"strings"
	"testing"
)

func ptrString(s string) *string {
	return &s
}

func ptrFloat64(f float64) *float64 {
	return &f
}

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
	// With per-artifact uniqueness, duplicate names across kinds are now allowed
	// at the global level. They will be validated per-artifact after constraint filtering.
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
	// With per-artifact uniqueness, duplicate names are allowed at global level
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
	// Per-artifact name validation catches duplicates within the same kind
	docs := []Document{
		{File: "source1.yaml", Position: 1, Kind: "DataSource", Name: "shared_name"},
		{File: "source2.yaml", Position: 2, Kind: "DataSource", Name: "shared_name"},
	}

	err := ValidateArtefactNames("testArtefact", docs)
	if err == nil {
		t.Fatal("expected validation error for duplicate names within artifact")
	}
	msg := err.Error()
	if !strings.Contains(msg, "testArtefact") || !strings.Contains(msg, "shared_name") {
		t.Fatalf("expected artifact and duplicate name reference, got %v", err)
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
	artifacts := []Artifact{
		{Document: Document{Name: "main-report"}},
		{Document: Document{Name: "sales-report"}},
	}
	layoutPageNames := make(map[string]struct{})

	t.Run("valid live artifact", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/":      {Artifact: "main-report"},
					"/sales": {Artifact: "sales-report"},
				},
			},
		}
		if err := ValidateLiveArtefact(live, artifacts, layoutPageNames); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("missing root route", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/sales": {Artifact: "sales-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, layoutPageNames)
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
					"/":     {Artifact: "main-report"},
					"sales": {Artifact: "sales-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for route without leading slash")
		}
		if !strings.Contains(err.Error(), "must start with") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown artifact reference", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {Artifact: "unknown-report"},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for unknown artifact")
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
						Artifact: "main-report",
						QueryParams: []LiveQueryParamSpec{
							{Name: "YEAR"},
							{Name: "YEAR"},
						},
					},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, layoutPageNames)
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
					"/": {LayoutPages: LayoutPagesOrRefs{{Page: "page1"}, {Page: "page2"}}},
				},
			},
		}
		if err := ValidateLiveArtefact(live, artifacts, lpNames); err != nil {
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
					"/": {LayoutPages: LayoutPagesOrRefs{{Page: "page1"}, {Page: "unknown-page"}}},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, lpNames)
		if err == nil {
			t.Fatal("expected error for unknown layoutPage")
		}
		if !strings.Contains(err.Error(), "unknown LayoutPage") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("route with both artifact and layoutPages", func(t *testing.T) {
		lpNames := map[string]struct{}{
			"page1": {},
		}
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {Artifact: "main-report", LayoutPages: LayoutPagesOrRefs{{Page: "page1"}}},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, lpNames)
		if err == nil {
			t.Fatal("expected error for route with both artifact and layoutPages")
		}
		if !strings.Contains(err.Error(), "both artifact and layoutPages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("route with neither artifact nor layoutPages", func(t *testing.T) {
		live := LiveArtefact{
			Document: Document{Name: "dashboard"},
			Spec: LiveReportArtefactSpec{
				Title: "Dashboard",
				Routes: map[string]LiveRouteSpec{
					"/": {},
				},
			},
		}
		err := ValidateLiveArtefact(live, artifacts, layoutPageNames)
		if err == nil {
			t.Fatal("expected error for route with neither artifact nor layoutPages")
		}
		if !strings.Contains(err.Error(), "must have either artifact or layoutPages") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestValidateLayoutPageParams(t *testing.T) {
	t.Run("valid params", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "regional-sales",
			Params: []LayoutPageParamSpec{
				{Name: "REGION", Type: "string", Required: true},
				{Name: "YEAR", Type: "number", Default: ptrString("2024")},
			},
		}
		warnings, err := ValidateLayoutPageParams(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warnings) > 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	})

	t.Run("missing param name", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{Type: "string"},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err == nil {
			t.Fatal("expected error for missing param name")
		}
		if !strings.Contains(err.Error(), "missing required 'name' field") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("duplicate param name", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{Name: "REGION"},
				{Name: "REGION"},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err == nil {
			t.Fatal("expected error for duplicate param name")
		}
		if !strings.Contains(err.Error(), "duplicate param name") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid param type", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{Name: "REGION", Type: "invalid"},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err == nil {
			t.Fatal("expected error for invalid param type")
		}
		if !strings.Contains(err.Error(), "invalid type") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("select type without options", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{Name: "REGION", Type: "select"},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err == nil {
			t.Fatal("expected error for select without options")
		}
		if !strings.Contains(err.Error(), "requires options.items") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid select type", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{
					Name: "REGION",
					Type: "select",
					Options: &LayoutPageParamOptions{
						Items: []LayoutPageParamOptionItem{
							{Value: "EU", Label: "Europe"},
							{Value: "US", Label: "North America"},
						},
					},
				},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("number type min > max", func(t *testing.T) {
		doc := Document{
			Kind: "LayoutPage",
			Name: "test-page",
			Params: []LayoutPageParamSpec{
				{
					Name: "YEAR",
					Type: "number",
					Options: &LayoutPageParamOptions{
						Min: ptrFloat64(2030),
						Max: ptrFloat64(2020),
					},
				},
			},
		}
		_, err := ValidateLayoutPageParams(doc)
		if err == nil {
			t.Fatal("expected error for min > max")
		}
		if !strings.Contains(err.Error(), "min > max") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("non-LayoutPage returns nil", func(t *testing.T) {
		doc := Document{
			Kind: "DataSource",
			Name: "test",
		}
		warnings, err := ValidateLayoutPageParams(doc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if warnings != nil {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	})
}

func TestValidateLayoutPageRefParams(t *testing.T) {
	pageDocs := map[string]Document{
		"regional-sales": {
			Kind: "LayoutPage",
			Name: "regional-sales",
			Params: []LayoutPageParamSpec{
				{
					Name:     "REGION",
					Type:     "select",
					Required: true,
					Options: &LayoutPageParamOptions{
						Items: []LayoutPageParamOptionItem{
							{Value: "EU"},
							{Value: "US"},
						},
					},
				},
				{Name: "YEAR", Type: "number", Default: ptrString("2024")},
			},
		},
	}

	t.Run("valid params", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "regional-sales",
			Params: map[string]string{"REGION": "EU"},
		}
		warnings, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warnings) > 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	})

	t.Run("missing required param", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "regional-sales",
			Params: map[string]string{},
		}
		_, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err == nil {
			t.Fatal("expected error for missing required param")
		}
		if !strings.Contains(err.Error(), "missing required param") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unknown param warns", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "regional-sales",
			Params: map[string]string{"REGION": "EU", "UNKNOWN": "value"},
		}
		warnings, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warnings) != 1 {
			t.Fatalf("expected 1 warning, got %d", len(warnings))
		}
		if !strings.Contains(warnings[0], "unknown param") {
			t.Fatalf("unexpected warning: %v", warnings[0])
		}
	})

	t.Run("invalid select value", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "regional-sales",
			Params: map[string]string{"REGION": "INVALID"},
		}
		_, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err == nil {
			t.Fatal("expected error for invalid select value")
		}
		if !strings.Contains(err.Error(), "not a valid option") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("page not found returns nil", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "nonexistent",
			Params: map[string]string{},
		}
		warnings, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if warnings != nil {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	})

	t.Run("var reference skips validation", func(t *testing.T) {
		ref := LayoutPageRef{
			Page:   "regional-sales",
			Params: map[string]string{"REGION": "${DEFAULT_REGION}"},
		}
		warnings, err := ValidateLayoutPageRefParams(ref, pageDocs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warnings) > 0 {
			t.Fatalf("unexpected warnings: %v", warnings)
		}
	})
}
