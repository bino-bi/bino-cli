package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

const screenshotProjectYAML = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: overview
spec:
  children:
    - kind: LayoutCard
      metadata:
        name: card
      spec:
        children:
          - kind: Text
            metadata:
              name: nested_note
            spec:
              value: "in a card"
    - kind: Table
      ref: kpi_table
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: detail
spec:
  pageFormat: a4
  pageOrientation: portrait
  children:
    - kind: Text
      metadata:
        name: detail_note
      spec:
        value: "detail"
---
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: loose_note
spec:
  value: "on no page"
---
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: kpi_table
spec:
  dataset: kpis
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: monthly
spec:
  format: xga
  filename: monthly.pdf
  title: Monthly
  language: en
  selectedStyle: house
  layoutPages:
    - "over*"
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: detail
spec:
  format: a4
  filename: detail.pdf
  title: Detail
  layoutPages: [detail]
`

func loadScreenshotDocs(t *testing.T, yaml string) []config.Document {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	docs, err := config.LoadDirWithOptions(context.Background(), dir, config.LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

func TestResolveScreenshotJobs(t *testing.T) {
	docs := loadScreenshotDocs(t, screenshotProjectYAML)
	jobs, err := resolveScreenshotJobs(docs, []string{
		"ReportArtefact/monthly",
		"LayoutPage/overview",
		"Text/nested_note",
		"Table/kpi_table",
		"Text/loose_note",
		"overview", // same as LayoutPage/overview: dropped
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 5 {
		t.Fatalf("got %d jobs, want 5 (duplicate dropped)", len(jobs))
	}

	report := jobs[0]
	if report.Report == nil || report.Report.Document.Name != "monthly" || report.Capture.Kind != "LayoutPage" || report.Capture.Name != "" {
		t.Errorf("ReportArtefact job = %+v", report)
	}
	if report.Capture.FileBase != "reportartefact-monthly" {
		t.Errorf("FileBase = %q", report.Capture.FileBase)
	}

	page := jobs[1]
	if page.Report == nil || page.Report.Spec.SelectedStyle != "house" {
		t.Fatalf("page should render in its report: %+v", page.Report)
	}
	if got := page.Report.Spec.LayoutPages; len(got) != 1 || got[0].Page != "overview" {
		t.Errorf("page job layoutPages = %+v, want only overview", got)
	}

	nested := jobs[2]
	if nested.Report == nil || nested.Capture.Kind != "Text" || nested.Capture.Name != "nested_note" || nested.Capture.FileBase != "text-nested_note" {
		t.Errorf("nested component job = %+v", nested)
	}
	if ref := jobs[3]; ref.Report == nil {
		t.Errorf("a ref child should render on its page: %+v", ref)
	}
	if loose := jobs[4]; loose.Shot == nil || loose.Shot.Spec.Refs[0].Name != "loose_note" {
		t.Errorf("standalone component should use a ScreenshotArtefact: %+v", loose)
	}
}

func TestResolveScreenshotJobs_PageWithoutReport(t *testing.T) {
	docs := loadScreenshotDocs(t, `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: alone
spec:
  pageFormat: a4
  pageOrientation: portrait
  children: []
`)
	jobs, err := resolveScreenshotJobs(docs, []string{"alone"})
	if err != nil {
		t.Fatal(err)
	}
	spec := jobs[0].Report.Spec
	if spec.Format != "a4" || spec.Orientation != "portrait" || len(spec.LayoutPages) != 1 || spec.LayoutPages[0].Page != "alone" {
		t.Errorf("page-only report spec = %+v", spec)
	}
}

func TestResolveScreenshotJobs_Errors(t *testing.T) {
	docs := loadScreenshotDocs(t, screenshotProjectYAML)
	for _, value := range []string{"Table/missing", "missing", "LayoutPage/monthly", "Text/"} {
		_, err := resolveScreenshotJobs(docs, []string{value})
		if err == nil {
			t.Errorf("%q: expected an error", value)
			continue
		}
		msg := err.Error()
		for _, want := range []string{"ReportArtefact/monthly", "LayoutPage/overview", "Text/nested_note", "Text/loose_note"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%q: error does not list %s:\n%s", value, want, msg)
			}
		}
	}

	// "detail" is a page and a report.
	_, err := resolveScreenshotJobs(docs, []string{"detail"})
	if err == nil || !strings.Contains(err.Error(), "LayoutPage/detail or ReportArtefact/detail") {
		t.Errorf("ambiguous name: got %v", err)
	}
}

// The documented Grid pattern: a ref child with its own metadata.name. The
// renderer writes the id from the ref, so the capture must use the ref.
func TestResolveScreenshotJobs_RefChildUsesRef(t *testing.T) {
	docs := loadScreenshotDocs(t, `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: grid_page
