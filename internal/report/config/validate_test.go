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
