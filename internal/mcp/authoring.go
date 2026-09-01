package mcp

import (
	"context"
	"encoding/json"
)

// Authoring is the manifest write surface. It is implemented by the CLI layer
// (which owns the document builders, schema validation, and the atomic write
// path) and injected via Deps.Authoring. When nil, the write tools are not
// registered and the server is read-only. The interface lives here so the CLI
// can depend on internal/mcp without internal/mcp depending on internal/cli.
type Authoring interface {
	CreateManifest(ctx context.Context, in CreateManifestInput) (WriteResult, error)
	WriteManifest(ctx context.Context, in WriteManifestInput) (WriteResult, error)
	EditManifest(ctx context.Context, in EditManifestInput) (WriteResult, error)
	RemoveManifestPaths(ctx context.Context, in RemoveManifestPathsInput) (WriteResult, error)
	ReorderManifestSequence(ctx context.Context, in ReorderManifestSequenceInput) (WriteResult, error)
	ScaffoldSource(ctx context.Context, payload json.RawMessage) (ScaffoldResult, error)
	InitBundle(ctx context.Context, in InitBundleInput) (InitResult, error)
}

// EditManifestInput edits one document in a manifest file in place.
type EditManifestInput struct {
	File     string         `json:"file" jsonschema:"path to the manifest file, relative to the project root"`
	Position int            `json:"position,omitempty" jsonschema:"1-based document index within the file (default 1)"`
	Patch    map[string]any `json:"patch" jsonschema:"dotted-path edits to apply, e.g. {\"spec.title\": \"Q3\", \"spec.columns[0]\": \"region\"}"`
	DryRun   bool           `json:"dryRun,omitempty" jsonschema:"compute and validate the edit but do not write; the rewritten file is returned in the result's content"`
}

// RemoveManifestPathsInput deletes one or more dotted paths from one document in
// a manifest file in place.
type RemoveManifestPathsInput struct {
	File     string   `json:"file" jsonschema:"path to the manifest file, relative to the project root"`
	Position int      `json:"position,omitempty" jsonschema:"1-based document index within the file (default 1)"`
	Paths    []string `json:"paths" jsonschema:"dotted paths to delete; a trailing [index] removes a sequence element, e.g. [\"spec.subtitle\", \"spec.columns[2]\"]"`
	DryRun   bool     `json:"dryRun,omitempty" jsonschema:"compute and validate the removal but do not write; the rewritten file is returned in the result's content"`
}

// ReorderManifestSequenceInput moves an element within a sequence in one document
// of a manifest file in place.
type ReorderManifestSequenceInput struct {
	File     string `json:"file" jsonschema:"path to the manifest file, relative to the project root"`
	Position int    `json:"position,omitempty" jsonschema:"1-based document index within the file (default 1)"`
	Path     string `json:"path" jsonschema:"dotted path to the sequence to reorder, e.g. spec.columns"`
	From     int    `json:"from" jsonschema:"0-based index of the element to move"`
	To       int    `json:"to" jsonschema:"0-based index the element should end up at"`
	DryRun   bool   `json:"dryRun,omitempty" jsonschema:"compute and validate the reorder but do not write; the rewritten file is returned in the result's content"`
}

// CreateManifestInput describes a new manifest to create from a spec object.
type CreateManifestInput struct {
	Kind        string         `json:"kind" jsonschema:"the manifest kind, e.g. Table, DataSet, ReportArtefact"`
	Name        string         `json:"name" jsonschema:"metadata.name for the new manifest (must be unique within its kind)"`
	Spec        map[string]any `json:"spec" jsonschema:"the spec object for the kind; see the bino://schema/{kind} resource for its shape"`
	Description string         `json:"description,omitempty" jsonschema:"optional metadata.description"`
	File        string         `json:"file,omitempty" jsonschema:"output path relative to the project root; auto-placed by project convention when omitted"`
}

// WriteManifestInput persists a full manifest document verbatim.
type WriteManifestInput struct {
	File   string `json:"file" jsonschema:"output path relative to the project root"`
	YAML   string `json:"yaml" jsonschema:"the full manifest document YAML (apiVersion, kind, metadata, spec)"`
	Append bool   `json:"append,omitempty" jsonschema:"append to an existing multi-document file instead of creating a new one"`
}

// WriteResult reports where a manifest was written and how.
type WriteResult struct {
	File    string `json:"file"`
	Action  string `json:"action"`            // "created", "appended", "edited", or "computed" (dry run)
	Content string `json:"content,omitempty"` // the rewritten file text, set only for a dry-run edit (no write)
}

// ScaffoldResult lists the files written by scaffold_source.
type ScaffoldResult struct {
	OK    bool           `json:"ok"`
	Files []ScaffoldFile `json:"files"`
	Error string         `json:"error,omitempty"`
}

// ScaffoldFile is a single file written by scaffold_source.
type ScaffoldFile struct {
	Path     string `json:"path"`
	Appended bool   `json:"appended"`
}

// InitBundleInput bootstraps a new report bundle.
type InitBundleInput struct {
	Directory string            `json:"directory,omitempty" jsonschema:"target directory for the new bundle, relative to the project root; defaults to the project root itself (scaffold in place)"`
	Source    string            `json:"source,omitempty" jsonschema:"template source; empty selects the built-in minimal scaffold (zero network). 'standard' is a full reference bundle (CSV data source, dataset, IBCS table, chart, style, i18n, assets) in the canonical folder layout — prefer it when a worked example is wanted. 'predef' is a predef project: a reusable registry package with a [package] table plus mock data to preview it against. May also be owner/repo[/subdir]#ref, a URL, or a local path"`
	Set       map[string]string `json:"set,omitempty" jsonschema:"template field values by name (for remote/local templates)"`
	Name      string            `json:"name,omitempty" jsonschema:"metadata.name for the sample ReportArtefact (built-in templates)"`
	Title     string            `json:"title,omitempty" jsonschema:"display title for the sample report (built-in templates)"`
	Language  string            `json:"language,omitempty" jsonschema:"default locale: en or de (built-in templates)"`
	Force     bool              `json:"force,omitempty" jsonschema:"overwrite existing files"`
	Offline   bool              `json:"offline,omitempty" jsonschema:"never reach the network; require a cached template"`
	Trust     bool              `json:"trust,omitempty" jsonschema:"auto-confirm fetching from an uncurated owner/repo"`
}

// InitResult reports the bundle directory, files created, and provenance.
type InitResult struct {
	Directory      string   `json:"directory"`
	Files          []string `json:"files"`
	Template       string   `json:"template,omitempty"`
	ResolvedSource string   `json:"resolvedSource,omitempty"`
	ResolvedSHA    string   `json:"resolvedSHA,omitempty"`
	Folders        []string `json:"folders,omitempty"`
}
