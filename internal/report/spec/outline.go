package spec

import (
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// OutlineDoc is one manifest document of a buffer with its source positions.
type OutlineDoc struct {
	APIVersion string
	Kind       string
	Name       string
	// Position is the 1-based ordinal among the buffer's non-empty mapping
	// documents, counted the way the manifest loader counts them.
	Position int
	Range    Range
	Blocks   []OutlineBlock
}

// OutlineBlock is the inline text of a DataSet's spec.query or spec.prql.
type OutlineBlock struct {
	Field string // "query" or "prql"
	// Content spans the query text in the source. For a block scalar StartCol
	// is the content indent column and the range covers whole lines.
	Content Range
}

// Outline lists the mapping documents of a buffer with their ranges and
// inline query blocks. A buffer that fails to parse as a whole is parsed one
// `---` slice at a time, so a document being typed does not hide the others.
func Outline(content string) []OutlineDoc {
	lines := strings.Split(content, "\n")
	nodes, err := ParseYAMLNodes(content)
	if err == nil {
		var out []OutlineDoc
		for i, n := range nodes {
			end := len(lines)
			for _, next := range nodes[i+1:] {
				if next != nil {
					end = next.Line - 1
					break
				}
			}
			if d, ok := outlineDoc(n, content, 0, end); ok {
				d.Position = len(out) + 1
				out = append(out, d)
			}
		}
		return out
	}

	var out []OutlineDoc
	for _, sl := range splitDocSlices(content) {
		if strings.TrimSpace(sl.text) == "" {
			continue
		}
		sub, _ := ParseYAMLNodes(sl.text) //nolint:errcheck // a broken slice yields no node and is skipped
		if len(sub) == 0 {
			continue
		}
		if d, ok := outlineDoc(sub[0], sl.text, sl.startLine-1, sl.endLine); ok {
			d.Position = len(out) + 1
			out = append(out, d)
		}
	}
	return out
}

// outlineDoc builds the outline of one document root parsed from src, whose
// first line is line offset+1 of the buffer. endLine is the buffer line the
// document ends on.
func outlineDoc(root *yaml.Node, src string, offset, endLine int) (OutlineDoc, bool) {
	if root == nil || root.Kind != yaml.MappingNode || len(root.Content) == 0 {
		return OutlineDoc{}, false
	}
	d := OutlineDoc{
		APIVersion: mappingChildValue(root, "apiVersion"),
		Kind:       docKind(root),
		Name:       mappingChildValue(mappingChild(root, "metadata"), "name"),
		Range:      Range{StartLine: root.Line + offset, StartCol: root.Column, EndLine: endLine, EndCol: 1},
	}
	if d.Kind == "DataSet" {
		spec := mappingChild(root, "spec")
		for _, field := range []string{"query", "prql"} {
			n := mappingChild(spec, field)
			if n == nil || n.Kind != yaml.ScalarNode {
				continue
			}
			if r, ok := scalarContentRange(src, n); ok {
				r.StartLine += offset
				r.EndLine += offset
				d.Blocks = append(d.Blocks, OutlineBlock{Field: field, Content: r})
			}
		}
	}
	return d, true
}

// scalarContentRange returns the source span of a scalar's text. A block
// scalar spans its content lines from the indent column, without trailing
// blank lines; other scalars map only when scalarSource can map them.
func scalarContentRange(src string, n *yaml.Node) (Range, bool) {
	region, line, col, ok := scalarSource(src, n)
	if !ok {
		return Range{}, false
	}
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		return Range{StartLine: line, StartCol: col, EndLine: line, EndCol: col + utf8.RuneCountInString(region)}, true
	}
	regionLines := strings.Split(region, "\n")
	last := len(regionLines) - 1
	for last >= 0 && strings.TrimSpace(regionLines[last]) == "" {
		last--
	}
	if last < 0 {
		return Range{}, false
	}
	first := 0
	for strings.TrimSpace(regionLines[first]) == "" {
		first++
	}
	indent := len(regionLines[first]) - len(strings.TrimLeft(regionLines[first], " "))
	lastLine := strings.TrimRight(regionLines[last], "\r")
	return Range{
		StartLine: line + first,
		StartCol:  indent + 1,
		EndLine:   line + last,
		EndCol:    utf8.RuneCountInString(lastLine) + 1,
	}, true
}
