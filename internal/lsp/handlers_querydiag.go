package lsp

import (
	"context"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	reportspec "bino.bi/bino/internal/report/spec"
)

const (
	// queryCheckDelay is when a saved file's queries are checked if no
	// project refresh follows the save (an unchanged file is not reloaded).
	queryCheckDelay = 1500 * time.Millisecond
	// queryCheckSettle is the short wait after a project refresh, which is
	// what makes the backend see the saved queries.
	queryCheckSettle = 100 * time.Millisecond
	// queryCheckBudget bounds the DuckDB work of one check.
	queryCheckBudget = 3 * time.Second
)

// queryChecks holds one pending on-save check per file. seq numbers every
// schedule so a run whose file was saved again (or closed) is dropped; gen
// counts project refreshes so a check that ran before the refresh carrying
// its save runs again after it.
type queryChecks struct {
	mu      sync.Mutex
	gen     uint64
	next    uint64
	seq     map[string]uint64
	pending map[string]*pendingCheck
	stopped bool
}

type pendingCheck struct {
	timer *time.Timer
	gen   uint64 // refreshes seen at save time
	seq   uint64
}

func (q *queryChecks) schedule(path string, run func(seq uint64)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return
	}
	if old := q.pending[path]; old != nil {
		old.timer.Stop()
	}
	if q.pending == nil {
		q.pending = make(map[string]*pendingCheck)
		q.seq = make(map[string]uint64)
	}
	q.next++
	p := &pendingCheck{gen: q.gen, seq: q.next}
	q.seq[path] = p.seq
	p.timer = time.AfterFunc(queryCheckDelay, func() { q.fire(path, p, run) })
	q.pending[path] = p
}

// fire runs a due check. Without a refresh since the save the backend may
// still hold the old text, so the entry stays for projectChanged to re-run.
func (q *queryChecks) fire(path string, p *pendingCheck, run func(seq uint64)) {
	q.mu.Lock()
	if q.stopped || q.pending[path] != p {
		q.mu.Unlock()
		return
	}
	if q.gen != p.gen {
		delete(q.pending, path)
	}
	q.mu.Unlock()
	run(p.seq)
}

// projectChanged runs the pending checks soon: the refresh that carries the
// saved text has landed.
func (q *queryChecks) projectChanged() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.gen++
	for _, p := range q.pending {
		p.timer.Reset(queryCheckSettle)
	}
}

// current reports whether seq is still the latest check of path.
func (q *queryChecks) current(path string, seq uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return !q.stopped && q.seq[path] == seq
}

// forget drops the pending check of path and supersedes a running one.
func (q *queryChecks) forget(path string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if p := q.pending[path]; p != nil {
		p.timer.Stop()
	}
	delete(q.pending, path)
	delete(q.seq, path)
}

// stop cancels every pending check for good (server shutdown).
func (q *queryChecks) stop() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stopped = true
	for _, p := range q.pending {
		p.timer.Stop()
	}
	q.pending = nil
}

// wantsQueryDiagnostics reads {"queryDiagnostics": true} from initializationOptions.
func wantsQueryDiagnostics(opts protocol.LSPAny) bool {
	if len(opts) == 0 {
		return false
	}
	var o struct {
		QueryDiagnostics bool `json:"queryDiagnostics"`
	}
	return protocol.Unmarshal(opts, &o) == nil && o.QueryDiagnostics
}

// applyQueryDiagnosticsSetting reads bino.queryDiagnostics from a
// workspace/didChangeConfiguration payload. Turning it off clears the
// published query errors.
func (s *Server) applyQueryDiagnosticsSetting(settings protocol.LSPAny) {
	if len(settings) == 0 {
		return
	}
	var o struct {
		Bino struct {
			QueryDiagnostics *bool `json:"queryDiagnostics"`
		} `json:"bino"`
	}
	if protocol.Unmarshal(settings, &o) != nil || o.Bino.QueryDiagnostics == nil {
		return
	}
	on := *o.Bino.QueryDiagnostics
	s.queryDiagnostics.Store(on)
	if on {
		return
	}
	s.mu.Lock()
	stale := s.queryDiags
	s.queryDiags = nil
	s.mu.Unlock()
	for path := range stale {
		s.publishFile(path, 0)
	}
}

// scheduleQueryCheck queues the DuckDB check of a saved file, when enabled.
func (s *Server) scheduleQueryCheck(u uri.URI) {
	if !s.queryDiagnostics.Load() {
		return
	}
	path := u.FsPath()
	s.checks.schedule(path, func(seq uint64) { s.runQueryCheck(path, seq) })
}

