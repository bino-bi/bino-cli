// Package lsp implements bino's Language Server: a stdio LSP server over a YAML
// CST that serves completion, hover, diagnostics, and (phase 2) navigation and
// refactors for bino manifests. The heavy project state (DuckDB session, file
// watcher, schema) is reached through the Backend interface, which is satisfied
// either in-process (own ManagedState) or by proxying a running bino daemon —
// so the handler code is identical in both modes.
package lsp

import (
	"context"
	"encoding/json"
)

// Diag is a backend-agnostic diagnostic. It mirrors daemon.Diagnostic field for
// field but lives here so the LSP package never imports internal/daemon (the
// proxy build stays a pure HTTP client).
type Diag struct {
	File     string
	Position int // 1-based document ordinal
	Line     int // 1-based; 0 when unknown
	Column   int // 1-based; 0 when unknown
	Severity string
	Message  string
	Code     string
	Field    string // dotted path, e.g. "spec.title"
	Hint     string // actionable guidance, rendered as a trailing "hint:" line
}

// IndexDoc is one entry in the project index.
type IndexDoc struct {
	Kind     string
	Name     string
	File     string
	Position int
}

// GraphNode and GraphEdge describe a dependency-graph slice.
type GraphNode struct {
	ID   string
	Kind string
	Name string
	File string
}

type GraphEdge struct {
	FromID    string
	ToID      string
	Direction string
}

type GraphResult struct {
	Nodes []GraphNode
	Edges []GraphEdge
	Error string
}

// Backend is the single seam between the LSP logic and where project state
// lives. The in-process implementation wraps a daemon.ManagedState; the HTTP
// implementation talks to a running daemon over loopback + SSE.
type Backend interface {
	// Start performs first load and begins watching for project changes
	// (standalone: Refresh + file watch; proxy: SSE subscription).
	Start(ctx context.Context) error
	// Close releases any owned resources.
	Close() error

	// Index returns the project document index.
	Index(ctx context.Context) ([]IndexDoc, error)
	// MergedSchema returns the merged (built-in + plugin) JSON schema.
	MergedSchema(ctx context.Context) (json.RawMessage, error)
	// Columns introspects the columns of a named DataSet/DataSource. The caller
	// imposes the deadline (introspection has no built-in timeout).
	Columns(ctx context.Context, name string) ([]string, error)
	// GraphDeps returns a dependency-graph slice rooted at kind/name.
	GraphDeps(ctx context.Context, kind, name, direction string, maxDepth int) (GraphResult, error)

	// ValidateDraft validates an in-memory buffer (schema + constraints, no disk).
	ValidateDraft(ctx context.Context, yamlBytes []byte) ([]Diag, error)
	// ValidateProject validates the whole project on disk.
	ValidateProject(ctx context.Context, execQueries bool) (valid bool, diags []Diag, err error)

	// OnProjectChange registers a callback fired when the project changed on disk
	// (standalone: watcher; proxy: SSE index-updated/diagnostics).
	OnProjectChange(fn func())
}
