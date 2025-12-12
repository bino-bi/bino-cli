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
