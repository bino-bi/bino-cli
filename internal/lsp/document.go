package lsp

import (
	"sort"
	"strings"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	reportspec "bino.bi/bino/internal/report/spec"
)

// Document is one open editor buffer. Full document sync is used, so Text is the
// authoritative content after every didChange.
//
// Coordinate note: LSP positions are 0-based with UTF-16 character offsets;
// yaml.v3 (and the resolver) are 1-based with RUNE columns. All conversion is
// isolated in this file: incoming positions go through PositionToLineCol,
// outgoing ranges through RangeToProtocol / rangeToProtocolIn / OffsetToPosition.
type Document struct {
	URI     uri.URI
	Path    string
	Text    string
	Version int32

	mu          sync.Mutex
	lineOffsets []int // byte offset of each line start
}

// PositionToLineCol converts a 0-based UTF-16 LSP position to the resolver's
// 1-based (line, rune column).
func (d *Document) PositionToLineCol(p protocol.Position) (line, col int) {
	line = int(p.Line) + 1
	text, ok := d.lineText(line)
	if !ok {
		return line, int(p.Character) + 1
	}
	return line, runeColIn(text, int(p.Character))
}

// RangeToProtocol converts a 1-based rune-column spec.Range to a 0-based
// UTF-16 protocol.Range against this buffer's text.
func (d *Document) RangeToProtocol(r reportspec.Range) protocol.Range {
	return protocol.Range{
		Start: d.positionFor(r.StartLine, r.StartCol),
		End:   d.positionFor(r.EndLine, r.EndCol),
	}
}

func (d *Document) positionFor(line, col int) protocol.Position {
	text, ok := d.lineText(line)
	if !ok {
		return protocol.Position{Line: clampU32(line - 1), Character: clampU32(col - 1)}
	}
	return protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(text, col))}
}

// rangeToProtocolIn converts a rune-column spec.Range against arbitrary source
// text — for ranges targeting files that are not the current buffer (the name
// index's cross-file definitions/references).
func rangeToProtocolIn(text string, r reportspec.Range) protocol.Range {
	lines := strings.Split(text, "\n")
	pos := func(line, col int) protocol.Position {
		if line >= 1 && line <= len(lines) {
			lt := strings.TrimSuffix(lines[line-1], "\r")
			return protocol.Position{Line: clampU32(line - 1), Character: clampU32(utf16CharIn(lt, col))}
		}
		return protocol.Position{Line: clampU32(line - 1), Character: clampU32(col - 1)}
	}
	return protocol.Range{Start: pos(r.StartLine, r.StartCol), End: pos(r.EndLine, r.EndCol)}
}

// OffsetToPosition maps a byte offset in Text to a 0-based UTF-16 LSP
// position. Used by diagnostic backfill to anchor a ${VAR} occurrence found by
// byte scan, and for end-of-buffer insertion points.
func (d *Document) OffsetToPosition(offset int) protocol.Position {
	starts := d.lineStarts()
	// Find the last line start <= offset.
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > offset }) - 1
	if i < 0 {
		i = 0
	}
	if offset > len(d.Text) {
		offset = len(d.Text)
	}
	u16 := 0
	for _, r := range d.Text[starts[i]:offset] {
		u16 += utf16Len(r)
	}
	return protocol.Position{Line: clampU32(i), Character: clampU32(u16)}
}

// lineText returns the 1-based line's text without its newline (and without a
// trailing \r on CRLF files); ok=false when the line does not exist.
func (d *Document) lineText(line int) (string, bool) {
	starts := d.lineStarts()
	if line < 1 || line > len(starts) {
		return "", false
	}
	start := starts[line-1]
	end := len(d.Text)
	if line < len(starts) {
		end = starts[line] - 1
	}
	return strings.TrimSuffix(d.Text[start:end], "\r"), true
}

// utf16Len is a rune's UTF-16 code-unit count (astral runes are a surrogate pair).
func utf16Len(r rune) int {
	if r >= 0x10000 {
		return 2
	}
	return 1
}

// runeColIn converts a 0-based UTF-16 character offset within a line to a
// 1-based rune column. Offsets past the line's end extend one column per unit,
// so out-of-range positions stay monotonic.
func runeColIn(text string, chr int) int {
	u16, col := 0, 1
	for _, r := range text {
		if u16 >= chr {
			return col
		}
		u16 += utf16Len(r)
		col++
	}
	return col + (chr - u16)
}

// utf16CharIn converts a 1-based rune column within a line to a 0-based UTF-16
// character offset; columns past the line's end extend one unit per column.
func utf16CharIn(text string, col int) int {
	u16, cur := 0, 1
	for _, r := range text {
		if cur >= col {
			return u16
		}
		u16 += utf16Len(r)
		cur++
	}
	return u16 + (col - cur)
}

func (d *Document) lineStarts() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lineOffsets != nil {
		return d.lineOffsets
	}
	offs := []int{0}
	for i := 0; i < len(d.Text); i++ {
		if d.Text[i] == '\n' {
			offs = append(offs, i+1)
		}
	}
	d.lineOffsets = offs
	return offs
}

func clampU32(v int) uint32 {
	if v < 0 {
		return 0
	}
	return uint32(v) //nolint:gosec // line/column values never approach uint32 max
}

// DocumentStore holds the open buffers.
type DocumentStore struct {
	mu   sync.RWMutex
	docs map[uri.URI]*Document
}

// NewDocumentStore returns an empty store.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: make(map[uri.URI]*Document)}
}

// Set replaces the buffer for u (Full sync) and returns the stored document.
func (s *DocumentStore) Set(u uri.URI, text string, version int32) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := &Document{URI: u, Path: u.FsPath(), Text: text, Version: version}
	s.docs[u] = d
	return d
}

// Get returns the buffer for u.
func (s *DocumentStore) Get(u uri.URI) (*Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[u]
	return d, ok
}

// Remove drops the buffer for u (on didClose).
func (s *DocumentStore) Remove(u uri.URI) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.docs, u)
}

// All returns a snapshot of every open buffer.
func (s *DocumentStore) All() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Document, 0, len(s.docs))
	for _, d := range s.docs {
		out = append(out, d)
	}
	return out
}
