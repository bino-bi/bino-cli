package markdown

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTOCLinkHrefs checks that only the TOC-only print uses absolute TOC
// links; the inline TOC keeps fragment links for the browser.
func TestTOCLinkHrefs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Intro\n\n## Setup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	render := func(tocOnly bool) string {
		res, err := RenderFilesWithContext(context.Background(), []string{"doc.md"}, FullRenderOptions{
			RenderOptions: RenderOptions{BaseDir: dir, TableOfContents: true},
			TOCOnly:       tocOnly,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}
	if got := render(true); !strings.Contains(got, "<a href='"+TOCLinkPrefix+"setup'>") {
		t.Errorf("TOC-only links:\n%s", got)
	}
	if got := render(false); !strings.Contains(got, "<a href='#setup'>") || strings.Contains(got, TOCLinkPrefix) {
		t.Errorf("inline TOC links:\n%s", got)
	}
}
