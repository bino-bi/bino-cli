package cli

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/schema"
)

func TestBuildAnnotationDocument(t *testing.T) {
	doc, err := buildAnnotationDocument(AnnotationManifestData{
		Name:    "drop",
		Mark:    "arrow",
		Color:   "negative",
		Target:  "ChartTime:sales",
		Point:   []string{"date=2024-01-01", "part=total"},
		To:      "ChartTime:sales",
		ToPoint: []string{"date=2026-01-01", "part=total"},
		Title:   "Drop",
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := doc.Spec.(*schema.AnnotationSpec)
	if spec.Mark != "arrow" || spec.Target.Kind != "ChartTime" || spec.Target.Name != "sales" ||
		spec.Target.Point.Date != "2024-01-01" || spec.Target.Point.Part != "total" || spec.To.Point.Date != "2026-01-01" {
		t.Fatalf("spec = %+v", spec)
	}
	manifest, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(manifest); err != nil {
		t.Fatalf("the built document must pass the schema: %v\n%s", err, manifest)
	}
}

func TestBuildAnnotationDocument_DefaultMarkIsLeftOut(t *testing.T) {
	doc, err := buildAnnotationDocument(AnnotationManifestData{Name: "a", Mark: "marker", Target: "Table:t"})
	if err != nil {
		t.Fatal(err)
	}
	spec := doc.Spec.(*schema.AnnotationSpec)
	if spec.Mark != "" || spec.Target.Point != nil {
		t.Fatalf("spec = %+v", spec)
	}
}

func TestBuildAnnotationDocument_Errors(t *testing.T) {
	tests := map[string]AnnotationManifestData{
		"--mark arrow needs --to": {Name: "a", Mark: "arrow", Target: "Table:t"},
		"--to needs --mark arrow": {Name: "a", To: "Table:t", Target: "Table:t"},
		"unknown mark":            {Name: "a", Mark: "ring", Target: "Table:t"},
		"use Kind:name":           {Name: "a", Target: "t"},
		"use field=value":         {Name: "a", Target: "Table:t", Point: []string{"year=2024"}},
		"--to-point needs --to":   {Name: "a", Target: "Table:t", ToPoint: []string{"date=2024-01-01"}},
	}
	for want, data := range tests {
		if _, err := buildAnnotationDocument(data); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
}

func TestSplitPointEntries(t *testing.T) {
	got := splitPointEntries("category=Food, Beverages, scenario=ac1")
	if len(got) != 2 || got[0] != "category=Food, Beverages" || got[1] != "scenario=ac1" {
		t.Fatalf("got %q", got)
	}
	if got := splitPointEntries(" "); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
}
