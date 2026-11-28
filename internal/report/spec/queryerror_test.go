package spec

import "testing"

func TestParseDuckDBError(t *testing.T) {
	msg, line, ok := ParseDuckDBError("query: Parser Error: syntax error at or near \"FORM\"\n\nLINE 3: FORM x\n        ^")
	if !ok || line != 3 || msg != "Parser Error: syntax error at or near \"FORM\"" {
		t.Errorf("got %q %d %v", msg, line, ok)
	}
	if _, _, ok := ParseDuckDBError("context deadline exceeded"); ok {
		t.Error("a timeout is not a DuckDB error")
	}
}

func TestQueryErrorLine(t *testing.T) {
	block := OutlineBlock{Field: "query", Content: Range{StartLine: 5, StartCol: 5, EndLine: 8, EndCol: 10}}
	cases := []struct {
		n, want int
		folded  bool
	}{
		{0, 5, false},
		{1, 5, false},
		{3, 7, false},
		{9, 5, false}, // past the block
		{3, 5, true},  // a folded block joins its lines
	}
	for _, c := range cases {
		b := block
		b.Folded = c.folded
		if got := QueryErrorLine(b, c.n); got != c.want {
			t.Errorf("QueryErrorLine(n=%d, folded=%v) = %d, want %d", c.n, c.folded, got, c.want)
		}
	}
}
