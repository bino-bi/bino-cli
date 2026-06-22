package cli

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/lsp"
	"bino.bi/bino/internal/plugin"
)

// columnIntrospectDeadline caps in-process column introspection so a slow/locking
// query never hangs an LSP request (IntrospectColumns has no built-in timeout).
const columnIntrospectDeadline = 800 * time.Millisecond

// lspInProcessBackend satisfies lsp.Backend over a local daemon.ManagedState — the
// standalone path (no daemon running). It is the LSP analog of runMCPStandalone.
type lspInProcessBackend struct {
	managed  *daemon.ManagedState
	reg      *plugin.PluginRegistry
	cleanup  func()
	onChange atomic.Pointer[func()]
	sessMu   sync.Mutex // serializes shared-session-touching calls
}

func newLSPInProcessBackend(managed *daemon.ManagedState, reg *plugin.PluginRegistry, cleanup func()) *lspInProcessBackend {
	return &lspInProcessBackend{managed: managed, reg: reg, cleanup: cleanup}
}

func (b *lspInProcessBackend) Start(ctx context.Context) error {
	if err := b.managed.State.Refresh(ctx); err != nil {
		// Surface but don't fail — an invalid project should still get diagnostics.
		_ = err
	}
	return b.managed.Watch(ctx, func(_ *daemon.State, _ string) {
		if fn := b.onChange.Load(); fn != nil {
			(*fn)()
		}
	})
}

func (b *lspInProcessBackend) Close() error {
	if b.cleanup != nil {
		b.cleanup()
	}
	return nil
}

func (b *lspInProcessBackend) OnProjectChange(fn func()) { b.onChange.Store(&fn) }

func (b *lspInProcessBackend) Index(_ context.Context) ([]lsp.IndexDoc, error) {
	r := b.managed.State.Index()
	out := make([]lsp.IndexDoc, len(r.Documents))
	for i, d := range r.Documents {
		out[i] = lsp.IndexDoc{Kind: d.Kind, Name: d.Name, File: d.File, Position: d.Position}
	}
	return out, nil
}

func (b *lspInProcessBackend) MergedSchema(ctx context.Context) (json.RawMessage, error) {
	agg := plugin.NewSchemaAggregator(b.reg)
	if err := agg.Build(ctx); err != nil {
		return nil, err
	}
	return agg.MergedSchema(), nil
}

func (b *lspInProcessBackend) Columns(ctx context.Context, name string) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, columnIntrospectDeadline)
	defer cancel()
	b.sessMu.Lock()
	defer b.sessMu.Unlock()
	return b.managed.State.IntrospectColumns(cctx, name)
}

func (b *lspInProcessBackend) GraphDeps(ctx context.Context, kind, name, direction string, maxDepth int) (lsp.GraphResult, error) {
	r := b.managed.State.GraphDeps(ctx, kind, name, direction, maxDepth)
	out := lsp.GraphResult{Error: r.Error}
	for _, n := range r.Nodes {
		out.Nodes = append(out.Nodes, lsp.GraphNode{ID: n.ID, Kind: n.Kind, Name: n.Name, File: n.File})
	}
	for _, e := range r.Edges {
		out.Edges = append(out.Edges, lsp.GraphEdge{FromID: e.FromID, ToID: e.ToID, Direction: e.Direction})
	}
	return out, nil
}

func (b *lspInProcessBackend) ValidateDraft(ctx context.Context, yamlBytes []byte) ([]lsp.Diag, error) {
	diags, err := b.managed.State.ValidateDraft(ctx, yamlBytes)
	if err != nil {
		return nil, err
	}
	return mapDaemonDiags(diags), nil
}

func (b *lspInProcessBackend) ValidateProject(ctx context.Context, execQueries bool) (bool, []lsp.Diag, error) {
	if execQueries {
		b.sessMu.Lock()
		defer b.sessMu.Unlock()
	}
	r := b.managed.State.Validate(ctx, execQueries)
	return r.Valid, mapDaemonDiags(r.Diagnostics), nil
}

// mapDaemonDiags converts daemon diagnostics to the LSP package's local type.
func mapDaemonDiags(in []daemon.Diagnostic) []lsp.Diag {
	out := make([]lsp.Diag, len(in))
	for i, d := range in {
		out[i] = lsp.Diag{
			File:     d.File,
			Position: d.Position,
			Line:     d.Line,
			Column:   d.Column,
			Severity: d.Severity,
			Message:  d.Message,
			Code:     d.Code,
			Field:    d.Field,
		}
	}
	return out
}
