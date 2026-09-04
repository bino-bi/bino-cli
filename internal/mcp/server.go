// Package mcp exposes bino's introspection, validation, authoring, and build
// surface as a Model Context Protocol server. It is built once against the
// shared in-process daemon.State and served two ways: mounted on `bino daemon`
// over Streamable HTTP, and via the standalone `bino mcp` stdio entrypoint.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"bino.bi/bino/internal/daemon"
	"bino.bi/bino/internal/plugin"
	"bino.bi/bino/internal/report/datasource"
	embedkinds "bino.bi/bino/internal/report/embed"
	tmpl "bino.bi/bino/internal/template"
	"bino.bi/bino/internal/version"
)

// Deps bundles everything the MCP server needs to serve a single project.
type Deps struct {
	State    *daemon.State
	Registry *plugin.PluginRegistry
	// Authoring, when non-nil, enables the manifest write tools (create_manifest,
	// write_manifest, scaffold_source, init_bundle, edit_manifest). Supplied by
	// the CLI layer.
	Authoring Authoring
}

const serverInstructions = `bino is "Report-as-Code": pixel-perfect PDF reports defined as YAML manifests + SQL.

Resources:
  bino://schema          full merged JSON Schema (built-in + plugin kinds)
  bino://schema/{kind}   spec schema for one kind (self-contained, with $defs)
  bino://kinds           every manifest kind + its capability category
  bino://documents       project index: every document -> {kind, name, file, position}

Schema tools:
  outline_kind(kind)     compact per-field outline of a kind's spec (start here)
  scaffold_kind(kind)    minimal YAML document for a kind, required fields pre-filled
  describe_kind(kind)    full spec JSON Schema (large; only when the outline is ambiguous)

Typical authoring loop: outline_kind(kind) -> scaffold_kind(kind) -> get_columns to
learn a dataset's columns -> draft YAML -> validate_draft -> create_manifest /
write_manifest -> build. bino's schema and validation are your guardrails.`

// NewServer constructs the MCP server. deps.State must be non-nil; deps.Registry
// may be nil (no plugins).
func NewServer(deps Deps) *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "bino",
		Title:   "bino — Report-as-Code",
		Version: version.Version,
	}, &mcpsdk.ServerOptions{
		Instructions: serverInstructions,
	})

	h := &handlers{deps: deps}
	h.registerResources(srv)
	h.registerReadTools(srv)
	h.registerSchemaTools(srv)
	h.registerBuildTool(srv)
	h.registerLayoutTool(srv)
	h.registerAuthoringTools(srv)
	return srv
}

// handlers carries the shared dependencies for every resource/tool handler.
type handlers struct {
	deps Deps
}

// --- Resources ---

func (h *handlers) registerResources(srv *mcpsdk.Server) {
	srv.AddResource(&mcpsdk.Resource{
		Name:        "schema",
		URI:         "bino://schema",
		Title:       "Merged JSON Schema",
		Description: "The full merged JSON Schema for all manifest kinds (built-in + plugin).",
		MIMEType:    "application/json",
	}, h.readSchema)

	srv.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		Name:        "schema-for-kind",
		URITemplate: "bino://schema/{kind}",
		Title:       "Schema for a kind",
		Description: "The self-contained spec schema for a single manifest kind.",
		MIMEType:    "application/json",
	}, h.readSchemaForKind)

	srv.AddResource(&mcpsdk.Resource{
		Name:        "kinds",
		URI:         "bino://kinds",
		Title:       "Manifest kinds",
		Description: "Every manifest kind and its capability category (data/layout/embeddable/artefact/config).",
		MIMEType:    "application/json",
	}, h.readKinds)

	srv.AddResource(&mcpsdk.Resource{
		Name:        "documents",
		URI:         "bino://documents",
		Title:       "Project index",
		Description: "Every document in the project: kind, name, file, and document position.",
		MIMEType:    "application/json",
	}, h.readDocuments)

	srv.AddResource(&mcpsdk.Resource{
		Name:        "templates",
		URI:         "bino://templates",
		Title:       "Available templates",
		Description: "Built-in and curated templates that init_bundle can scaffold from.",
		MIMEType:    "application/json",
	}, h.readTemplates)
}

