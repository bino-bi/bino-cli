package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/schema"
)

// validateLayoutDoc renders a layout document and asserts it passes the
// embedded JSON schema.
func validateLayoutDoc(t *testing.T, doc *schema.Document) string {
	t.Helper()
	b, err := RenderSchemaDocument(doc)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := schema.Validate(b); err != nil {
		t.Fatalf("generated manifest failed schema.Validate:\n%s\nerror: %v", b, err)
	}
	return string(b)
}

func TestBuildLayoutPageDocument(t *testing.T) {
	t.Run("empty skeleton validates", func(t *testing.T) {
		got := validateLayoutDoc(t, buildLayoutPageDocument(LayoutPageManifestData{Name: "new_page"}))
		if !strings.Contains(got, "children: []") {
			t.Errorf("skeleton missing empty children array:\n%s", got)
		}
	})

	t.Run("referenced children validate", func(t *testing.T) {
		data := LayoutPageManifestData{
			Name: "detail_page",
			Children: []schema.LayoutChild{
				{Kind: "Text", Ref: "header_text"},
				{Kind: "Table", Ref: "sales_table"},
			},
		}
		got := validateLayoutDoc(t, buildLayoutPageDocument(data))
		for _, want := range []string{"kind: Text", "ref: header_text", "kind: Table", "ref: sales_table"} {
			if !strings.Contains(got, want) {
				t.Errorf("missing %q in:\n%s", want, got)
			}
		}
	})
}

func TestBuildLayoutCardDocument(t *testing.T) {
	t.Run("empty skeleton validates", func(t *testing.T) {
		got := validateLayoutDoc(t, buildLayoutCardDocument(LayoutCardManifestData{Name: "new_card"}))
		if !strings.Contains(got, "children: []") {
			t.Errorf("skeleton missing empty children array:\n%s", got)
		}
	})

	t.Run("title maps to titleBusinessUnit", func(t *testing.T) {
		data := LayoutCardManifestData{
			Name:     "summary_card",
			Title:    "Sales Summary",
			Children: []schema.LayoutChild{{Kind: "ChartStructure", Ref: "sales_chart"}},
		}
		got := validateLayoutDoc(t, buildLayoutCardDocument(data))
		if !strings.Contains(got, "titleBusinessUnit: Sales Summary") {
			t.Errorf("missing titleBusinessUnit in:\n%s", got)
		}
	})
}

func TestResolveLayoutChildren(t *testing.T) {
	manifests := []ManifestInfo{
		{Kind: "Text", Name: "header_text"},
		{Kind: "Table", Name: "sales_table"},
		{Kind: "DataSet", Name: "sales_data"},
	}

	t.Run("known names resolve with their kind", func(t *testing.T) {
		children, err := resolveLayoutChildren([]string{"sales_table", "header_text"}, manifests)
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		want := []schema.LayoutChild{
			{Kind: "Table", Ref: "sales_table"},
			{Kind: "Text", Ref: "header_text"},
		}
		if len(children) != len(want) {
			t.Fatalf("children = %+v, want %+v", children, want)
		}
		for i := range want {
			if children[i] != want[i] {
				t.Errorf("children[%d] = %+v, want %+v", i, children[i], want[i])
			}
		}
	})

	t.Run("unknown name fails fast", func(t *testing.T) {
		_, err := resolveLayoutChildren([]string{"missing_component"}, manifests)
		if err == nil || !strings.Contains(err.Error(), "missing_component") {
			t.Fatalf("err = %v, want error naming missing_component", err)
		}
	})

	t.Run("non-child kind is not referencable", func(t *testing.T) {
		_, err := resolveLayoutChildren([]string{"sales_data"}, manifests)
		if err == nil {
			t.Fatal("expected error for DataSet used as layout child")
		}
	})
}

// TestWriteLayoutPageManifestNoChildren is the end-to-end repro for
// example-reports#99: `bino add layoutpage <name> --output <p> --no-prompt`
// must write a valid skeleton even though no children exist yet.
func TestWriteLayoutPageManifestNoChildren(t *testing.T) {
	workdir := t.TempDir()
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)

	if err := writeLayoutPageManifest(cmd, workdir, LayoutPageManifestData{Name: "test_page"}, "pages/test.yaml", false); err != nil {
		t.Fatalf("write: %v", err)
	}

	b, err := os.ReadFile(filepath.Join(workdir, "pages", "test.yaml"))
	if err != nil {
		t.Fatalf("read created manifest: %v", err)
	}
	if err := schema.Validate(b); err != nil {
		t.Fatalf("created manifest failed schema.Validate:\n%s\nerror: %v", b, err)
	}
}
