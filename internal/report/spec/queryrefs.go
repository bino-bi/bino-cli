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

// QueryRefs returns every use of name as a table in DataSet queries: inline
// query/prql scalars and the files their `$file:` form points to. SQL reads a
// DataSource or DataSet by its bare name, so a rename must rewrite these too.
// readFile loads a `$file:` target (open buffer or disk); a file shared by
// several DataSets is scanned once, and a file that cannot be read is skipped
// and returned in skipped. It fails when a use sits in a scalar whose source
// text cannot be mapped back safely (quoted with escapes, or a multi-line flow
// scalar).
func (idx *NameIndex) QueryRefs(name string, readFile func(path string) (string, bool)) (out []SymbolRef, skipped []string, err error) {
	seen := make(map[string]bool)
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
			if seen[path] {
				continue
			}
			seen[path] = true
			content, ok := readFile(path)
			if !ok {
				skipped = append(skipped, path)
				continue
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
				return nil, nil, fmt.Errorf("the %s at %s:%d uses %q, but its YAML quoting cannot be edited safely; "+
					"write it as a block scalar (%s: |) or rename it there by hand", q.field, q.file, q.node.Line, name, q.field)
			}
			out = appendUses(out, q.file, region, line, col, got, name)
		default:
		}
	}
	return out, skipped, nil
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

// queryToken is a word, a quoted identifier or a punctuation byte of a query.
type queryToken struct {
	text   string // word or identifier content; the byte for punctuation
	off    int    // byte offset of text in the query
	word   bool   // bare word
	quoted bool   // quoted identifier ("x" in SQL, `x` in PRQL)
}

// tokenizeQuery splits a query into tokens. It skips string literals ('...',
// and "..." in PRQL) and comments (-- and /* */ in SQL, # in PRQL). A bare
// word may contain '-', as manifest names do.
func tokenizeQuery(text string, prql bool) []queryToken {
	identQuote := byte('"')
	if prql {
		identQuote = '`'
	}
	var out []queryToken
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || prql && c == '"':
			end := strings.IndexByte(text[i+1:], c)
			if end < 0 {
				return out
			}
			out = append(out, queryToken{text: "'", off: i})
			i += end + 2
		case c == identQuote:
			end := strings.IndexByte(text[i+1:], c)
			if end < 0 {
				return out
			}
			out = append(out, queryToken{text: text[i+1 : i+1+end], off: i + 1, quoted: true})
			i += end + 2
		case !prql && strings.HasPrefix(text[i:], "--"), prql && c == '#':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return out
			}
			i += end
		case !prql && strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 4
		case isNameByte(c) && c != '-':
			j := i
			for j < len(text) && isNameByte(text[j]) && (prql || !strings.HasPrefix(text[j:], "--")) {
				j++
			}
			out = append(out, queryToken{text: text[i:j], off: i, word: true})
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			out = append(out, queryToken{text: string(c), off: i})
			i++
		}
	}
	return out
}

// sqlClauseWords end a FROM clause's comma-separated table list.
var sqlClauseWords = map[string]bool{
	"where": true, "group": true, "order": true, "having": true, "limit": true, "offset": true,
	"qualify": true, "window": true, "union": true, "except": true, "intersect": true,
	"select": true, "on": true, "using": true, "returning": true, "fetch": true, "set": true, "values": true,
}

// findNameUses returns the byte offsets of uses of name as a table in a
// query: the word after FROM or JOIN (in PRQL after `side:` arguments and an
// `alias =`; in SQL also further comma-separated tables of the same FROM
// clause), bare or quoted. When the query reads the table, a qualifier
// name.col is a use too. Column names, aliases, literals, comments and longer
// names (name_2024, t.name, @x/name) are not. A query that defines a CTE
// (or PRQL `let`) of that name shadows the table, so it has no uses.
func findNameUses(text, name string, prql bool) []int {
	if name == "" {
		return nil
	}
	toks := tokenizeQuery(text, prql)
	punct := func(i int, s string) bool {
		return i < len(toks) && !toks[i].word && !toks[i].quoted && toks[i].text == s
	}
	ident := func(i int) bool { return i < len(toks) && (toks[i].word || toks[i].quoted) }
	isName := func(i int) bool {
		if !ident(i) || toks[i].text != name {
			return false
		}
		start := toks[i].off
		if toks[i].quoted {
			start--
		}
		return start == 0 || strings.IndexByte(".@/", text[start-1]) < 0
	}
	var uses []int
	hasTable := false
	fromList := []bool{false} // per SQL paren depth: inside a FROM table list
	// table handles the table position at j and returns the last index used.
	table := func(j int) int {
		if !ident(j) {
			return j - 1
		}
		if isName(j) && !punct(j+1, ".") {
			uses = append(uses, toks[j].off)
			hasTable = true
		}
		return j
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		lw := strings.ToLower(t.text)
		switch {
		case !prql && isName(i) && cteAfter(toks, i),
			prql && t.word && lw == "let" && isName(i+1) && punct(i+2, "="):
			return nil
		case t.word && (lw == "from" || lw == "join"):
			j := i + 1
			if prql {
			skip:
				for {
					switch {
					case ident(j) && punct(j+1, ":") && ident(j+2):
						j += 3 // side:left
					case ident(j) && punct(j+1, "=") && !punct(j+2, "="):
						j += 2 // alias =
					default:
						break skip
					}
				}
			} else if lw == "from" {
				fromList[len(fromList)-1] = true
			}
			i = table(j)
		case isName(i) && punct(i+1, "."):
			uses = append(uses, t.off)
		case prql: // the cases below track SQL FROM lists
		case punct(i, "("):
			fromList = append(fromList, false)
		case punct(i, ")"):
			if len(fromList) > 1 {
				fromList = fromList[:len(fromList)-1]
			}
		case punct(i, ",") && fromList[len(fromList)-1]:
			i = table(i + 1)
		case t.word && sqlClauseWords[lw]:
			fromList[len(fromList)-1] = false
		}
	}
	if !hasTable {
		return nil
	}
	return uses
}

// cteAfter reports whether toks[i] starts a SQL CTE definition: a name, an
// optional column list, then AS (.
func cteAfter(toks []queryToken, i int) bool {
	punct := func(j int, s string) bool {
		return j < len(toks) && !toks[j].word && !toks[j].quoted && toks[j].text == s
	}
	j := i + 1
	if punct(j, "(") {
		for depth := 0; j < len(toks); j++ {
			if punct(j, "(") {
				depth++
			} else if punct(j, ")") {
				depth--
				if depth == 0 {
					j++
					break
				}
			}
		}
	}
	return j < len(toks) && toks[j].word && strings.EqualFold(toks[j].text, "as") && punct(j+1, "(")
}

// isNameByte reports whether b can continue a manifest name (letters, digits,
// '_', '-') or is part of a multi-byte rune.
func isNameByte(b byte) bool {
	return b == '_' || b == '-' || b >= 0x80 ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}