func (h *handlers) readTemplates(_ context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	payload := map[string]any{
		"builtin": tmpl.BuiltinNames(),
		"curated": tmpl.CuratedNames(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return rawJSONResource("bino://templates", raw), nil
}

func (h *handlers) readSchema(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	agg, err := h.aggregator(ctx)
	if err != nil {
		return nil, err
	}
	return rawJSONResource("bino://schema", agg.MergedSchema()), nil
}

func (h *handlers) readSchemaForKind(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	kind := strings.TrimPrefix(req.Params.URI, "bino://schema/")
	if kind == "" || kind == req.Params.URI {
		return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
	}
	raw, ok, err := h.specSchemaForKind(ctx, kind)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
	}
	return rawJSONResource(req.Params.URI, raw), nil
}

func (h *handlers) readKinds(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	kinds, err := h.listKinds(ctx)
	if err != nil {
		return nil, err
	}
	return jsonResource("bino://kinds", map[string]any{"kinds": kinds})
}

func (h *handlers) readDocuments(_ context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	return jsonResource("bino://documents", h.deps.State.Index())
}

// --- Read tools ---

// emptyInput is the input type for tools that take no arguments.
type emptyInput struct{}

// KindInfo describes a manifest kind, its capability category, and whether it
// renders standalone as a component (the designer's live canvas and the preview
// read this flag from the same authority as the render layer).
type KindInfo struct {
	Name       string `json:"name"`
	Category   string `json:"category"`
	Embeddable bool   `json:"embeddable"`
}

func (h *handlers) registerReadTools(srv *mcpsdk.Server) {
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_kinds",
		Description: "List every manifest kind (built-in + plugin) with its capability category.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, listKindsOutput, error) {
		kinds, err := h.listKinds(ctx)
		if err != nil {
			return nil, listKindsOutput{}, err
		}
		return nil, listKindsOutput{Kinds: kinds}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "describe_kind",
		Description: "Full spec JSON Schema for one manifest kind (self-contained, with $defs). Large — up to ~20k tokens for layout kinds. Prefer outline_kind first and scaffold_kind to start a document; use this only for a shape the outline leaves ambiguous.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in describeKindInput) (*mcpsdk.CallToolResult, describeKindOutput, error) {
		raw, ok, err := h.specSchemaForKind(ctx, in.Kind)
		if err != nil {
			return nil, describeKindOutput{}, err
		}
		out := describeKindOutput{Kind: in.Kind, Found: ok}
		if ok {
			if err := json.Unmarshal(raw, &out.Schema); err != nil {
				return nil, describeKindOutput{}, fmt.Errorf("decode schema: %w", err)
			}
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "describe_project",
		Description: "List every document in the project: kind, name, file, and document position.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ emptyInput) (*mcpsdk.CallToolResult, daemon.IndexResult, error) {
		return nil, h.deps.State.Index(), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "describe_document",
		Description: "List the documents (kind, name, position) declared in a single manifest file.",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in describeDocumentInput) (*mcpsdk.CallToolResult, describeDocumentOutput, error) {
		idx := h.deps.State.Index()
		out := describeDocumentOutput{File: in.File, Documents: []daemon.IndexDocument{}}
		for _, doc := range idx.Documents {
			if doc.File == in.File {
				out.Documents = append(out.Documents, doc)
			}
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "validate_project",
		Description: "Validate the project on disk and return diagnostics. Set execute_queries to also run datasets and report data-validation warnings.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in validateProjectInput) (*mcpsdk.CallToolResult, daemon.ValidateResult, error) {
		return nil, h.deps.State.Validate(ctx, in.ExecuteQueries), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_columns",
		Description: "Return the column names of a DataSet or DataSource. Prefix the name with '$' to force a DataSource lookup.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in getColumnsInput) (*mcpsdk.CallToolResult, daemon.ColumnsResult, error) {
		return nil, h.deps.State.Columns(ctx, in.Name), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_rows",
		Description: "Return a sample of rows from a DataSet or DataSource (default limit 100). Prefix the name with '$' to force a DataSource lookup.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in getRowsInput) (*mcpsdk.CallToolResult, daemon.RowsResult, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 100
		}
		return nil, h.deps.State.Rows(ctx, in.Name, limit), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "graph_deps",
		Description: "Traverse the dependency graph from a node. direction: 'out' (dependencies), 'in' (dependents), or 'both' (default).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in graphDepsInput) (*mcpsdk.CallToolResult, daemon.GraphDepsResult, error) {
		return nil, h.deps.State.GraphDeps(ctx, in.Kind, in.Name, in.Direction, in.MaxDepth), nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "validate_draft",
		Description: "Validate manifest YAML in memory (no disk write) and return schema/constraint diagnostics. Use this to check a draft before create_manifest or write_manifest.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in validateDraftInput) (*mcpsdk.CallToolResult, daemon.ValidateResult, error) {
		diags, err := h.deps.State.ValidateDraft(ctx, []byte(in.YAML))
		if err != nil {
			return nil, daemon.ValidateResult{}, err
		}
		if diags == nil {
			diags = []daemon.Diagnostic{}
		}
		return nil, daemon.ValidateResult{Valid: len(diags) == 0, Diagnostics: diags}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "introspect_source",
		Description: "Probe a not-yet-registered data source (CSV/Excel/database) and return its columns, sample rows, Excel sheet names, and detected CSV options. Pass the bare DataSource spec object (no apiVersion/kind/metadata).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in introspectSourceInput) (*mcpsdk.CallToolResult, introspectSourceOutput, error) {
		specJSON, err := json.Marshal(in.Spec)
		if err != nil {
			return nil, introspectSourceOutput{}, fmt.Errorf("encode spec: %w", err)
		}
		out := introspectSourceOutput{Columns: []datasource.ProbeColumn{}, SampleRows: []map[string]any{}}
		res, err := h.deps.State.IntrospectSource(ctx, specJSON, in.Sheet, in.Limit)
		if err != nil {
			// Surface probe failures (bad path, unknown type, ...) as structured
			// output so the agent can read and fix them, not as a protocol error.
			out.Error = err.Error()
			return nil, out, nil //nolint:nilerr // probe error reported in out.Error
		}
		out.Columns = res.Columns
		out.Sheets = res.Sheets
		out.SampleRows = res.SampleRows
		out.Truncated = res.Truncated
		out.DetectedCSV = res.DetectedCSV
		if out.Columns == nil {
			out.Columns = []datasource.ProbeColumn{}
		}
		if out.SampleRows == nil {
			out.SampleRows = []map[string]any{}
		}
		return nil, out, nil
	})
}

