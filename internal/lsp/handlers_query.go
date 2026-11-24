package lsp

//go:generate go run gen_sqlwords.go

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.lsp.dev/protocol"

	reportspec "bino.bi/bino/internal/report/spec"
)

// hoverDataBudget bounds how long a hover waits for a cold dataset sample.
// Hover is not on the typing path, so it may wait longer than completion.
const hoverDataBudget = time.Second

// queryScan is what a query reads like up to the cursor.
type queryScan struct {
	inLiteral bool   // in a string literal or comment
	quoted    bool   // in a quoted identifier ("x" in SQL, `x` in PRQL)
	partial   string // the identifier being typed
	prev      string // the token before it, lowercased
}

// afterTable reports whether the cursor names a table: the word after FROM or
// JOIN (SQL and PRQL alike).
func (q queryScan) afterTable() bool {
	return !q.inLiteral && (q.prev == "from" || q.prev == "join")
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// scanQuery tokenizes query text that ends at the cursor. It skips string
// literals and comments (-- and /* */ in SQL, # in PRQL).
func scanQuery(text string, prql bool) queryScan {
	identQuote := byte('"')
	if prql {
		identQuote = '`'
	}
	var last string // the last complete token
	push := func(tok string) { last = strings.ToLower(tok) }
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '\'' || prql && c == '"':
			end := strings.IndexByte(text[i+1:], c)
			if end < 0 {
				return queryScan{inLiteral: true}
			}
			push("'")
			i += end + 2
		case c == identQuote:
			end := strings.IndexByte(text[i+1:], c)
			if end < 0 {
				return queryScan{quoted: true, partial: text[i+1:], prev: last}
			}
			push(text[i+1 : i+1+end])
			i += end + 2
		case !prql && strings.HasPrefix(text[i:], "--"), prql && c == '#':
			end := strings.IndexByte(text[i:], '\n')
			if end < 0 {
				return queryScan{inLiteral: true}
			}
			i += end
		case !prql && strings.HasPrefix(text[i:], "/*"):
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return queryScan{inLiteral: true}
			}
			i += end + 4
		case isIdentByte(c):
			j := i
			for j < len(text) && isIdentByte(text[j]) {
				j++
			}
			if j == len(text) {
				return queryScan{partial: text[i:], prev: last}
			}
			push(text[i:j])
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			push(string(c))
			i++
		}
	}
	return queryScan{prev: last}
}

// queryPrefix returns the query text from the start of the cursor's
// query/prql block up to the cursor (1-based line, rune col), and the name of
// the DataSet that owns the block. When the outline cannot map the block (a
// quoted scalar with escapes) it falls back to the cursor line.
func queryPrefix(doc *Document, line, col int, field string) (prefix, owner string) {
	for _, d := range reportspec.Outline(doc.Text) {
		if line < d.Range.StartLine || line > d.Range.EndLine {
			continue
		}
		for _, b := range d.Blocks {
			if b.Field != field || b.Content.StartLine > line {
				continue
			}
			var sb strings.Builder
			for l := b.Content.StartLine; l <= line; l++ {
				text, _ := doc.lineText(l)
				start, end := 0, len(text)
				if l == b.Content.StartLine {
					start = runeByteOffset(text, b.Content.StartCol)
				}
				if l == line {
					end = runeByteOffset(text, col)
				}
				if start < end {
					sb.WriteString(text[start:end])
				}
				if l < line {
					sb.WriteByte('\n')
				}
			}
			return sb.String(), d.Name
		}
		break
	}
	text, _ := doc.lineText(line)
	return text[:runeByteOffset(text, col)], ""
}

// runeByteOffset is the byte offset of 1-based rune column col in s, clamped.
func runeByteOffset(s string, col int) int {
	off := 0
	for c := 1; c < col && off < len(s); c++ {
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
	}
	return off
}

