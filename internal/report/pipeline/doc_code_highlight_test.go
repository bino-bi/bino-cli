package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

// renderPlainDocWithFence renders the doc-bundle plaindoc with a yaml fence
// appended and spec.codeHighlight set to spec.
func renderPlainDocWithFence(t *testing.T, spec *config.CodeHighlightSpec) (string, error) {
	t.Helper()
	ctx := context.Background()
	workdir := copyDocBundle(t)
	mdPath := filepath.Join(workdir, "docs", "plaindoc.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	md = append(md, "\n```yaml\nkind: DataSet\n```\n"...)
	if err := os.WriteFile(mdPath, md, 0o644); err != nil {
		t.Fatal(err)
	}

	loadResult, err := LoadManifests(ctx, workdir, nil)
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}
	documentArtefacts, err := config.CollectDocumentArtefacts(loadResult.Documents)
	if err != nil {
		t.Fatalf("collect document artefacts: %v", err)
	}
	var artifact config.DocumentArtefact
	for _, docArt := range documentArtefacts {
		if docArt.Document.Name == "plaindoc" {
			artifact = docArt
		}
	}
	artifact.Spec.CodeHighlight = spec

	result, err := RenderDocumentArtefactHTML(ctx, workdir, loadResult.Documents, artifact, DocumentArtefactRenderOptions{
		EngineVersion: "v1.0.0",
	})
	return string(result.HTML), err
}

func TestRenderDocumentArtefactHTML_CodeHighlight(t *testing.T) {
	tests := []struct {
		name string
		spec *config.CodeHighlightSpec
		want bool
	}{
		{name: "unset spec highlights with default theme", spec: nil, want: true},
		{name: "theme none disables highlighting", spec: &config.CodeHighlightSpec{Theme: "none"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, err := renderPlainDocWithFence(t, tt.spec)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			for _, marker := range []string{`class="chroma"`, ".chroma .k {"} {
				if got := strings.Contains(html, marker); got != tt.want {
					t.Errorf("contains %q = %v, want %v", marker, got, tt.want)
				}
			}
		})
	}
}

func TestRenderDocumentArtefactHTML_UnknownCodeHighlightTheme(t *testing.T) {
	_, err := renderPlainDocWithFence(t, &config.CodeHighlightSpec{Theme: "nosuch"})
	if err == nil {
		t.Fatal("want error for unknown theme")
	}
	for _, want := range []string{"plaindoc", "spec.codeHighlight.theme", `"nosuch"`, "github"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}