// --- Tool input/output types (schemas inferred from these via jsonschema tags) ---

type listKindsOutput struct {
	Kinds []KindInfo `json:"kinds"`
}

type describeKindInput struct {
	Kind string `json:"kind" jsonschema:"the manifest kind to describe, e.g. Table or DataSet"`
}

type describeKindOutput struct {
	Kind   string         `json:"kind"`
	Found  bool           `json:"found"`
	Schema map[string]any `json:"schema,omitempty"`
}

type describeDocumentInput struct {
	File string `json:"file" jsonschema:"path to the manifest file, relative to the project root or absolute"`
}

type describeDocumentOutput struct {
	File      string                 `json:"file"`
	Documents []daemon.IndexDocument `json:"documents"`
}

type validateProjectInput struct {
	ExecuteQueries bool `json:"execute_queries,omitempty" jsonschema:"also execute datasets and report data-validation warnings (slower)"`
}

type getColumnsInput struct {
	Name string `json:"name" jsonschema:"DataSet or DataSource name; prefix with '$' to force a DataSource"`
}

type getRowsInput struct {
	Name  string `json:"name" jsonschema:"DataSet or DataSource name; prefix with '$' to force a DataSource"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum rows to return (default 100)"`
}

type graphDepsInput struct {
	Kind      string `json:"kind" jsonschema:"manifest kind of the root node, e.g. DataSet or ReportArtefact"`
	Name      string `json:"name" jsonschema:"name of the root node"`
	Direction string `json:"direction,omitempty" jsonschema:"'out' (dependencies), 'in' (dependents), or 'both' (default)"`
	MaxDepth  int    `json:"max_depth,omitempty" jsonschema:"limit traversal depth (0 = unlimited)"`
}

type validateDraftInput struct {
	YAML string `json:"yaml" jsonschema:"the manifest YAML to validate, one or more documents separated by ---"`
}

type introspectSourceInput struct {
	Spec  map[string]any `json:"spec" jsonschema:"the DataSource spec object without the apiVersion/kind/metadata envelope, e.g. {type: csv, path: data/sales.csv}"`
	Sheet string         `json:"sheet,omitempty" jsonschema:"Excel sheet name to read (optional)"`
	Limit int            `json:"limit,omitempty" jsonschema:"maximum sample rows (default 100)"`
}

type introspectSourceOutput struct {
	Columns     []datasource.ProbeColumn `json:"columns"`
	Sheets      []string                 `json:"sheets,omitempty"`
	SampleRows  []map[string]any         `json:"sampleRows"`
	Truncated   bool                     `json:"truncated"`
	DetectedCSV *datasource.DetectedCSV  `json:"detectedCsv,omitempty"`
	Error       string                   `json:"error,omitempty"`
}