// queryTableAt returns the table name under the cursor when it follows FROM
// or JOIN: the scanned identifier extended to the end of the word.
func queryTableAt(doc *Document, pc reportspec.PositionContext, line, col int) (string, bool) {
	prefix, _ := queryPrefix(doc, line, col, pc.FieldName)
	sc := scanQuery(prefix, pc.FieldName == "prql")
	if !sc.afterTable() {
		return "", false
	}
	text, _ := doc.lineText(line)
	rest := text[runeByteOffset(text, col):]
	if sc.quoted {
		q := "\""
		if pc.FieldName == "prql" {
			q = "`"
		}
		if i := strings.Index(rest, q); i >= 0 {
			rest = rest[:i]
		}
	} else {
		i := 0
		for i < len(rest) && isIdentByte(rest[i]) {
			i++
		}
		rest = rest[:i]
	}
	name := sc.partial + rest
	return name, name != ""
}

// completeQuery serves a cursor inside a DataSet query/prql block: DataSet and
// DataSource names after FROM/JOIN, else the bound datasets' columns plus (for
// SQL) DuckDB keywords and functions.
func (s *Server) completeQuery(ctx context.Context, doc *Document, pc reportspec.PositionContext, line, col int) ([]protocol.CompletionItem, bool) {
	prql := pc.FieldName == "prql"
	prefix, owner := queryPrefix(doc, line, col, pc.FieldName)
	sc := scanQuery(prefix, prql)
	if sc.inLiteral {
		return nil, false
	}
	if sc.afterTable() {
		idx := s.getIndex(ctx)
		return completeTableNames(idx, owner, sc.quoted), idx == nil
	}
	cols := s.unionColumns(ctx, pc.BoundDatasets)
	items := completeColumns(cols)
	if !prql && !sc.quoted {
		items = append(items, sqlWordItems()...)
	}
	// Not warm yet: re-query once introspection lands.
	return items, cols == nil && len(pc.BoundDatasets) > 0
}

var plainIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// completeTableNames offers the project's DataSets and DataSources, except the
// DataSet being edited. Names that are not plain identifiers are inserted
// quoted unless the cursor already is inside quotes.
func completeTableNames(index []IndexDoc, owner string, quoted bool) []protocol.CompletionItem {
	seen := map[string]bool{owner: true}
	items := make([]protocol.CompletionItem, 0, len(index))
	for _, d := range index {
		if (d.Kind != "DataSet" && d.Kind != "DataSource") || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		kind := protocol.CompletionItemKindStruct
		if d.Kind == "DataSet" {
			kind = protocol.CompletionItemKindClass
		}
		item := protocol.CompletionItem{
			Label:    d.Name,
			Kind:     kind,
			Detail:   protocol.NewOptional(d.Kind),
			SortText: protocol.NewOptional("0" + d.Name),
		}
		if !quoted && !plainIdent.MatchString(d.Name) {
			item.InsertText = protocol.NewOptional(`"` + d.Name + `"`)
		}
		items = append(items, item)
	}
	return items
}

// sqlWordItems are the DuckDB keyword and function items, sorted after columns.
var sqlWordItems = sync.OnceValue(func() []protocol.CompletionItem {
	items := make([]protocol.CompletionItem, 0, len(duckdbKeywords)+len(duckdbFunctions))
	for _, k := range duckdbKeywords {
		items = append(items, protocol.CompletionItem{
			Label: k, Kind: protocol.CompletionItemKindKeyword, SortText: protocol.NewOptional("2" + k),
		})
	}
	for _, f := range duckdbFunctions {
		items = append(items, protocol.CompletionItem{
			Label: f, Kind: protocol.CompletionItemKindFunction, SortText: protocol.NewOptional("3" + f),
		})
	}
	return items
})

