package datacheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/lint"
)

const salesSource = `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_src
spec:
  type: inline
  content:
    - category: EU
      ac1: 10
      pp1: 8
`

const salesSet = `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
spec:
  query: |
    SELECT category,
           ac1,
           pp1
    FROM sales_src
`

// loadProject writes the files into a temp dir and loads them.
func loadProject(t *testing.T, files map[string]string) []config.Document {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bino.toml"), []byte("report-id = \"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := config.LoadDir(context.Background(), dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return docs
}

func run(t *testing.T, docs []config.Document) Result {
	t.Helper()
	res, err := Run(context.Background(), docs, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func byRule(findings []lint.Finding, rule string) []lint.Finding {
	var out []lint.Finding
	for _, f := range findings {
		if f.RuleID == rule {
			out = append(out, f)
		}
	}
	return out
}

// lineOf returns the 1-based line of the first line containing s.
func lineOf(t *testing.T, text, s string) int {
	t.Helper()
	for i, l := range strings.Split(text, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	t.Fatalf("%q not in text", s)
	return 0
}

func TestDatasetSQLErrorOnQueryLine(t *testing.T) {
	set := strings.Replace(salesSet, "           pp1", "           pp1_typo", 1)
	docs := loadProject(t, map[string]string{"src.yaml": salesSource, "set.yaml": set})
	res := run(t, docs)

	got := byRule(res.Findings, lint.RuleDatasetSQL)
	if len(got) != 1 {
		t.Fatalf("dataset-sql findings = %+v", res.Findings)
	}
	f := got[0]
	if want := lineOf(t, set, "pp1_typo"); f.Line != want || f.Column != 12 {
		t.Errorf("finding at %d:%d, want %d:12", f.Line, f.Column, want)
	}
	if !strings.HasSuffix(f.File, "set.yaml") || f.Severity != "error" || !strings.Contains(f.Message, "pp1_typo") || !strings.Contains(f.Message, `DataSet "sales"`) {
		t.Errorf("unexpected finding %+v", f)
	}
	if len(res.FailedDataSets) != 1 || res.FailedDataSets[0] != "sales" {
		t.Errorf("FailedDataSets = %v", res.FailedDataSets)
	}
}

func TestScenarioColumnMissingSlot(t *testing.T) {
	page := `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: p
spec:
  children:
    - kind: Table
      spec:
        dataset: sales
        scenarios: [ac1, pl1]
`
	docs := loadProject(t, map[string]string{"src.yaml": salesSource, "set.yaml": salesSet, "page.yaml": page})
	res := run(t, docs)

	got := byRule(res.Findings, lint.RuleScenarioColumn)
	if len(got) != 1 {
		t.Fatalf("scenario-column findings = %+v", res.Findings)
	}
	f := got[0]
	if want := lineOf(t, page, "scenarios:"); f.Line != want || f.Path != "spec.children.0.spec.scenarios.1" {
		t.Errorf("finding at line %d path %s, want line %d", f.Line, f.Path, want)
	}
	if !strings.Contains(f.Message, `"pl1"`) || !strings.Contains(f.Message, `DataSet "sales"`) {
		t.Errorf("message %q", f.Message)
	}
	if len(byRule(res.Findings, lint.RuleDatasetColumn)) != 0 {
		t.Errorf("pl1 reported twice: %+v", res.Findings)
	}
}

func TestScenarioColumnVarianceSlot(t *testing.T) {
	set := strings.Replace(salesSet, "           ac1,\n", "", 1)
	set = strings.Replace(set, "pp1", "pp1, 1 AS pl1", 1)
	chart := `apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: c
spec:
  dataset: sales
  level: category
  scenarios: [pl1]
  variances:
    - dac1_pl1_pos
`
	docs := loadProject(t, map[string]string{"src.yaml": salesSource, "set.yaml": set, "chart.yaml": chart})
	res := run(t, docs)

	got := byRule(res.Findings, lint.RuleScenarioColumn)
	if len(got) != 1 {
		t.Fatalf("scenario-column findings = %+v", res.Findings)
	}
	f := got[0]
	if !strings.Contains(f.Message, "dac1_pl1_pos") || !strings.Contains(f.Message, `"ac1"`) {
		t.Errorf("message %q should name the variance and ac1", f.Message)
	}
	if want := lineOf(t, chart, "dac1_pl1_pos"); f.Line != want {
		t.Errorf("line %d, want %d", f.Line, want)
	}
}

func TestDatasetColumnFilter(t *testing.T) {
	chart := `apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: c
spec:
  dataset: sales
  level: category
  scenarios: [ac1]
  filter: "Category = 'EU'"
`
	docs := loadProject(t, map[string]string{"src.yaml": salesSource, "set.yaml": salesSet, "chart.yaml": chart})
	res := run(t, docs)
	got := byRule(res.Findings, lint.RuleDatasetColumn)
	if len(got) != 1 || !strings.Contains(got[0].Message, `"Category"`) || !strings.Contains(got[0].Message, "case sensitive") {
		t.Fatalf("dataset-column findings = %+v", res.Findings)
	}
	if want := lineOf(t, chart, "filter:"); got[0].Line != want {
		t.Errorf("line %d, want %d", got[0].Line, want)
	}
}

func TestCSVFormatNoHeader(t *testing.T) {
	src := `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: raw
spec:
  type: csv
  path: raw.csv
`
	docs := loadProject(t, map[string]string{"raw.yaml": src, "raw.csv": "1,EU,10.5\n2,US,12.0\n3,DE,9.0\n"})
	res := run(t, docs)
	got := byRule(res.Findings, lint.RuleCSVFormat)
	if len(got) != 1 || got[0].Severity != "warning" || !strings.Contains(got[0].Message, "no header row") {
		t.Fatalf("csv-format findings = %+v", res.Findings)
	}
	if want := lineOf(t, src, "path:"); got[0].Line != want {
		t.Errorf("line %d, want %d", got[0].Line, want)
	}
}

func TestRunStopsAtDeadline(t *testing.T) {
	docs := loadProject(t, map[string]string{"src.yaml": salesSource, "set.yaml": salesSet})
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	res, err := Run(ctx, docs, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Unchecked) != 1 || res.Unchecked[0] != "sales" || len(res.Findings) != 0 {
		t.Errorf("Unchecked = %v, want [sales]", res.Unchecked)
	}
}

// Two variants of a DataSource and a DataSet, picked by the artefact
// language, must each be checked with their own partner only.
func TestConstraintVariantsCheckedPerArtefact(t *testing.T) {
	artefact := func(name, lang string) string {
		return `apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: ` + name + `
spec:
  format: a4
  orientation: portrait
  language: ` + lang + `
  filename: ` + name + `.pdf
  title: T
  layoutPages: [p]
`
	}
	variant := func(lang, col string) string {
		return `apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: src
  constraints: [spec.language==` + lang + `]
spec:
  type: inline
  content:
    - ` + col + `: 1
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales
  constraints: [spec.language==` + lang + `]
spec:
  query: SELECT ` + col + ` AS ac1 FROM src
`
	}
	docs := loadProject(t, map[string]string{
		"de.yaml": artefact("r_de", "de"), "en.yaml": artefact("r_en", "en"),
		"v_de.yaml": variant("de", "umsatz"), "v_en.yaml": variant("en", "revenue"),
	})
	res := run(t, docs)
	if got := byRule(res.Findings, lint.RuleDatasetSQL); len(got) != 0 {
		t.Errorf("each artefact builds, got %+v", got)
	}

	// A real error in one variant is still found, once.
	docs = loadProject(t, map[string]string{
		"de.yaml": artefact("r_de", "de"), "en.yaml": artefact("r_en", "en"),
		"v_de.yaml": variant("de", "umsatz"), "v_en.yaml": strings.Replace(variant("en", "revenue"), "SELECT revenue", "SELECT revnue", 1),
	})
	res = run(t, docs)
	got := byRule(res.Findings, lint.RuleDatasetSQL)
	if len(got) != 1 || !strings.HasSuffix(got[0].File, "v_en.yaml") {
		t.Errorf("want one dataset-sql finding in v_en.yaml, got %+v", got)
	}
}

func TestVariantsCoverOtherArtefactKinds(t *testing.T) {
	shot := `apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata:
  name: shot
  labels:
    target: shot
spec:
  filenamePrefix: shot
  refs:
    - kind: ChartStructure
      name: c
`
	set := strings.Replace(salesSet, "  name: sales\n", "  name: sales\n  constraints: [labels.target==shot]\n", 1)
	orphan := `apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: orphan
  constraints: [mode==preview]
spec:
  query: SELECT 1 AS ac1
`
	docs := loadProject(t, map[string]string{"shot.yaml": shot, "src.yaml": salesSource, "set.yaml": set, "orphan.yaml": orphan})
	sets, uncovered := Variants(docs)
	if len(sets) != 1 || len(uncovered) != 1 || uncovered[0] != "orphan" {
		t.Fatalf("sets %d, uncovered %v; want 1 set and [orphan]", len(sets), uncovered)
	}
	found := false
	for _, d := range sets[0] {
		if d.Kind == "DataSet" && d.Name == "sales" {
			found = true
		}
	}
	if !found {
		t.Error("the DataSet only a ScreenshotArtefact selects is not checked")
	}
}
