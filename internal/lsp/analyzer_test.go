package lsp

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

// errDraftBackend simulates a backend whose draft validation fails (e.g. a
// stale daemon answering 404).
type errDraftBackend struct{ fakeBackend }

func (e *errDraftBackend) ValidateDraft(context.Context, []byte) ([]Diag, error) {
	return nil, errors.New("daemon replied 404")
}

// supersededBackend blocks the first ValidateDraft until its context is
// cancelled and answers normally afterwards, so a test can pin one run in
// flight while a newer keystroke supersedes it.
type supersededBackend struct {
	fakeBackend
	mu    sync.Mutex
	calls int
	first chan struct{}
}

func (b *supersededBackend) ValidateDraft(ctx context.Context, _ []byte) ([]Diag, error) {
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	if n == 1 {
		close(b.first)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []Diag{{Severity: "error", Message: "second run", Line: 1, Column: 1}}, nil
}

type published struct {
	version int32
	diags   []protocol.Diagnostic
}

// TestAnalyzer_BackendErrorClearsDiagnostics: a failed ValidateDraft must
// publish an empty set for the analyzed version — a silent return leaves the
// previous squiggles anchored to text that no longer exists.
func TestAnalyzer_BackendErrorClearsDiagnostics(t *testing.T) {
	docs := NewDocumentStore()
	u := uri.File("/proj/report.yaml")
	docs.Set(u, "kind: Table\n", 1)

	out := make(chan published, 4)
	publish := func(_ uri.URI, ver int32, diags []protocol.Diagnostic) {
		out <- published{ver, diags}
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	a := NewAnalyzer(context.Background(), &errDraftBackend{}, docs, publish, log, time.Millisecond)
	defer a.Shutdown()

	a.Schedule(u)
	select {
	case p := <-out:
		if len(p.diags) != 0 {
			t.Fatalf("backend error must clear draft diagnostics, got %d", len(p.diags))
		}
		if p.version != 1 {
			t.Fatalf("clearing publish must carry the analyzed version 1, got %d", p.version)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no publish after a backend error — stale diagnostics would linger")
	}
}

// TestBackfillDiagnostics_HintSuffix: a diagnostic's hint renders as a second
// message line, and the quick-fix Data (parsed from the ORIGINAL message) is
// still extracted for missing-property diagnostics.
func TestBackfillDiagnostics_HintSuffix(t *testing.T) {
	doc := &Document{Text: "kind: Table\n"}
	diags := backfillDiagnostics(doc, []Diag{
		{
			Line: 1, Column: 1, Severity: "error",
			Message: "missing property 'spec'",
			Hint:    "This field is required by the schema",
			Field:   "(root)",
			Code:    "schema-validation",
		},
	})
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	raw, ok := diags[0].Message.(protocol.String)
	if !ok {
		t.Fatalf("unexpected message type %T", diags[0].Message)
	}
	msg := string(raw)
	if !strings.Contains(msg, "missing property 'spec'") || !strings.Contains(msg, "hint: This field is required") {
		t.Fatalf("message must carry the original text plus the hint line, got %q", msg)
	}
	if diags[0].Data == nil {
		t.Fatal("missing-property Data must survive the hint suffix (quick-fix pipeline)")
	}
}

// TestAnalyzer_SupersededRunDoesNotPublish: when a newer keystroke cancels an
// in-flight run, only the newer run may publish — and it must, exactly once.
func TestAnalyzer_SupersededRunDoesNotPublish(t *testing.T) {
	docs := NewDocumentStore()
	u := uri.File("/proj/report.yaml")
	docs.Set(u, "kind: Table\n", 1)

	be := &supersededBackend{first: make(chan struct{})}
	out := make(chan published, 4)
	publish := func(_ uri.URI, ver int32, diags []protocol.Diagnostic) {
		out <- published{ver, diags}
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	a := NewAnalyzer(context.Background(), be, docs, publish, log, time.Millisecond)
	defer a.Shutdown()

	a.Schedule(u)
	<-be.first // first run is in flight, blocked on its context
	docs.Set(u, "kind: DataSet\n", 2)
	a.Schedule(u) // cancels the first run

	select {
	case p := <-out:
		if p.version != 2 {
			t.Fatalf("the superseding run must publish version 2, got %d", p.version)
		}
		if len(p.diags) != 1 {
			t.Fatalf("expected the second run's single diagnostic, got %d", len(p.diags))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the superseding run never published")
	}
	select {
	case p := <-out:
		t.Fatalf("superseded run must not publish, got extra publish for version %d", p.version)
	case <-time.After(200 * time.Millisecond):
	}
}
