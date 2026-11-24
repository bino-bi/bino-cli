package spec

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// queryBlock is a DataSet's `query:` or `prql:` value, inline or `$file:`.
type queryBlock struct {
	file  string
	field string // "query" or "prql"
	node  *yaml.Node
}

func (idx *NameIndex) recordQueries(file string, spec *yaml.Node) {
	for _, field := range []string{"query", "prql"} {
		if n := mappingChild(spec, field); n != nil {
			idx.queries = append(idx.queries, queryBlock{file: file, field: field, node: n})
		}
	}
}

// recordInlineDatasetQueries records the queries of inline DataSets bound
// through a `dataset:` field (a mapping, or mappings in a list).
func (idx *NameIndex) recordInlineDatasetQueries(file string, val *yaml.Node) {
	switch val.Kind {
	case yaml.MappingNode:
		idx.recordQueries(file, val)
	case yaml.SequenceNode:
		for _, e := range val.Content {
			if e.Kind == yaml.MappingNode {
				idx.recordQueries(file, e)
			}
		}
	default:
	}
}

// QueryRefs returns every whole-word use of name in DataSet queries: inline
// query/prql scalars and the files their `$file:` form points to. SQL reads a
// DataSource or DataSet by its bare name, so a rename must rewrite these too.
// readFile loads a `$file:` target (open buffer or disk). It fails when a use
// sits in a scalar whose source text cannot be mapped back safely (quoted with
// escapes, or a multi-line flow scalar), or when a query file cannot be read.
func (idx *NameIndex) QueryRefs(name string, readFile func(path string) (string, bool)) ([]SymbolRef, error) {
	var out []SymbolRef
	for _, q := range idx.queries {
		prql := q.field == "prql"
		switch q.node.Kind {
		case yaml.MappingNode:
			rel := mappingChildValue(q.node, "$file")
			if rel == "" {
				continue
			}
			path := rel
			if !filepath.IsAbs(path) {
				path = filepath.Join(filepath.Dir(q.file), rel)
			}
			content, ok := readFile(path)
			if !ok {
				return nil, fmt.Errorf("cannot read query file %s (from %s), so its uses of %q cannot be renamed", path, q.file, name)
			}
			out = appendUses(out, path, content, 1, 1, findNameUses(content, name, prql), name)
		case yaml.ScalarNode:
			want := findNameUses(q.node.Value, name, prql)
			if len(want) == 0 {
				continue
			}
			region, line, col, ok := scalarSource(idx.files[q.file], q.node)
			var got []int
			if ok {
				got = findNameUses(region, name, prql)
			}
			if !ok || len(got) != len(want) {
				return nil, fmt.Errorf("the %s at %s:%d uses %q, but its YAML quoting cannot be edited safely; "+
					"write it as a block scalar (%s: |) or rename it there by hand", q.field, q.file, q.node.Line, name, q.field)
			}
			out = appendUses(out, q.file, region, line, col, got, name)
		default:
		}
	}
	return out, nil
}

// scalarSource returns the source text a scalar's value is written in, with
// the 1-based rune position of its first character. Block scalars (| and >)
// map line by line; a single-line plain or quoted scalar maps only when the
// source text equals the value (no escapes).
func scalarSource(content string, n *yaml.Node) (region string, line, col int, ok bool) {
	lines := strings.Split(content, "\n")
	if n.Line < 1 || n.Line > len(lines) {
		return "", 0, 0, false
	}
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		start := n.Line // 0-based index of the first content line
		indent := -1
		end := start
		for i := start; i < len(lines); i++ {
			l := strings.TrimRight(lines[i], "\r")
			if strings.TrimSpace(l) == "" {
				end = i + 1
				continue
			}
			lead := len(l) - len(strings.TrimLeft(l, " "))
			if indent < 0 {
				indent = lead
			}
			if lead < indent || lead == 0 {
				break
			}
			end = i + 1
		}
		if indent <= 0 {
			return "", 0, 0, false
		}
		return strings.Join(lines[start:end], "\n"), n.Line + 1, 1, true
	}
	if strings.Contains(n.Value, "\n") {
		return "", 0, 0, false
	}
	col = n.Column
	if n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		col++
	}
	src := lines[n.Line-1]
	off := 0
	for c := 1; c < col; c++ {
		if off >= len(src) {
			return "", 0, 0, false
		}
		_, size := utf8.DecodeRuneInString(src[off:])
		off += size
	}
	if !strings.HasPrefix(src[off:], n.Value) {
		return "", 0, 0, false
	}
	return n.Value, n.Line, col, true
}

// appendUses converts byte offsets in region (which starts at line:col) into
// rune-column ranges of name.
func appendUses(out []SymbolRef, file, region string, line, col int, offsets []int, name string) []SymbolRef {
	width := utf8.RuneCountInString(name)
	for _, off := range offsets {
		l, c := line, col
		before := region[:off]
		if nl := strings.LastIndexByte(before, '\n'); nl >= 0 {
			l += strings.Count(before, "\n")
			c = 1
			before = before[nl+1:]
		}
		c += utf8.RuneCountInString(before)
		out = append(out, SymbolRef{
			File: file, Range: Range{StartLine: l, StartCol: c, EndLine: l, EndCol: c + width},
			TargetName: name, Field: "query",
		})
	}
	return out
}

// findNameUses returns the byte offsets of whole-word uses of name in a query,
// bare or inside a quoted identifier ("name" or `name`). It skips string
// literals ('...') and line comments (-- in SQL, # in PRQL), and a use that is
// qualified (t.name) or part of a longer name (name_2024, my-name, @x/name).
func findNameUses(text, name string, prql bool) []int {
	if name == "" {
		return nil
	}
	var out []int
	inStr, inComment := false, false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case inComment:
			inComment = ch != '\n'
			continue
		case inStr:
			inStr = ch != '\''
			continue
		case ch == '\'':
			inStr = true
			continue
		case !prql && ch == '-' && strings.HasPrefix(text[i:], "--"), prql && ch == '#':
			inComment = true
			continue
		}
		if !strings.HasPrefix(text[i:], name) {
			continue
		}
		if i > 0 && (isNameByte(text[i-1]) || strings.IndexByte(".@/", text[i-1]) >= 0) {
			continue
		}
		end := i + len(name)
		if end < len(text) && isNameByte(text[end]) {
			continue
		}
		out = append(out, i)
		i = end - 1
	}
	return out
}

// isNameByte reports whether b can continue a manifest name (letters, digits,
// '_', '-') or is part of a multi-byte rune.
func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b >= 0x80 ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
