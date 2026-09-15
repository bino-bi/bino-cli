package lint

import (
	"context"
	"testing"
)

func TestDuplicateName(t *testing.T) {
	artefact := func(name string, labels map[string]string) Document {
		return Document{File: "report.yaml", Position: 1, Kind: "ReportArtefact", Name: name, Labels: labels, Raw: rawDoc("ReportArtefact", name, map[string]any{"format": "xga"})}
	}
	named := func(file, kind, name string, constraints []string) Document {
		return Document{File: file, Position: 1, Kind: kind, Name: name, Constraints: parseConstraints(t, constraints...), Raw: rawDoc(kind, name, nil)}
	}

	t.Run("duplicate LayoutPage is reported on the later document", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "LayoutPage", "main", nil),
			named("b.yaml", "LayoutPage", "main", nil),
		}
		findings := duplicateName.Check(context.Background(), docs)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
		f := findings[0]
		if f.RuleID != "duplicate-name" || f.File != "b.yaml" || f.DocIdx != 1 || f.Path != "metadata.name" || f.Severity != "error" {
			t.Errorf("unexpected finding %+v", f)
		}
		want := `duplicate LayoutPage name "main": also defined in a.yaml #1; both are included in artefact "report" after applying constraints`
		if f.Message != want {
			t.Errorf("message = %q, want %q", f.Message, want)
		}
	})

	t.Run("duplicate DataSource", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "DataSource", "sales", nil),
			named("b.yaml", "DataSource", "sales", nil),
		}
		findings := duplicateName.Check(context.Background(), docs)
		if len(findings) != 1 || findings[0].File != "b.yaml" {
			t.Fatalf("expected 1 finding on b.yaml, got %+v", findings)
		}
	})

	t.Run("same name in different kinds is fine", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "DataSource", "sales", nil),
			named("b.yaml", "DataSet", "sales", nil),
		}
		if findings := duplicateName.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("build and preview variants are fine", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "LayoutPage", "main", []string{"mode==build"}),
			named("b.yaml", "LayoutPage", "main", []string{"mode==preview"}),
		}
		if findings := duplicateName.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("preview-only collision is reported", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "LayoutPage", "main", []string{"mode==preview"}),
			named("b.yaml", "LayoutPage", "main", []string{"mode==preview"}),
		}
		findings := duplicateName.Check(context.Background(), docs)
		if len(findings) != 1 || findings[0].File != "b.yaml" {
			t.Fatalf("expected 1 finding on b.yaml, got %+v", findings)
		}
	})

	t.Run("label variants for different artefacts are fine", func(t *testing.T) {
		docs := []Document{
			artefact("dev-report", map[string]string{"env": "dev"}),
			artefact("prod-report", map[string]string{"env": "prod"}),
			named("a.yaml", "LayoutPage", "main", []string{"labels.env==dev"}),
			named("b.yaml", "LayoutPage", "main", []string{"labels.env==prod"}),
		}
		if findings := duplicateName.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("label collision for one artefact is reported", func(t *testing.T) {
		docs := []Document{
			artefact("dev-report", map[string]string{"env": "dev"}),
			artefact("prod-report", map[string]string{"env": "prod"}),
			named("a.yaml", "LayoutPage", "main", []string{"labels.env==dev"}),
			named("b.yaml", "LayoutPage", "main", []string{"labels.env==dev"}),
		}
		findings := duplicateName.Check(context.Background(), docs)
		if len(findings) != 1 || findings[0].File != "b.yaml" {
			t.Fatalf("expected 1 finding on b.yaml, got %+v", findings)
		}
	})

	t.Run("collision in both modes and two artefacts is reported once", func(t *testing.T) {
		docs := []Document{
			artefact("r1", nil),
			artefact("r2", nil),
			named("a.yaml", "LayoutPage", "main", nil),
			named("b.yaml", "LayoutPage", "main", nil),
		}
		findings := duplicateName.Check(context.Background(), docs)
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
		}
	})

	t.Run("no ReportArtefact means no findings", func(t *testing.T) {
		docs := []Document{
			named("a.yaml", "LayoutPage", "main", nil),
			named("b.yaml", "LayoutPage", "main", nil),
		}
		if findings := duplicateName.Check(context.Background(), docs); len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("artefactKind constraints are evaluated for a report", func(t *testing.T) {
		docs := []Document{
			artefact("report", nil),
			named("a.yaml", "LayoutPage", "main", []string{"artefactKind==report"}),
			named("b.yaml", "LayoutPage", "main", []string{"artefactKind==report"}),
		}
		if findings := duplicateName.Check(context.Background(), docs); len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %+v", findings)
		}
	})
}
