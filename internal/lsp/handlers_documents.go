package lsp

import (
	"context"
	"time"

	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"

	"bino.bi/bino/internal/report/config"
	reportspec "bino.bi/bino/internal/report/spec"
)

// Custom methods for editor clients. Other clients never call them, and the
// notification is only sent to a client that asked for it in its
// initializationOptions, so they stay invisible to every other editor.
const (
	methodDocuments    = "bino/documents"
	methodIndex        = "bino/index"
	notifyIndexChanged = "bino/indexChanged"
)

// indexRequestBudget bounds a bino/index fetch. It is not on a typing path, so
// it may wait for a cold backend longer than completion does.
const indexRequestBudget = 10 * time.Second

// DocumentsResult answers bino/documents for one open buffer.
type DocumentsResult struct {
	// IsBino is the same check that gates every other feature of this server.
	IsBino    bool           `json:"isBino"`
	Version   int32          `json:"version"`
	Documents []DocumentInfo `json:"documents"`
}

// DocumentInfo is one bino manifest of the buffer.
type DocumentInfo struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Position int            `json:"position"`
	Range    protocol.Range `json:"range"`
	Blocks   []QueryBlock   `json:"blocks"`
}

// QueryBlock is the inline text of a DataSet's spec.query or spec.prql.
type QueryBlock struct {
	Field    string         `json:"field"`
	Language string         `json:"language"`
	Range    protocol.Range `json:"range"`
}

// IndexResult answers bino/index with the project index (the same shape as
// the daemon's /index).
type IndexResult struct {
	Documents []IndexEntry `json:"documents"`
}

// IndexEntry is one manifest of the project index.
type IndexEntry struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Position int    `json:"position"`
}

// Request serves the non-standard methods.
func (s *Server) Request(ctx context.Context, method string, params any) (any, error) {
	switch method {
	case methodDocuments:
		var p struct {
			TextDocument *protocol.TextDocumentIdentifier `json:"textDocument"`
		}
		if raw, ok := params.(protocol.LSPAny); ok {
			if err := protocol.Unmarshal(raw, &p); err != nil {
				return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, err.Error())
			}
		}
		if p.TextDocument == nil {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "textDocument is required")
		}
		return s.documents(*p.TextDocument), nil
	case methodIndex:
		return s.projectIndex(ctx), nil
	default:
		return nil, jsonrpc2.ErrMethodNotFound
	}
}

// documents outlines an open buffer from its current (unsaved) text.
func (s *Server) documents(td protocol.TextDocumentIdentifier) DocumentsResult {
	res := DocumentsResult{Documents: []DocumentInfo{}}
	doc, ok := s.docs.Get(td.URI)
	if !ok {
		return res
	}
	res.Version = doc.Version
	res.IsBino = isBinoBuffer(doc.Text)
	if !res.IsBino {
		return res
	}
	for _, d := range reportspec.Outline(doc.Text) {
		if !config.IsBinoHeader(d.APIVersion, d.Kind, nil) {
			continue
		}
		info := DocumentInfo{
			Kind:     d.Kind,
			Name:     d.Name,
			Position: d.Position,
			Range:    doc.RangeToProtocol(d.Range),
			Blocks:   []QueryBlock{},
		}
		for _, b := range d.Blocks {
			lang := "sql"
			if b.Field == "prql" {
				lang = "prql"
			}
			info.Blocks = append(info.Blocks, QueryBlock{Field: b.Field, Language: lang, Range: doc.RangeToProtocol(b.Content)})
		}
		res.Documents = append(res.Documents, info)
	}
	return res
}

// projectIndex returns the server's project index, the one the editor's
// trees and pickers read.
func (s *Server) projectIndex(ctx context.Context) IndexResult {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()
	if idx == nil {
		fctx, cancel := context.WithTimeout(ctx, indexRequestBudget)
		defer cancel()
		var err error
		idx, err = s.backend.Index(fctx)
		if err != nil {
			s.log.Warnf("index unavailable: %v", err)
		} else {
			s.mu.Lock()
			s.index = idx
			s.mu.Unlock()
		}
	}
	out := IndexResult{Documents: make([]IndexEntry, 0, len(idx))}
	for _, d := range idx {
		out.Documents = append(out.Documents, IndexEntry(d))
	}
	return out
}

// notifyIndexChange tells an opted-in client that the project index changed.
func (s *Server) notifyIndexChange() {
	s.mu.RLock()
	conn := s.conn
	s.mu.RUnlock()
	if conn == nil || !s.indexNotifications.Load() {
		return
	}
	_ = conn.Notify(s.ctx, notifyIndexChanged, struct{}{}) //nolint:errcheck // best-effort push; the editor may have disconnected
}

// wantsIndexNotifications reads {"indexChanged": true} from initializationOptions.
func wantsIndexNotifications(opts protocol.LSPAny) bool {
	if len(opts) == 0 {
		return false
	}
	var o struct {
		IndexChanged bool `json:"indexChanged"`
	}
	return protocol.Unmarshal(opts, &o) == nil && o.IndexChanged
}
