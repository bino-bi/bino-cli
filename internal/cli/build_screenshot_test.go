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