// --- Schema helpers ---

// aggregator builds a fresh schema aggregator over the (possibly empty) plugin
// registry. Building is cheap when no plugins are registered.
func (h *handlers) aggregator(ctx context.Context) (*plugin.SchemaAggregator, error) {
	reg := h.deps.Registry
	if reg == nil {
		reg = plugin.NewRegistry()
	}
	agg := plugin.NewSchemaAggregator(reg)
	if err := agg.Build(ctx); err != nil {
		return nil, fmt.Errorf("build schema: %w", err)
	}
	return agg, nil
}

// specSchemaForKind returns a self-contained spec schema for a kind. Plugin
// kinds come straight from the aggregator; built-in kinds are extracted from the
// merged schema's allOf if/then block and wrapped with the merged $defs so the
// returned schema resolves on its own.
func (h *handlers) specSchemaForKind(ctx context.Context, kind string) (json.RawMessage, bool, error) {
	agg, err := h.aggregator(ctx)
	if err != nil {
		return nil, false, err
	}
	if raw, ok := agg.SchemaForKind(kind); ok {
		return raw, true, nil
	}
	raw, ok := extractSpecSchema(agg.MergedSchema(), kind)
	return raw, ok, nil
}

// extractSpecSchema pulls then.properties.spec for the given kind out of the
// merged schema's allOf blocks and makes it self-contained by attaching $defs.
func extractSpecSchema(merged json.RawMessage, kind string) (json.RawMessage, bool) {
	var doc map[string]any
	if json.Unmarshal(merged, &doc) != nil {
		return nil, false
	}
	allOf, _ := doc["allOf"].([]any)
	for _, raw := range allOf {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ifClause, ok := block["if"].(map[string]any)
		if !ok {
			continue
		}
		props, ok := ifClause["properties"].(map[string]any)
		if !ok {
			continue
		}
		kindProp, ok := props["kind"].(map[string]any)
		if !ok {
			continue
		}
		if c, _ := kindProp["const"].(string); c != kind {
			continue
		}
		then, ok := block["then"].(map[string]any)
		if !ok {
			return nil, false
		}
		thenProps, ok := then["properties"].(map[string]any)
		if !ok {
			return nil, false
		}
		spec, ok := thenProps["spec"].(map[string]any)
		if !ok {
			return nil, false
		}
		out := make(map[string]any)
		maps.Copy(out, spec)
		if defs, ok := doc["$defs"]; ok {
			out["$defs"] = defs
		}
		if sc, ok := doc["$schema"]; ok {
			out["$schema"] = sc
		}
		b, err := json.Marshal(out)
		if err != nil {
			return nil, false
		}
		return b, true
	}
	return nil, false
}

// listKinds enumerates kinds from the merged schema's kind enum and categorizes them.
func (h *handlers) listKinds(ctx context.Context) ([]KindInfo, error) {
	agg, err := h.aggregator(ctx)
	if err != nil {
		return nil, err
	}
	names := kindEnum(agg.MergedSchema())
	out := make([]KindInfo, 0, len(names))
	for _, n := range names {
		out = append(out, KindInfo{Name: n, Category: h.categoryFor(n), Embeddable: embedkinds.IsEmbeddable(n)})
	}
	return out, nil
}

// kindEnum extracts properties.kind.enum from the merged schema.
func kindEnum(merged json.RawMessage) []string {
	var doc struct {
		Properties struct {
			Kind struct {
				Enum []string `json:"enum"`
			} `json:"kind"`
		} `json:"properties"`
	}
	if json.Unmarshal(merged, &doc) != nil {
		return nil
	}
	return doc.Properties.Kind.Enum
}

// categoryFor returns the capability category for a kind. Built-in kinds resolve
// from the single authority (internal/report/embed); plugin-provided kinds fall
// back to the registry's categorization.
func (h *handlers) categoryFor(kind string) string {
	if c, ok := embedkinds.BuiltinCategory(kind); ok {
		return c
	}
	if h.deps.Registry != nil {
		return h.deps.Registry.CategorizeKind(kind).CapabilityCategory()
	}
	return "embeddable"
}

// --- Resource result helpers ---

func jsonResource(uri string, v any) (*mcpsdk.ReadResourceResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return rawJSONResource(uri, b), nil
}

func rawJSONResource(uri string, raw json.RawMessage) *mcpsdk.ReadResourceResult {
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(raw),
		}},
	}
}
