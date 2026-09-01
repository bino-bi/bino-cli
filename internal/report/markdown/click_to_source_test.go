package markdown

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark"

	"bino.bi/bino/internal/report/config"
)

// TestRefRendererWithContext_SourceAttrs proves resolved :ref components
// carry the data-bino-* attributes the preview's search, inspector, and
// cmd/ctrl-click reveal-source key on — and that unresolved refs carry none.
func TestRefRendererWithContext_SourceAttrs(t *testing.T) {
	t.Parallel()

	rc := NewRenderContext([]config.Document{{
		Kind: "Text",
		Name: "intro",
		Raw:  json.RawMessage(`{"kind":"Text","metadata":{"name":"intro"},"spec":{"value":"Hello"}}`),
	}}, nil, nil, "v1.0.0")

	md := goldmark.New(goldmark.WithExtensions(NewRefExtensionWithContext(rc)))

	t.Run("resolved ref", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := md.Convert([]byte(":ref[Text:intro]"), &buf); err != nil {
			t.Fatalf("convert: %v", err)
		}
		got := buf.String()
		for _, want := range []string{
			"data-ref-kind='Text'",
			"data-bino-kind='Text'",
			"data-bino-name='intro'",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q:\n%s", want, got)
			}
		}
		if strings.Contains(got, "id='bino-") {
			t.Errorf("ref containers must not carry ids (a ref used twice would duplicate them):\n%s", got)
		}
	})

	t.Run("unresolved ref renders a bare placeholder", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := md.Convert([]byte(":ref[Text:missing]"), &buf); err != nil {
			t.Fatalf("convert: %v", err)
		}
		got := buf.String()
		if !strings.Contains(got, "<bn-ref kind='Text' name='missing'>") {
			t.Errorf("placeholder missing:\n%s", got)
		}
		if strings.Contains(got, "data-bino-kind") {
			t.Errorf("unresolved ref must not carry source attributes:\n%s", got)
		}
	})
}

// TestRenderFilesWithContext_SourceSections proves each source file's output
// is wrapped in a section carrying its root-relative path, that page breaks
// stay between the sections, and that the wrappers are absent without a
// project root and in TOC-only renders.
func TestRenderFilesWithContext_SourceSections(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := []string{
		filepath.Join(dir, "intro.md"),
		filepath.Join(dir, "sub", "chapter.md"),
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i, f := range files {
		if err := os.WriteFile(f, []byte("# Heading "+string(rune('A'+i))+"\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	render := func(opts FullRenderOptions) string {
		t.Helper()
		result, err := RenderFilesWithContext(context.Background(), files, opts)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		return string(result.HTML)
	}

	t.Run("sections carry root-relative forward-slashed paths", func(t *testing.T) {
		t.Parallel()
		got := render(FullRenderOptions{
			RenderOptions: RenderOptions{BaseDir: dir, PageBreakBetweenFiles: true},
			ProjectRoot:   dir,
		})
		for _, want := range []string{
			"<section class='bn-doc-source' data-bino-file='intro.md'>",
			"<section class='bn-doc-source' data-bino-file='sub/chapter.md'>",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing %q:\n%s", want, got)
			}
		}
		// The page break sits between the sections, not inside one.
		closeFirst := strings.Index(got, "</section>")
		breakIdx := strings.Index(got, "bn-page-break")
		openSecond := strings.Index(got, "data-bino-file='sub/chapter.md'")
		ordered := closeFirst != -1 && closeFirst < breakIdx && breakIdx < openSecond
		if !ordered {
			t.Errorf("expected </section> … page break … <section>, got:\n%s", got)
		}
	})

	t.Run("no project root, no wrappers", func(t *testing.T) {
		t.Parallel()
		got := render(FullRenderOptions{RenderOptions: RenderOptions{BaseDir: dir}})
		if strings.Contains(got, "bn-doc-source") {
			t.Errorf("wrappers must be disabled without a project root:\n%s", got)
		}
	})

	t.Run("TOC-only renders no wrappers", func(t *testing.T) {
		t.Parallel()
		got := render(FullRenderOptions{
			RenderOptions: RenderOptions{BaseDir: dir, TableOfContents: true},
			ProjectRoot:   dir,
			TOCOnly:       true,
		})
		if strings.Contains(got, "bn-doc-source") {
			t.Errorf("TOC-only output must not carry source sections:\n%s", got)
		}
	})
}
