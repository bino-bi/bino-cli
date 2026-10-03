package refresh

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
	reportgraph "bino.bi/bino/internal/report/graph"
)

// docArtefactFixture builds a DocumentArtefact named "handbook" whose
// manifest sits in dir and whose sources resolve against it.
func docArtefactFixture(title, dir string, sources ...string) config.DocumentArtefact {
	return config.DocumentArtefact{
		Document: config.Document{Kind: "DocumentArtefact", Name: "handbook", File: filepath.Join(dir, "handbook.yaml")},
		Spec:     config.DocumentArtefactSpec{Title: title, Format: "a4", Sources: sources},
	}
}

func TestDocSourceCount(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"01_intro.md", "02_body.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("# H\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if got := docSourceCount(docArtefactFixture("H", dir, "*.md")); got != 2 {
		t.Errorf("docSourceCount(glob) = %d, want 2", got)
	}
	if got := docSourceCount(docArtefactFixture("H", dir, "missing/*.md")); got != 0 {
		t.Errorf("docSourceCount(unresolvable) = %d, want 0", got)
	}
}

// TestDocArtefactInfo asserts the doc meta fields reach the toolbar JSON
// payload - and that report artefacts (zero values) omit them entirely.
func TestDocArtefactInfo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# H\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	docArt := docArtefactFixture("The Handbook", dir, "notes.md")
	docArt.Spec.Orientation = "portrait"
	docArt.Spec.Locale = "en"
	docArt.Spec.TableOfContents = true
	docArt.Spec.DisplayHeaderFooter = true

	payload, err := json.Marshal(docArtefactInfo(docArt))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(payload)
	for _, want := range []string{
		`"isDoc":true`,
		`"orientation":"portrait"`,
		`"locale":"en"`,
		`"chapters":1`,
		`"toc":true`,
		`"headerFooter":true`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("payload missing %s: %s", want, got)
		}
	}

	report, err := json.Marshal(previewArtefactInfo{Name: "r", Title: "R", Format: "a4"})
	if err != nil {
		t.Fatalf("marshal report info: %v", err)
	}
	for _, ban := range []string{"orientation", "locale", "chapters", "toc", "headerFooter"} {
		if strings.Contains(string(report), ban) {
			t.Errorf("report artefact payload must omit %s: %s", ban, report)
		}
	}
}

func TestWithAllPagesDocuments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# H\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	handbook := docArtefactFixture("The Handbook", dir, "notes.md")

	t.Run("empty state is replaced with docs-aware message and links", func(t *testing.T) {
		t.Parallel()
		ctx := []byte("<bn-context locale='de'><section class='empty-state'>Define a LayoutPage or Text manifest to see the preview.</section></bn-context>")
		got := string(withAllPagesDocuments(ctx, []config.DocumentArtefact{handbook}))
		for _, want := range []string{
			"No report pages are defined in this bundle.",
			"href='doc/handbook'",
			"The Handbook",
			"a4 · 1 chapter",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "Define a LayoutPage") {
			t.Errorf("misleading report-only empty state still present:\n%s", got)
		}
	})

	t.Run("strip is appended before bn-context close on non-empty views", func(t *testing.T) {
		t.Parallel()
		ctx := []byte("<bn-context locale='de'><bn-layout-page>page</bn-layout-page></bn-context>")
		got := string(withAllPagesDocuments(ctx, []config.DocumentArtefact{handbook}))
		if !strings.Contains(got, "<bn-layout-page>page</bn-layout-page>") {
			t.Errorf("existing content lost:\n%s", got)
		}
		stripIdx := strings.Index(got, "bn-docs-strip")
		closeIdx := strings.LastIndex(got, "</bn-context>")
		if stripIdx == -1 || closeIdx == -1 || stripIdx > closeIdx {
			t.Errorf("strip not inserted inside bn-context:\n%s", got)
		}
	})

	t.Run("no documents leaves the context untouched", func(t *testing.T) {
		t.Parallel()
		ctx := []byte("<bn-context locale='de'>x</bn-context>")
		if got := string(withAllPagesDocuments(ctx, nil)); got != string(ctx) {
			t.Errorf("expected byte-identical output, got %q", got)
		}
	})

	t.Run("titles and names are escaped", func(t *testing.T) {
		t.Parallel()
		evil := docArtefactFixture("<script>alert(1)</script>", dir, "notes.md")
		ctx := []byte("<bn-context locale='de'>x</bn-context>")
		got := string(withAllPagesDocuments(ctx, []config.DocumentArtefact{evil}))
		if strings.Contains(got, "<script>") {
			t.Errorf("title not escaped:\n%s", got)
		}
	})
}

// TestBuildPreviewGraphDataColumns asserts the graph modal payload carries the
// columns of a component and omits the key on nodes without columns.
func TestBuildPreviewGraphDataColumns(t *testing.T) {
	t.Parallel()

	docs := []config.Document{
		{Kind: "DataSet", Name: "sales", File: "test.yaml", Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1", "kind": "DataSet", "metadata": {"name": "sales"},
			"spec": {"query": "SELECT 1 AS ac1"}
		}`)},
		{Kind: "LayoutPage", Name: "page", File: "test.yaml", Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1", "kind": "LayoutPage", "metadata": {"name": "page"},
			"spec": {"children": [{"kind": "Table", "spec": {"dataset": "sales", "scenarios": ["ac1"]}}]}
		}`)},
		{Kind: "ReportArtefact", Name: "report", File: "test.yaml", Raw: json.RawMessage(`{
			"apiVersion": "bino.bi/v1", "kind": "ReportArtefact", "metadata": {"name": "report"},
			"spec": {"filename": "out.pdf", "title": "Report", "layoutPages": ["page"]}
		}`)},
	}
	g, err := reportgraph.Build(context.Background(), docs)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	root, ok := g.ReportArtefactByName("report")
	if !ok {
		t.Fatal("artefact report not found")
	}

	payload, err := json.Marshal(buildPreviewGraphData(g, root))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var data struct {
		Nodes map[string]map[string]json.RawMessage `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	component, ok := data.Nodes["Component:page#0"]
	if !ok {
		t.Fatalf("component node missing: %s", payload)
	}
	var columns []reportgraph.ColumnRef
	if err := json.Unmarshal(component["columns"], &columns); err != nil {
		t.Fatalf("component columns: %v: %s", err, payload)
	}
	want := reportgraph.ColumnRef{Dataset: "sales", Column: "ac1", Role: "scenario", Field: "scenarios"}
	if !slices.Contains(columns, want) {
		t.Errorf("component columns = %+v, want to contain %+v", columns, want)
	}

	dataset, ok := data.Nodes["DataSet:sales"]
	if !ok {
		t.Fatalf("dataset node missing: %s", payload)
	}
	if _, ok := dataset["columns"]; ok {
		t.Errorf("dataset node must omit columns: %s", payload)
	}
}
