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

// RefNode represents a :ref[Kind:name] or :ref[Kind:name]{caption="..."} reference in the AST.
type RefNode struct {
	ast.BaseInline
	RefKind string
	RefName string
	Caption string // optional caption for figure wrapping
}

// Kind implements ast.Node.
func (n *RefNode) Kind() ast.NodeKind {
	return KindRefNode
}

// Dump implements ast.Node.
func (n *RefNode) Dump(source []byte, level int) {
	m := map[string]string{
		"RefKind": n.RefKind,
		"RefName": n.RefName,
	}
	if n.Caption != "" {
		m["Caption"] = n.Caption
	}
	ast.DumpHelper(n, source, level, m, nil)
}

// KindRefNode is the ast.NodeKind for RefNode.
var KindRefNode = ast.NewNodeKind("RefNode")

// refPattern matches :ref[Kind:name] and :ref[Kind:name]{caption="..."} syntax.
// Group 1: Kind, Group 2: name, Group 3 (optional): caption value
var refPattern = regexp.MustCompile(`^:ref\[([A-Za-z]+):([A-Za-z0-9_-]+)\](?:\{caption="([^"]*)"\})?`)

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

// Parse parses :ref[Kind:name] and :ref[Kind:name]{caption="..."} syntax.
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
	caption := ""
	if len(match) > 3 && match[3] != nil {
		caption = string(match[3])
	}

	// Advance the reader past the matched content
	block.Advance(len(match[0]))
	_ = segment // suppress unused warning

	node := &RefNode{
		RefKind: kind,
		RefName: name,
		Caption: caption,
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

	// Wrap in figure if caption is present
	if n.Caption != "" {
		_, _ = w.WriteString(`<figure class="bn-figure">`)
	}

	_, _ = w.WriteString(`<bn-ref kind="`)
	_, _ = w.WriteString(n.RefKind)
	_, _ = w.WriteString(`" name="`)
	_, _ = w.WriteString(n.RefName)
	_, _ = w.WriteString(`"></bn-ref>`)

	// Close figure and add caption
	if n.Caption != "" {
		_, _ = w.WriteString(`<figcaption>`)
		// HTML escape the caption
		for _, r := range n.Caption {
			switch r {
			case '&':
				_, _ = w.WriteString("&amp;")
			case '<':
				_, _ = w.WriteString("&lt;")
			case '>':
				_, _ = w.WriteString("&gt;")
			case '"':
				_, _ = w.WriteString("&quot;")
			default:
				_, _ = w.WriteRune(r)
			}
		}
		_, _ = w.WriteString(`</figcaption></figure>`)
	}

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
