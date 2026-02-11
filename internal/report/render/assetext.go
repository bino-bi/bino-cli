package render

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const assetURLPrefix = "asset:"

// assetTransformer rewrites ast.Image nodes whose Destination starts with "asset:"
// to point to the resolved URL from the provided map.
type assetTransformer struct {
	assetURLs map[string]string
}

// Transform implements parser.ASTTransformer.
func (t *assetTransformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	if len(t.assetURLs) == 0 {
		return
	}
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := string(img.Destination)
		if !strings.HasPrefix(dest, assetURLPrefix) {
			return ast.WalkContinue, nil
		}
		name := dest[len(assetURLPrefix):]
		if resolved, found := t.assetURLs[name]; found {
			img.Destination = []byte(resolved)
		} else {
			img.Destination = []byte("#asset-not-found:" + name)
		}
		return ast.WalkContinue, nil
	})
}

// assetExtension registers the assetTransformer with goldmark.
type assetExtension struct {
	assetURLs map[string]string
}

// NewAssetExtension creates a goldmark extension that resolves asset: image URLs.
// If assetURLs is nil or empty the extension is a no-op.
func NewAssetExtension(assetURLs map[string]string) goldmark.Extender {
	return &assetExtension{assetURLs: assetURLs}
}

// Extend implements goldmark.Extender.
func (e *assetExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithASTTransformers(
			util.Prioritized(&assetTransformer{assetURLs: e.assetURLs}, 100),
		),
	)
}
