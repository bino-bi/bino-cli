package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

// copyBundle copies testdata/<name> (recursively) into a temp dir so the
// dataset cache under .bino/ never writes into the repository tree.
func copyBundle(t *testing.T, name string) string {
	t.Helper()
	workdir := t.TempDir()
	if err := os.CopyFS(workdir, os.DirFS(filepath.Join("testdata", name))); err != nil {
		t.Fatalf("copy bundle %s: %v", name, err)
	}
	return workdir
}

// renderDocBundleArtefact loads the doc-bundle and renders one of its
// DocumentArtefacts to HTML.
func renderDocBundleArtefact(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	workdir := copyBundle(t, "doc-bundle")

	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	documentArtefacts, err := config.CollectDocumentArtefacts(loadResult.Documents)
	if err != nil {
		t.Fatalf("collect document artefacts: %v", err)
	}
	var artifact config.DocumentArtefact
	found := false
	for _, docArt := range documentArtefacts {
		if docArt.Document.Name == name {
			artifact = docArt
			found = true
		}
	}
	if !found {
		t.Fatalf("document artefact %s not found in bundle", name)
	}

	result, err := RenderDocumentArtefactHTML(ctx, workdir, loadResult.Documents, artifact, DocumentArtefactRenderOptions{
		EngineVersion: "v1.0.0",
	})
	if err != nil {
		t.Fatalf("render document artefact html: %v", err)
	}
	return string(result.HTML)
}

// TestIntegration_DocRenderExecutesOnlyReferencedData proves a document
// executes and embeds exactly the data its :ref components reach in the
// dependency graph: the directly bound dataset, the source-alias dataset,
// and their datasource — but not the unreferenced dataset.
func TestIntegration_DocRenderExecutesOnlyReferencedData(t *testing.T) {
	html := renderDocBundleArtefact(t, "refdoc")

	if !strings.Contains(html, "bn-document-content") {
		t.Fatal("rendered HTML is missing the document content section")
	}

	// The referenced dataset is embedded with real rows that flowed through
	// DuckDB — the canary that scoping never starves an embedded component.
	datasetRe := regexp.MustCompile(`<bn-dataset[^>]*name='used_ds'[^>]*>([^<]*)</bn-dataset>`)
	m := datasetRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatal("rendered HTML is missing the used_ds dataset element")
	}
	payload := decodeInlinePayload(t, m[1])
	for _, want := range []string{"DACH", "4250"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("used_ds payload missing %q:\n%s", want, payload)
		}
	}

	// The source-alias dataset rides on the DataSet -> DataSource graph edge.
	if !strings.Contains(html, "name='alias_ds'") {
		t.Fatal("rendered HTML is missing the alias_ds dataset element")
	}
	// The datasource reached through the closure is embedded.
	if !strings.Contains(html, "<bn-datasource name='src_a'") {
		t.Fatal("rendered HTML is missing the src_a datasource element")
	}
	// The dataset nothing references must not be executed or embedded.
	if strings.Contains(html, "name='unused_ds'") {
		t.Fatal("rendered HTML embeds unused_ds, which no component references")
	}
}

// TestIntegration_DocRenderZeroRefsExecutesNothing proves a prose-only
// document embeds no data at all.
func TestIntegration_DocRenderZeroRefsExecutesNothing(t *testing.T) {
	html := renderDocBundleArtefact(t, "plaindoc")

	if strings.Contains(html, "<bn-dataset") {
		t.Fatal("prose-only document embeds a dataset")
	}
	if strings.Contains(html, "<bn-datasource") {
		t.Fatal("prose-only document embeds a datasource")
	}
}

// TestDocumentDataScopeFallback proves scoping reports ok=false (render
// falls back to the full document set) when the artefact has no graph node.
func TestDocumentDataScopeFallback(t *testing.T) {
	t.Parallel()

	_, ok := documentDataScope(context.Background(), logx.Nop(), nil, "ghost")
	if ok {
		t.Fatal("expected ok=false for an artefact without a graph node")
	}
}
