package lsp

import (
	"sort"
	"sync"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	reportspec "bino.bi/bino/internal/report/spec"
)

// Document is one open editor buffer. Full document sync is used, so Text is the
// authoritative content after every didChange.
//
// Coordinate note: LSP positions are 0-based and UTF-16; yaml.v3 (and the
// resolver) are 1-based. v1 treats a character offset as a rune/byte column —
// correct for ASCII-dominant manifests. All conversion is isolated here so a
// future UTF-16 fix is a one-file change.
type Document struct {
	URI     uri.URI
	Path    string
	Text    string
	Version int32

	mu          sync.Mutex
	lineOffsets []int // byte offset of each line start
}

// PositionToLineCol converts a 0-based LSP position to 1-based (line, col).
func (d *Document) PositionToLineCol(p protocol.Position) (line, col int) {
	return int(p.Line) + 1, int(p.Character) + 1
}

// RangeToProtocol converts a 1-based spec.Range to a 0-based protocol.Range.
func RangeToProtocol(r reportspec.Range) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: clampU32(r.StartLine - 1), Character: clampU32(r.StartCol - 1)},
		End:   protocol.Position{Line: clampU32(r.EndLine - 1), Character: clampU32(r.EndCol - 1)},
	}
}

// OffsetToPosition maps a byte offset in Text to a 0-based LSP position. Used by
// diagnostic backfill to anchor a ${VAR} occurrence found by byte scan.
func (d *Document) OffsetToPosition(offset int) protocol.Position {
	starts := d.lineStarts()
	// Find the last line start <= offset.
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > offset }) - 1
	if i < 0 {
		i = 0
	}
	return protocol.Position{Line: clampU32(i), Character: clampU32(offset - starts[i])}
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