// forgetQueryCheck drops a closed file's pending check and its query errors.
func (s *Server) forgetQueryCheck(u uri.URI) {
	path := u.FsPath()
	s.checks.forget(path)
	s.mu.Lock()
	_, had := s.queryDiags[path]
	delete(s.queryDiags, path)
	s.mu.Unlock()
	if had {
		s.publishFile(path, 0)
	}
}

// runQueryCheck runs each inline SQL DataSet query of a file with LIMIT 0
// through the backend and publishes the DuckDB errors on the query's lines.
// seq is the check's schedule number; a superseded result is dropped.
func (s *Server) runQueryCheck(path string, seq uint64) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Errorf("query check of %s panicked: %v\n%s", path, r, debug.Stack())
		}
	}()
	if !s.queryDiagnostics.Load() {
		return
	}
	var doc *Document
	if d, ok := s.docs.Get(uri.File(path)); ok {
		doc = d
	} else {
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		doc = &Document{Path: path, Text: string(b)}
	}
	if !isBinoBuffer(doc.Text) {
		return
	}
	base := s.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithTimeout(base, queryCheckBudget)
	defer cancel()

	var diags []protocol.Diagnostic
	var unchecked []protocol.Range // blocks the budget did not reach
	for _, d := range reportspec.Outline(doc.Text) {
		block, ok := sqlQueryBlock(d)
		if !ok {
			continue
		}
		if ctx.Err() != nil {
			unchecked = append(unchecked, doc.RangeToProtocol(block.Content))
			continue
		}
		_, err := s.backend.Columns(ctx, d.Name)
		if ctx.Err() != nil {
			unchecked = append(unchecked, doc.RangeToProtocol(block.Content))
			continue
		}
		if err == nil {
			continue
		}
		msg, n, ok := reportspec.ParseDuckDBError(err.Error())
		if !ok {
			continue
		}
		diags = append(diags, protocol.Diagnostic{
			Range:    doc.RangeToProtocol(queryErrorRange(doc, block, n)),
			Severity: protocol.DiagnosticSeverityError,
			Message:  protocol.String(msg),
			Source:   protocol.NewOptional("bino"),
			Code:     codeToken("query-error"),
		})
	}

	s.mu.Lock()
	if !s.queryDiagnostics.Load() || !s.checks.current(path, seq) {
		s.mu.Unlock()
		return
	}
	if s.queryDiags == nil {
		s.queryDiags = make(map[string][]protocol.Diagnostic)
	}
	// Keep the previous findings of the DataSets the budget skipped.
	for _, prev := range s.queryDiags[path] {
		for _, r := range unchecked {
			if prev.Range.Start.Line >= r.Start.Line && prev.Range.Start.Line <= r.End.Line {
				diags = append(diags, prev)
				break
			}
		}
	}
	s.queryDiags[path] = diags
	s.mu.Unlock()
	s.publishFile(path, doc.Version)
}

// sqlQueryBlock returns the inline SQL query of a DataSet. A DataSet with
// prql is skipped: its errors come from the prql extension, not the query text.
func sqlQueryBlock(d reportspec.OutlineDoc) (reportspec.OutlineBlock, bool) {
	if d.Kind != "DataSet" || d.Name == "" {
		return reportspec.OutlineBlock{}, false
	}
	var out reportspec.OutlineBlock
	found := false
	for _, b := range d.Blocks {
		switch b.Field {
		case "prql":
			return reportspec.OutlineBlock{}, false
		case "query":
			out, found = b, true
		}
	}
	return out, found
}

// queryErrorRange maps query line n (1-based, 0 = unknown) to the source line
// of a query block. Only a literal block scalar keeps the query's line breaks;
// folded and single-line queries mark their first line.
func queryErrorRange(doc *Document, b reportspec.OutlineBlock, n int) reportspec.Range {
	c := b.Content
	line := reportspec.QueryErrorLine(b, n)
	if line == c.StartLine && c.StartLine == c.EndLine {
		return c
	}
	text, _ := doc.lineText(line)
	startCol := len([]rune(text)) - len([]rune(strings.TrimLeft(text, " \t"))) + 1
	if line == c.StartLine {
		startCol = c.StartCol
	}
	return reportspec.Range{StartLine: line, StartCol: startCol, EndLine: line, EndCol: len([]rune(text)) + 1}
}
