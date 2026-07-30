package lsp

import (
	"strings"
	"testing"

	"go.lsp.dev/protocol"

	reportspec "bino.bi/bino/internal/report/spec"
)

// The fixture mixes ASCII, a 2-byte umlaut (1 UTF-16 unit), and a 4-byte emoji
// (2 UTF-16 units) so byte/rune/UTF-16 confusion cannot cancel out.
const utf16Doc = "kind: Text\n" +
	"metadata:\n" +
	"  name: über_text\n" + // ü: byte col ≠ rune col
	"spec:\n" +
	"  value: Überschrift 🙂 Quartal\n" // emoji: rune col ≠ UTF-16 col

func TestPositionToLineCol_UTF16(t *testing.T) {
	d := &Document{Text: utf16Doc}
	cases := []struct {
		name     string
		pos      protocol.Position
		wantLine int
		wantCol  int
	}{
		{"ascii line", protocol.Position{Line: 0, Character: 6}, 1, 7},
		// "  name: über_text": cursor after "ü" — 1 UTF-16 unit, 1 rune.
		{"after umlaut", protocol.Position{Line: 2, Character: 9}, 3, 10},
		// value line: "  value: Überschrift 🙂 Quartal"
		// cursor after the emoji: UTF-16 counts it as 2 units, runes as 1.
		{"after emoji", protocol.Position{Line: 4, Character: 23}, 5, 23},
		{"past EOL clamps monotonically", protocol.Position{Line: 0, Character: 50}, 1, 51},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, col := d.PositionToLineCol(tc.pos)
			if line != tc.wantLine || col != tc.wantCol {
				t.Errorf("PositionToLineCol(%+v) = (%d,%d), want (%d,%d)", tc.pos, line, col, tc.wantLine, tc.wantCol)
			}
		})
	}
}

func TestRangeToProtocol_RoundTrip(t *testing.T) {
	d := &Document{Text: utf16Doc}
	// The word "Quartal" on the value line: the prefix
	// "  value: Überschrift 🙂 " is 23 runes → Quartal starts at rune col 24.
	r := reportspec.Range{StartLine: 5, StartCol: 24, EndLine: 5, EndCol: 31}
	pr := d.RangeToProtocol(r)
	// UTF-16: same prefix is 24 units (emoji counts 2) → character 24.
	if pr.Start.Character != 24 || pr.End.Character != 31 {
		t.Fatalf("RangeToProtocol = %+v, want characters 24..31", pr)
	}
	// Round-trip: the protocol position resolves back to the same rune column.
	line, col := d.PositionToLineCol(pr.Start)
	if line != 5 || col != 24 {
		t.Fatalf("round trip = (%d,%d), want (5,24)", line, col)
	}
	// The span must select exactly "Quartal" in the buffer.
	text, _ := d.lineText(5)
	runes := []rune(text)
	if got := string(runes[23:30]); got != "Quartal" {
		t.Fatalf("rune span selects %q, want Quartal", got)
	}
}

func TestOffsetToPosition_UTF16(t *testing.T) {
	d := &Document{Text: utf16Doc}
	// Byte offset of "Quartal" on the last content line.
	off := strings.Index(d.Text, "Quartal")
	pos := d.OffsetToPosition(off)
	if pos.Line != 4 || pos.Character != 24 {
		t.Fatalf("OffsetToPosition = %+v, want line 4 char 24 (UTF-16)", pos)
	}
}

func TestLineSpan_UTF16End(t *testing.T) {
	d := &Document{Text: utf16Doc}
	// Span from rune col 1 to end of the value line: 30 runes → 31 UTF-16 units.
	r := lineSpan(d, 5, 1)
	if r.Start.Character != 0 {
		t.Errorf("start character = %d, want 0", r.Start.Character)
	}
	if r.End.Character != 31 {
		t.Errorf("end character = %d, want 31 (UTF-16 width of the line)", r.End.Character)
	}
}

func TestEnvVarDiagnostics_AfterUmlaut(t *testing.T) {
	doc := &Document{Text: "kind: Text\nspec:\n  value: Ü ${MISSING_VAR} end\n"}
	out := envVarDiagnostics(doc, Diag{
		Severity: "warning",
		Message:  "Unresolved environment variable: MISSING_VAR",
		Code:     "missing-env-var",
	})
	if len(out) != 1 {
		t.Fatalf("expected 1 anchored diagnostic, got %d", len(out))
	}
	// "  value: Ü " before ${ — 11 runes, 11 UTF-16 units (Ü is BMP), but 12 bytes.
	if got := out[0].Range.Start.Character; got != 11 {
		t.Fatalf("env-var anchor character = %d, want 11 (UTF-16, not bytes)", got)
	}
}

func TestCompletion_RefTextEditAfterUmlaut(t *testing.T) {
	// A ref value on a line whose prefix contains no umlauts is byte==utf16;
	// this doc puts the DIFFERENCE in front: a name containing an umlaut is
	// referenced, so the TextEdit range must be measured in UTF-16.
	s := newTestServer()
	doc := "kind: Table\nmetadata:\n  name: über_tabelle\nspec:\n  dataset: sales\n"
	u := openDoc(t, s, doc)
	res, err := s.Completion(t.Context(), completionParams(u, 4, 12))
	if err != nil {
		t.Fatal(err)
	}
	labels := completionLabels(t, res)
	if !contains(labels, "sales") {
		t.Fatalf("dataset completion should still work on a document with umlauts (got %v)", labels)
	}
}

func TestCRLFLineWidth(t *testing.T) {
	d := &Document{Text: "kind: Table\r\nmetadata:\r\n"}
	text, ok := d.lineText(1)
	if !ok || text != "kind: Table" {
		t.Fatalf("lineText must exclude the trailing CR, got %q", text)
	}
	r := lineSpan(d, 1, 1)
	if r.End.Character != 11 {
		t.Fatalf("CRLF line width = %d, want 11", r.End.Character)
	}
}
