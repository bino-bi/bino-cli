package lsp

import (
	"context"
	"os"
	"regexp"
	"strconv"
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

// queryChecks holds one pending on-save check per file.
type queryChecks struct {
	mu      sync.Mutex
	pending map[string]*time.Timer
}

func (q *queryChecks) schedule(path string, run func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t := q.pending[path]; t != nil {
		t.Stop()
	}
	if q.pending == nil {
		q.pending = make(map[string]*time.Timer)
	}
	var t *time.Timer
	t = time.AfterFunc(queryCheckDelay, func() {
		q.mu.Lock()
		mine := q.pending[path] == t
		if mine {
			delete(q.pending, path)
		}
		q.mu.Unlock()
		if mine {
			run()
		}
	})
	q.pending[path] = t
}

// projectChanged runs the pending checks soon: the refresh that carries the
// saved text has landed.
func (q *queryChecks) projectChanged() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, t := range q.pending {
		t.Reset(queryCheckSettle)
	}
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
	s.checks.schedule(path, func() { s.runQueryCheck(path) })
}

// runQueryCheck runs each inline SQL DataSet query of a file with LIMIT 0
// through the backend and publishes the DuckDB errors on the query's lines.
func (s *Server) runQueryCheck(path string) {
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
	for _, d := range reportspec.Outline(doc.Text) {
		block, ok := sqlQueryBlock(d)
		if !ok {
			continue
		}
		_, err := s.backend.Columns(ctx, d.Name)
		if err == nil || ctx.Err() != nil {
			continue
		}
		msg, n, ok := parseDuckDBError(err.Error())
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
	if s.queryDiags == nil {
		s.queryDiags = make(map[string][]protocol.Diagnostic)
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

var (
	duckdbErrorClass = regexp.MustCompile(`[A-Z][A-Za-z]*(?: [A-Z][A-Za-z]*)* Error: `)
	duckdbErrorLine  = regexp.MustCompile(`\bLINE (\d+):`)
	foldedHeader     = regexp.MustCompile(`:\s*>`)
)

// parseDuckDBError extracts a DuckDB error ("Binder Error: ...") from a
// backend error and the query line its "LINE n:" excerpt points at (0 when
// absent). Errors that are not DuckDB's (timeouts, unknown names) yield ok=false.
func parseDuckDBError(text string) (msg string, line int, ok bool) {
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

// queryErrorRange maps query line n (1-based, 0 = unknown) to the source line
// of a query block. Only a literal block scalar keeps the query's line breaks;
// folded and single-line queries mark their first line.
func queryErrorRange(doc *Document, b reportspec.OutlineBlock, n int) reportspec.Range {
	c := b.Content
	line := c.StartLine
	if n > 1 && c.StartLine+n-1 <= c.EndLine && !isFoldedBlock(doc, c.StartLine) {
		line = c.StartLine + n - 1
	}
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

// isFoldedBlock reports whether the block scalar whose content starts on
// line has a folded (>) header.
func isFoldedBlock(doc *Document, line int) bool {
	for l := line - 1; l >= 1; l-- {
		text, _ := doc.lineText(l)
		if strings.TrimSpace(text) != "" {
			return foldedHeader.MatchString(text)
		}
	}
	return false
}
