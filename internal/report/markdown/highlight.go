package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"

	"bino.bi/bino/internal/report/config"
)

// DefaultCodeHighlightTheme is the chroma style used when codeHighlight.theme is unset.
const DefaultCodeHighlightTheme = "github"

// CodeHighlightNone disables syntax highlighting.
const CodeHighlightNone = "none"

// CodeHighlight is a resolved syntax highlighting setup. A nil value means highlighting is off.
type CodeHighlight struct {
	Style       *chroma.Style
	LineNumbers bool
}

// ResolveCodeHighlight turns the manifest spec into a CodeHighlight.
// It returns nil for theme "none" and an error listing valid names for an unknown theme.
func ResolveCodeHighlight(spec *config.CodeHighlightSpec) (*CodeHighlight, error) {
	theme := DefaultCodeHighlightTheme
	lineNumbers := false
	if spec != nil {
		if spec.Theme != "" {
			theme = spec.Theme
		}
		lineNumbers = spec.LineNumbers
	}
	if theme == CodeHighlightNone {
		return nil, nil
	}
	// styles.Get falls back silently on a miss, so look up the registry directly.
	style, ok := styles.Registry[theme]
	if !ok {
		return nil, fmt.Errorf("unknown theme %q; valid themes: %s, %s",
			theme, CodeHighlightNone, strings.Join(styles.Names(), ", "))
	}
	return &CodeHighlight{Style: style, LineNumbers: lineNumbers}, nil
}

func (h *CodeHighlight) formatOptions() []chromahtml.Option {
	return []chromahtml.Option{
		chromahtml.WithClasses(true),
		chromahtml.WrapLongLines(true),
		chromahtml.WithLineNumbers(h.LineNumbers),
	}
}

func (h *CodeHighlight) extension() goldmark.Extender {
	return highlighting.NewHighlighting(
		highlighting.WithCustomStyle(h.Style),
		highlighting.WithFormatOptions(h.formatOptions()...),
	)
}

// themeCSS returns the chroma class stylesheet for the style, or "" when h is nil.
func (h *CodeHighlight) themeCSS() string {
	if h == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := chromahtml.New(h.formatOptions()...).WriteCSS(&buf, h.Style); err != nil {
		return ""
	}
	return buf.String()
}