spec:
  children:
    - kind: Grid
      metadata:
        name: grid
      spec:
        rowHeaders: [electronics]
        columnHeaders: [q1]
        children:
          - row: electronics
            column: q1
            kind: ChartStructure
            ref: detailed-chart
            metadata:
              name: detailed-electronics-q1
---
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: detailed-chart
spec:
  dataset: sales
`)
	for _, value := range []string{"ChartStructure/detailed-chart", "ChartStructure/detailed-electronics-q1"} {
		jobs, err := resolveScreenshotJobs(docs, []string{value})
		if err != nil {
			t.Fatalf("%s: %v", value, err)
		}
		if got := jobs[0].Capture.Name; got != "detailed-chart" {
			t.Errorf("%s: capture name %q, want the ref detailed-chart", value, got)
		}
		if jobs[0].Report == nil || jobs[0].Report.Spec.LayoutPages[0].Page != "grid_page" {
			t.Errorf("%s: want a render of grid_page, got %+v", value, jobs[0])
		}
	}
}

// A component used only as a Tree node resolves by its ref.
func TestResolveScreenshotJobs_TreeRefNode(t *testing.T) {
	docs := loadScreenshotDocs(t, `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: tree_page
spec:
  children:
    - kind: Tree
      metadata:
        name: tree
      spec:
        edges:
          - from: root
            to: leaf
        nodes:
          - id: root
            kind: Label
            spec:
              value: Total
          - id: leaf
            kind: ChartStructure
            ref: tree-chart
---
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: tree-chart
spec:
  dataset: sales
`)
	jobs, err := resolveScreenshotJobs(docs, []string{"ChartStructure/tree-chart"})
	if err != nil {
		t.Fatal(err)
	}
	if got := jobs[0].Capture.Name; got != "tree-chart" {
		t.Errorf("capture name %q, want tree-chart", got)
	}
	if jobs[0].Report == nil || jobs[0].Report.Spec.LayoutPages[0].Page != "tree_page" {
		t.Errorf("want a render of tree_page, got %+v", jobs[0])
	}
}

const constrainedScreenshotYAML = `apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: overview
spec:
  children:
    - kind: Text
      metadata:
        name: detail_note
        constraints:
          - labels.detail == high
      spec:
        value: "only in detailed reports"
    - kind: Text
      metadata:
        name: plain_note
      spec:
        value: "always"
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: a_summary
  labels:
    detail: low
spec:
  format: xga
  filename: summary.pdf
  title: Summary
  layoutPages: [overview]
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: b_detailed
  labels:
    detail: high
spec:
  format: xga
  filename: detailed.pdf
  title: Detailed
  layoutPages: [overview]
`

func TestResolveScreenshotJobs_ComponentPicksReportThatKeepsIt(t *testing.T) {
	docs := loadScreenshotDocs(t, constrainedScreenshotYAML)
	jobs, err := resolveScreenshotJobs(docs, []string{"Text/detail_note", "Text/plain_note", "LayoutPage/overview"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"b_detailed", "a_summary", "a_summary"}
	for i, job := range jobs {
		if job.Report == nil || job.Report.Document.Name != want[i] {
			t.Errorf("%s: rendered in %+v, want %s", job.Ref, job.Report, want[i])
		}
	}
}

func TestResolveScreenshotJobs_ComponentFilteredEverywhere(t *testing.T) {
	yaml := strings.Replace(constrainedScreenshotYAML, "detail: high\nspec", "detail: medium\nspec", 1)
	docs := loadScreenshotDocs(t, yaml)
	_, err := resolveScreenshotJobs(docs, []string{"Text/detail_note"})
	if err == nil || !strings.Contains(err.Error(), "no ReportArtefact that lists its page renders the component") {
		t.Fatalf("got %v", err)
	}
}
