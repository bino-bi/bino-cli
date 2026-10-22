package markdown

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bino.bi/bino/internal/report/config"
)

func renderMarkdown(t *testing.T, md string, h *CodeHighlight) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RenderFilesWithContext(context.Background(), []string{"doc.md"}, FullRenderOptions{
		RenderOptions: RenderOptions{BaseDir: dir},
		CodeHighlight: h,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return string(res.HTML)
}

func mustResolve(t *testing.T, spec *config.CodeHighlightSpec) *CodeHighlight {
	t.Helper()
	h, err := ResolveCodeHighlight(spec)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return h
}

func TestCodeHighlightKnownLanguages(t *testing.T) {
	t.Parallel()
	h := mustResolve(t, nil)
	tests := []struct {
		lang, code, token string
	}{
		{"yaml", "kind: DataSet\n", `<span class="nt">kind</span>`},
		{"bash", "echo hello\n", `<span class="nb">echo</span>`},
		{"toml", "[engine]\nversion = \"1\"\n", `<span class="nx">version</span>`},
		{"sql", "SELECT 1 FROM sales;\n", `<span class="k">SELECT</span>`},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			t.Parallel()
			out := renderMarkdown(t, "```"+tt.lang+"\n"+tt.code+"```\n", h)
			if !strings.Contains(out, `class="chroma"`) {
				t.Errorf("missing chroma wrapper:\n%s", out)
			}
			if !strings.Contains(out, tt.token) {
				t.Errorf("missing token %s:\n%s", tt.token, out)
			}
		})
	}
}

func TestCodeHighlightPlainFallback(t *testing.T) {
	t.Parallel()
	h := mustResolve(t, nil)
	tests := []struct {
		name, md, want string
	}{
		{"no language", "```\nplain <text>\n```\n", "<pre><code>plain &lt;text&gt;\n</code></pre>"},
		{"unknown language", "```nosuchlang\nplain\n```\n", `<pre><code class="language-nosuchlang">plain` + "\n</code></pre>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := renderMarkdown(t, tt.md, h)
			if strings.Contains(out, "chroma") {
				t.Errorf("unexpected chroma markup:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("want %q in:\n%s", tt.want, out)
			}
		})
	}
}

func TestCodeHighlightNone(t *testing.T) {
	t.Parallel()
	h := mustResolve(t, &config.CodeHighlightSpec{Theme: "none"})
	if h != nil {
		t.Fatalf("theme none: want nil, got %+v", h)
	}
	out := renderMarkdown(t, "```yaml\nkind: DataSet\n```\n", h)
	if strings.Contains(out, "chroma") || strings.Contains(out, `<span class=`) {
		t.Errorf("unexpected highlight markup:\n%s", out)
	}
	doc, _ := WrapDocumentWithContext([]byte(out), FullDocumentOptions{CodeHighlight: h})
	if strings.Contains(string(doc), ".chroma") {
		t.Errorf("unexpected theme CSS in document")
	}
}

func TestCodeHighlightLineNumbers(t *testing.T) {
	t.Parallel()
	h := mustResolve(t, &config.CodeHighlightSpec{LineNumbers: true})
	out := renderMarkdown(t, "```yaml\na: 1\nb: 2\n```\n", h)
	if !strings.Contains(out, `<span class="ln">1</span>`) || !strings.Contains(out, `<span class="ln">2</span>`) {
		t.Errorf("missing line numbers:\n%s", out)
	}
	if strings.Contains(renderMarkdown(t, "```yaml\na: 1\n```\n", mustResolve(t, nil)), `class="ln"`) {
		t.Errorf("line numbers present by default")
	}
}

func TestResolveCodeHighlight(t *testing.T) {
	t.Parallel()
	if h := mustResolve(t, nil); h.Style.Name != DefaultCodeHighlightTheme || h.LineNumbers {
		t.Errorf("nil spec: got style %q lineNumbers %v", h.Style.Name, h.LineNumbers)
	}
	if h := mustResolve(t, &config.CodeHighlightSpec{LineNumbers: true}); h.Style.Name != DefaultCodeHighlightTheme {
		t.Errorf("empty theme: got style %q", h.Style.Name)
	}
	if h := mustResolve(t, &config.CodeHighlightSpec{Theme: "monokai"}); h.Style.Name != "monokai" {
		t.Errorf("monokai: got style %q", h.Style.Name)
	}
	_, err := ResolveCodeHighlight(&config.CodeHighlightSpec{Theme: "nosuch"})
	if err == nil {
		t.Fatal("unknown theme: want error")
	}
	for _, want := range []string{`"nosuch"`, "valid themes", "none", "github", "monokai"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// TestCodeHighlightCSSBeforeCustomCSS checks that a user stylesheet comes
// after the theme CSS, so it can override token classes.
func TestCodeHighlightCSSBeforeCustomCSS(t *testing.T) {
	t.Parallel()
	const custom = ".chroma .k { color: #123456; }"
	doc, _ := WrapDocumentWithContext([]byte("<p>x</p>"), FullDocumentOptions{
		DocumentOptions: DocumentOptions{Stylesheet: custom},
		CodeHighlight:   mustResolve(t, nil),
	})
	html := string(doc)
	themeIdx := strings.Index(html, "/* Keyword */ .chroma .k {")
	customIdx := strings.Index(html, custom)
	if themeIdx < 0 || customIdx < 0 {
		t.Fatalf("theme CSS at %d, custom CSS at %d", themeIdx, customIdx)
	}
	if themeIdx > customIdx {
		t.Errorf("theme CSS (%d) must come before custom CSS (%d)", themeIdx, customIdx)
	}
	if strings.Index(html, "</style>") < customIdx {
		t.Errorf("custom CSS outside the style block")
	}
}
