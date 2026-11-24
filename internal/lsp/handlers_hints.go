package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
)

// maxHintValue caps a shown value; the tooltip names the source, not the value.
const maxHintValue = 40

// InlayHint shows what each ${NAME} in range resolves to: the default of a
// param the document declares, else the variable from the environment or
// bino.toml [build.env], in the order the build looks them up. Unset
// names get no hint (the missing-env-var diagnostic covers them), and values
// of secret-looking names are masked.
func (s *Server) InlayHint(_ context.Context, params *protocol.InlayHintParams) ([]protocol.InlayHint, error) {
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok || !isBinoBuffer(doc.Text) {
		return nil, nil
	}
	nodes, _ := reportspec.ParseYAMLNodes(doc.Text) //nolint:errcheck // lenient parse; a broken buffer still gets env hints
	docParams := make([][]config.LayoutPageParamSpec, len(nodes))
	for i, n := range nodes {
		if n != nil {
			docParams[i], _ = decodeDocParams(n)
		}
	}
	lookup := s.varLookup()

	var hints []protocol.InlayHint
	for _, m := range config.VarRefs(doc.Text) {
		end := doc.OffsetToPosition(m[1])
		if !inRange(end, params.Range) {
			continue
		}
		name := doc.Text[m[2]:m[3]]
		line := int(end.Line) + 1
		value, source, ok := resolveVar(name, paramsAtLine(nodes, docParams, line), lookup)
		if !ok {
			continue
		}
		if secretName(name) {
			value = "****"
		}
		hints = append(hints, protocol.InlayHint{
			Position:    end,
			Label:       protocol.String("= " + shortValue(value)),
			Tooltip:     protocol.String(name + " from " + source),
			PaddingLeft: boolPtr(true),
		})
	}
	return hints, nil
}

// varSource is one place a ${NAME} can resolve from.
type varSource struct {
	name   string
	lookup config.LookupFunc
}

// varLookup returns the build's variable sources after params: the process
// environment, then bino.toml [build.env] (environment wins, see
// CommandEnv.Apply). The environment is the language server's own.
func (s *Server) varLookup() []varSource {
	out := []varSource{{"the environment", config.EnvLookup()}}
	if s.root == "" {
		return out
	}
	if cfg, err := pathutil.LoadProjectConfig(s.root); err == nil && len(cfg.Build.Env.Values) > 0 {
		out = append(out, varSource{"bino.toml [build.env]", config.MapLookup(cfg.Build.Env.Values)})
	}
	return out
}

// resolveVar resolves name against the document's params, then the variable
// sources. A declared param without a default gets its value from the caller,
// so it has nothing to show.
func resolveVar(name string, params []config.LayoutPageParamSpec, sources []varSource) (value, source string, ok bool) {
	for _, p := range params {
		if p.Name == name {
			if p.Default == nil {
				return "", "", false
			}
			return *p.Default, "the default of param " + name, true
		}
		if p.Type == "select" && p.Name+"_LABEL" == name && p.Default != nil && p.Options != nil {
			for _, it := range p.Options.Items {
				if it.Value == *p.Default && it.Label != "" {
					return it.Label, "the default of param " + p.Name, true
				}
			}
		}
	}
	for _, src := range sources {
		if v, found := src.lookup(name); found {
			return v, src.name, true
		}
	}
	return "", "", false
}

// paramsAtLine returns the params of the document that contains line.
func paramsAtLine(nodes []*yaml.Node, params [][]config.LayoutPageParamSpec, line int) []config.LayoutPageParamSpec {
	at := -1
	for i, n := range nodes {
		if n == nil {
			continue
		}
		if at >= 0 && n.Line > line {
			break
		}
		at = i
	}
	if at < 0 {
		return nil
	}
	return params[at]
}

// secretName reports whether a variable name looks like it holds a secret.
func secretName(name string) bool {
	u := strings.ToUpper(name)
	for _, w := range []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "KEY", "CREDENTIAL", "PRIVATE"} {
		if strings.Contains(u, w) {
			return true
		}
	}
	return false
}

func shortValue(v string) string {
	v = strings.ReplaceAll(v, "\n", " ")
	if utf8.RuneCountInString(v) > maxHintValue {
		v = string([]rune(v)[:maxHintValue]) + "..."
	}
	if v == "" {
		return `""`
	}
	return v
}

func inRange(p protocol.Position, r protocol.Range) bool {
	after := p.Line > r.Start.Line || (p.Line == r.Start.Line && p.Character >= r.Start.Character)
	before := p.Line < r.End.Line || (p.Line == r.End.Line && p.Character <= r.End.Character)
	return after && before
}

// linkKeys are the manifest fields whose value is a file path relative to the
// manifest: $file query references, Asset localPath, DataSource and PEM path,
// DocumentArtefact sources file and stylesheet.
var linkKeys = map[string]bool{"$file": true, "localPath": true, "path": true, "file": true, "stylesheet": true}

// DocumentLink links file-path values to their file, resolved against the
// manifest's directory. Only existing regular files are linked, so URLs,
// globs, directories and unresolved ${VAR} paths are left alone.
func (s *Server) DocumentLink(_ context.Context, params *protocol.DocumentLinkParams) ([]protocol.DocumentLink, error) {
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok || doc.Path == "" || !isBinoBuffer(doc.Text) {
		return nil, nil
	}
	nodes, _ := reportspec.ParseYAMLNodes(doc.Text) //nolint:errcheck // lenient parse; unparsable documents contribute no links
	dir := filepath.Dir(doc.Path)
	var links []protocol.DocumentLink
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if v := n.Content[i+1]; linkKeys[n.Content[i].Value] && v.Kind == yaml.ScalarNode {
					if l, ok := fileLink(doc, dir, v); ok {
						links = append(links, l)
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return links, nil
}

func fileLink(doc *Document, dir string, v *yaml.Node) (protocol.DocumentLink, bool) {
	p := v.Value
	if p == "" || strings.Contains(p, "://") || strings.ContainsAny(p, "*?[") || strings.Contains(p, "${") {
		return protocol.DocumentLink{}, false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return protocol.DocumentLink{}, false
	}
	r, ok := scalarRange(doc, v)
	if !ok {
		return protocol.DocumentLink{}, false
	}
	if v.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		r.Start.Character++
		r.End.Character--
	}
	target := uri.File(p)
	return protocol.DocumentLink{Range: r, Target: &target}, true
}
