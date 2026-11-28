package spec

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	duckdbErrorClass = regexp.MustCompile(`[A-Z][A-Za-z]*(?: [A-Z][A-Za-z]*)* Error: `)
	duckdbErrorLine  = regexp.MustCompile(`\bLINE (\d+):`)
)

// ParseDuckDBError extracts a DuckDB error ("Binder Error: ...") from an
// error text and the query line its "LINE n:" excerpt points at (0 when
// absent). Errors that are not DuckDB's (timeouts, unknown names) yield ok=false.
func ParseDuckDBError(text string) (msg string, line int, ok bool) {
	loc := duckdbErrorClass.FindStringIndex(text)
	if loc == nil {
		return "", 0, false
	}
	msg = text[loc[0]:]
	if m := duckdbErrorLine.FindStringSubmatchIndex(msg); m != nil {
		line, _ = strconv.Atoi(msg[m[2]:m[3]]) //nolint:errcheck // the pattern only matches digits
		msg = msg[:m[0]]
	}
	return strings.TrimSpace(msg), line, true
}

// QueryErrorLine maps query line n (1-based, 0 = unknown) to the source line
// of a query block. Only a literal block scalar keeps the query's line breaks;
// folded and single-line queries map to their first line.
func QueryErrorLine(b OutlineBlock, n int) int {
	c := b.Content
	if n > 1 && c.StartLine+n-1 <= c.EndLine && !b.Folded {
		return c.StartLine + n - 1
	}
	return c.StartLine
}