// queryDefinition jumps from a table name after FROM/JOIN to its DataSet or
// DataSource (the DataSet wins, as in the query engine).
func (s *Server) queryDefinition(ctx context.Context, doc *Document, pc reportspec.PositionContext, line, col int) (protocol.DefinitionResult, error) {
	name, ok := queryTableAt(doc, pc, line, col)
	if !ok {
		return nil, nil
	}
	ni := s.getNameIndex(ctx)
	for _, kind := range []string{"DataSet", "DataSource"} {
		if def, found := ni.Definition(kind, name); found {
			return protocol.LocationSlice{s.locationOf(def.File, def.NameRange)}, nil
		}
	}
	return nil, nil
}

// queryHover shows the data of a table named after FROM/JOIN.
func (s *Server) queryHover(ctx context.Context, doc *Document, pc reportspec.PositionContext, line, col int) string {
	name, ok := queryTableAt(doc, pc, line, col)
	if !ok {
		return ""
	}
	known := false
	for _, d := range s.getIndex(ctx) {
		if d.Name == name && (d.Kind == "DataSet" || d.Kind == "DataSource") {
			known = true
			break
		}
	}
	if !known {
		return ""
	}
	return s.datasetHover(ctx, name)
}

// datasetHover renders a dataset's columns with types and its first rows,
// falling back to the bare column list when no sample arrives in time.
func (s *Server) datasetHover(ctx context.Context, ref string) string {
	hctx, cancel := context.WithTimeout(ctx, hoverDataBudget)
	defer cancel()
	name := strings.TrimPrefix(ref, "$")
	if smp, ok := s.data.sample(hctx, s.backend, name); ok {
		return sampleMarkdown(ref, smp)
	}
	cols, ok := s.data.columns(hctx, s.backend, name)
	if !ok || len(cols) == 0 {
		return ""
	}
	return "**" + ref + "** columns:\n\n" + "`" + strings.Join(cols, "`, `") + "`"
}

// sampleCellWidth caps a rendered sample value, in runes.
const sampleCellWidth = 24

// sampleMarkdown renders a sample as a markdown table whose header carries the
// column types.
func sampleMarkdown(ref string, smp DatasetSample) string {
	var b strings.Builder
	b.WriteString("**" + mdCell(ref) + "**")
	if smp.Kind != "" {
		b.WriteString(" · " + smp.Kind)
	}
	fmt.Fprintf(&b, " · %d columns\n\n", len(smp.Columns))
	if len(smp.Columns) == 0 {
		return b.String()
	}
	b.WriteString("|")
	for i, c := range smp.Columns {
		cell := mdCell(c)
		if i < len(smp.Types) && smp.Types[i] != "" {
			cell += " (" + mdCell(smp.Types[i]) + ")"
		}
		b.WriteString(" " + cell + " |")
	}
	b.WriteString("\n|" + strings.Repeat("---|", len(smp.Columns)) + "\n")
	for _, row := range smp.Rows {
		b.WriteString("|")
		for i, c := range smp.Columns {
			v := formatSampleValue(row[c])
			if i < len(smp.Types) && smp.Types[i] == "DATE" {
				v = strings.TrimSuffix(v, "T00:00:00Z") // a DATE that went through JSON
			}
			b.WriteString(" " + mdCell(truncateRunes(v, sampleCellWidth)) + " |")
		}
		b.WriteString("\n")
	}
	if len(smp.Rows) == 0 {
		b.WriteString("\nNo rows.\n")
	}
	return b.String()
}

func formatSampleValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case time.Time:
		if x.Equal(x.Truncate(24 * time.Hour)) {
			return x.Format(time.DateOnly)
		}
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(v)
	}
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

var mdEscaper = strings.NewReplacer(
	"\\", "\\\\", "|", "\\|", "*", "\\*", "_", "\\_", "`", "\\`", "<", "\\<", "[", "\\[",
	"\r\n", " ", "\n", " ", "\r", " ",
)

// mdCell escapes text for a markdown table cell.
func mdCell(s string) string { return mdEscaper.Replace(s) }
