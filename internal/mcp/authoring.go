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
	ScaffoldSource(ctx context.Context, payload json.RawMessage) (ScaffoldResult, error)
	InitBundle(ctx context.Context, in InitBundleInput) (InitResult, error)
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
	File   string `json:"file"`
	Action string `json:"action"` // "created" or "appended"
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
	Directory string `json:"directory,omitempty" jsonschema:"target directory for the new bundle (default ./rainbow-report)"`
	Name      string `json:"name,omitempty" jsonschema:"metadata.name for the sample ReportArtefact"`
	Title     string `json:"title,omitempty" jsonschema:"display title for the sample report"`
	Language  string `json:"language,omitempty" jsonschema:"default locale: en or de"`
	Force     bool   `json:"force,omitempty" jsonschema:"overwrite existing files"`
}

// InitResult reports the bundle directory and the files created.
type InitResult struct {
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
}
