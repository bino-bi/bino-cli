package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"bino.bi/bino/internal/schema"
)

// Regression: the parameterized LayoutPage/ReportArtefact write paths were
// the only wizard paths that skipped ValidateName and schema.Validate — an
// invalid manifest landed on disk and surfaced later as a build error in a
// file the user did not write.

func discardCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(io.Discard)
	return cmd
}

func assertNotWritten(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("invalid manifest was written to %s", path)
	}
}

func TestWriteLayoutPageManifestWithParamsValidates(t *testing.T) {
	t.Run("reserved name is rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := LayoutPageManifestData{
			Name:   "_inline_bad",
			Params: []LayoutPageParamData{{Name: "region", Type: "string"}},
		}
		if err := writeLayoutPageManifest(discardCmd(), dir, data, "bad_page.yaml", false); err == nil {
			t.Fatal("expected a validation error for a reserved-prefix name")
		}
		assertNotWritten(t, filepath.Join(dir, "bad_page.yaml"))
	})

	t.Run("schema-invalid child kind is rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := LayoutPageManifestData{
			Name:     "param_page",
			Children: []schema.LayoutChild{{Kind: "Bogus", Ref: "x"}},
			Params:   []LayoutPageParamData{{Name: "region", Type: "string"}},
		}
		if err := writeLayoutPageManifest(discardCmd(), dir, data, "invalid_child.yaml", false); err == nil {
			t.Fatal("expected a schema validation error for an unknown child kind")
		}
		assertNotWritten(t, filepath.Join(dir, "invalid_child.yaml"))
	})

	t.Run("valid parameterized page writes and validates", func(t *testing.T) {
		dir := t.TempDir()
		data := LayoutPageManifestData{
			Name: "param_page",
			Params: []LayoutPageParamData{
				{Name: "region", Type: "string", Description: "Region filter", Required: true},
			},
		}
		if err := writeLayoutPageManifest(discardCmd(), dir, data, "good_page.yaml", false); err != nil {
			t.Fatalf("valid parameterized page failed to write: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "good_page.yaml"))
		if err != nil {
			t.Fatalf("read written manifest: %v", err)
		}
		if err := schema.Validate(content); err != nil {
			t.Errorf("written manifest does not validate: %v", err)
		}
	})
}

func TestWriteReportArtefactManifestWithParamsValidates(t *testing.T) {
	t.Run("reserved name is rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := ReportArtefactManifestData{
			Name:           "_inline_report",
			Filename:       "out.pdf",
			Title:          "Report",
			LayoutPageRefs: []LayoutPageRefData{{Page: "page_one", Params: map[string]string{"region": "emea"}}},
		}
		if err := writeReportArtefactManifest(discardCmd(), dir, data, "bad_report.yaml", false); err == nil {
			t.Fatal("expected a validation error for a reserved-prefix name")
		}
		assertNotWritten(t, filepath.Join(dir, "bad_report.yaml"))
	})

	t.Run("missing required title is rejected", func(t *testing.T) {
		dir := t.TempDir()
		data := ReportArtefactManifestData{
			Name:           "param_report",
			Filename:       "out.pdf",
			LayoutPageRefs: []LayoutPageRefData{{Page: "page_one", Params: map[string]string{"region": "emea"}}},
		}
		if err := writeReportArtefactManifest(discardCmd(), dir, data, "untitled_report.yaml", false); err == nil {
			t.Fatal("expected a schema validation error for a missing title")
		}
		assertNotWritten(t, filepath.Join(dir, "untitled_report.yaml"))
	})

	t.Run("valid parameterized artefact writes and validates", func(t *testing.T) {
		dir := t.TempDir()
		data := ReportArtefactManifestData{
			Name:           "param_report",
			Filename:       "out.pdf",
			Title:          "Report",
			LayoutPageRefs: []LayoutPageRefData{{Page: "page_one", Params: map[string]string{"region": "emea"}}},
		}
		if err := writeReportArtefactManifest(discardCmd(), dir, data, "good_report.yaml", false); err != nil {
			t.Fatalf("valid parameterized artefact failed to write: %v", err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "good_report.yaml"))
		if err != nil {
			t.Fatalf("read written manifest: %v", err)
		}
		if err := schema.Validate(content); err != nil {
			t.Errorf("written manifest does not validate: %v", err)
		}
	})
}
