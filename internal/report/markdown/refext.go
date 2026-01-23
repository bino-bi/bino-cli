package markdown

import (
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// RefNode represents a :ref[Kind:name] reference in the AST.
type RefNode struct {
	ast.BaseInline
	RefKind string
	RefName string
}

// Kind implements ast.Node.
func (n *RefNode) Kind() ast.NodeKind {
	return KindRefNode
}

// Dump implements ast.Node.
func (n *RefNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"RefKind": n.RefKind,
		"RefName": n.RefName,
	}, nil)
}

// KindRefNode is the ast.NodeKind for RefNode.
var KindRefNode = ast.NewNodeKind("RefNode")

// refPattern matches :ref[Kind:name] syntax.
var refPattern = regexp.MustCompile(`^:ref\[([A-Za-z]+):([A-Za-z0-9_-]+)\]`)

// refParser parses :ref[Kind:name] inline syntax.
type refParser struct{}

// NewRefParser returns a new inline parser for :ref syntax.
func NewRefParser() parser.InlineParser {
	return &refParser{}
}

// Trigger returns the trigger characters for this parser.
func (p *refParser) Trigger() []byte {
	return []byte{':'}
}

// Parse parses :ref[Kind:name] syntax.
func (p *refParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) < 6 { // minimum: :ref[X:Y]
		return nil
	}

	match := refPattern.FindSubmatch(line)
	if match == nil {
		return nil
	}

	kind := string(match[1])
	name := string(match[2])

	// Advance the reader past the matched content
	block.Advance(len(match[0]))
	_ = segment // suppress unused warning

	node := &RefNode{
		RefKind: kind,
		RefName: name,
	}
	return node
}

// refRenderer renders RefNode to HTML.
type refRenderer struct {
	html.Config
}

// NewRefRenderer returns a new renderer for RefNode.
func NewRefRenderer(opts ...html.Option) renderer.NodeRenderer {
	r := &refRenderer{
		Config: html.NewConfig(),
	}
	for _, opt := range opts {
		opt.SetHTMLOption(&r.Config)
	}
	return r
}

// RegisterFuncs implements renderer.NodeRenderer.
func (r *refRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindRefNode, r.renderRef)
}

func (r *refRenderer) renderRef(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	n := node.(*RefNode)
	_, _ = w.WriteString(`<bn-ref kind="`)
	_, _ = w.WriteString(n.RefKind)
	_, _ = w.WriteString(`" name="`)
	_, _ = w.WriteString(n.RefName)
	_, _ = w.WriteString(`"></bn-ref>`)

	return ast.WalkContinue, nil
}

// RefExtension is a goldmark extension for :ref[Kind:name] syntax.
type RefExtension struct{}

// Extend implements goldmark.Extender.
func (e *RefExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(NewRefParser(), 100),
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(NewRefRenderer(), 100),
		),
	)
}

// Ref returns a new RefExtension.
func Ref() goldmark.Extender {
	return &RefExtension{}
}
