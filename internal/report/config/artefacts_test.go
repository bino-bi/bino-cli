package config

import (
	"encoding/json"
	"testing"
)

func TestCollectArtefacts(t *testing.T) {
	doc := Document{
		File:     "report.yaml",
		Position: 1,
		Kind:     "ReportArtefact",
		Name:     "weekly",
	}
	payload := map[string]any{
		"spec": map[string]any{
			"format":      "a4",
			"orientation": "portrait",
			"language":    "de",
			"filename":    "weekly.pdf",
			"title":       "Weekly",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	doc.Raw = raw

	artefacts, err := CollectArtefacts([]Document{doc})
	if err != nil {
		t.Fatalf("CollectArtefacts returned error: %v", err)
	}
	if len(artefacts) != 1 {
		t.Fatalf("expected 1 artefact, got %d", len(artefacts))
	}
	got := artefacts[0]
	if got.Document.Name != "weekly" {
		t.Fatalf("unexpected artefact name %q", got.Document.Name)
	}
	if got.Spec.Filename != "weekly.pdf" {
		t.Fatalf("unexpected filename %q", got.Spec.Filename)
	}
}

func TestCollectArtefactsDuplicateNames(t *testing.T) {
	payload := map[string]any{
		"spec": map[string]any{
			"format":      "a4",
			"orientation": "portrait",
			"language":    "de",
			"filename":    "a.pdf",
			"title":       "A",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	docs := []Document{
		{File: "report.yaml", Position: 1, Kind: "ReportArtefact", Name: "dup", Raw: raw},
		{File: "report2.yaml", Position: 1, Kind: "ReportArtefact", Name: "dup", Raw: raw},
	}
	if _, err := CollectArtefacts(docs); err == nil {
		t.Fatalf("expected duplicate error")
	}
}

func TestApplyReportArtefactDefaults(t *testing.T) {
	spec := ReportArtefactSpec{}
	warnings := applyReportArtefactDefaults("demo", &spec)
	if spec.Format != DefaultArtefactFormat {
		t.Fatalf("expected default format %q, got %q", DefaultArtefactFormat, spec.Format)
	}
	if spec.Orientation != DefaultArtefactOrientation {
		t.Fatalf("expected default orientation %q, got %q", DefaultArtefactOrientation, spec.Orientation)
	}
	if spec.Language != DefaultArtefactLanguage {
		t.Fatalf("expected default language %q, got %q", DefaultArtefactLanguage, spec.Language)
	}
	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %d", len(warnings))
	}

	explicit := ReportArtefactSpec{Format: "a4", Orientation: "portrait", Language: "en"}
	if warns := applyReportArtefactDefaults("demo", &explicit); len(warns) != 0 {
		t.Fatalf("expected no warnings when fields are set, got %d", len(warns))
	}
}

func TestCollectLiveArtefacts(t *testing.T) {
	defaultVal := "all"
	payload := map[string]any{
		"spec": map[string]any{
			"title": "Dashboard",
			"routes": map[string]any{
				"/": map[string]any{
					"artefact": "main-report",
					"queryParams": []any{
						map[string]any{
							"name":        "REGION",
							"default":     defaultVal,
							"description": "Filter region",
						},
						map[string]any{
							"name":        "YEAR",
							"description": "Year filter",
						},
					},
				},
				"/sales": map[string]any{
					"artefact": "sales-report",
					"title":    "Sales",
				},
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	doc := Document{
		File:     "live.yaml",
		Position: 1,
		Kind:     "LiveReportArtefact",
		Name:     "dashboard",
		Raw:      raw,
	}

	liveArtefacts, err := CollectLiveArtefacts([]Document{doc})
	if err != nil {
		t.Fatalf("CollectLiveArtefacts returned error: %v", err)
	}
	if len(liveArtefacts) != 1 {
		t.Fatalf("expected 1 live artefact, got %d", len(liveArtefacts))
	}
	live := liveArtefacts[0]
	if live.Document.Name != "dashboard" {
		t.Fatalf("unexpected name %q", live.Document.Name)
	}
	if live.Spec.Title != "Dashboard" {
		t.Fatalf("unexpected title %q", live.Spec.Title)
	}
	if len(live.Spec.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(live.Spec.Routes))
	}
	if live.Spec.Routes["/"].Artefact != "main-report" {
		t.Fatalf("unexpected root artefact %q", live.Spec.Routes["/"].Artefact)
	}
	if len(live.Spec.Routes["/"].QueryParams) != 2 {
		t.Fatalf("expected 2 query params on root route, got %d", len(live.Spec.Routes["/"].QueryParams))
	}
}

func TestLiveRouteSpec_GetQueryParamDefaults(t *testing.T) {
	defaultVal := "2024"
	route := LiveRouteSpec{
		Artefact: "test-report",
		QueryParams: []LiveQueryParamSpec{
			{Name: "YEAR", Default: &defaultVal},
			{Name: "REGION"}, // no default
		},
	}
	defaults := route.GetQueryParamDefaults()
	if len(defaults) != 1 {
		t.Fatalf("expected 1 default, got %d", len(defaults))
	}
	if defaults["YEAR"] != "2024" {
		t.Fatalf("unexpected default for YEAR: %q", defaults["YEAR"])
	}
}

func TestLiveRouteSpec_GetRequiredQueryParams(t *testing.T) {
	defaultVal := "2024"
	route := LiveRouteSpec{
		Artefact: "test-report",
		QueryParams: []LiveQueryParamSpec{
			{Name: "YEAR", Default: &defaultVal},
			{Name: "REGION"}, // no default = required
		},
	}
	required := route.GetRequiredQueryParams()
	if len(required) != 1 {
		t.Fatalf("expected 1 required, got %d", len(required))
	}
	if required[0] != "REGION" {
		t.Fatalf("expected REGION to be required, got %q", required[0])
	}
}

func TestFindLiveArtefact(t *testing.T) {
	artefacts := []LiveArtefact{
		{Document: Document{Name: "alpha"}},
		{Document: Document{Name: "beta"}},
	}
	found := FindLiveArtefact(artefacts, "beta")
	if found == nil {
		t.Fatalf("expected to find beta")
	}
	if found.Document.Name != "beta" {
		t.Fatalf("unexpected name %q", found.Document.Name)
	}

	notFound := FindLiveArtefact(artefacts, "gamma")
	if notFound != nil {
		t.Fatalf("expected nil for non-existent artefact")
	}
}
