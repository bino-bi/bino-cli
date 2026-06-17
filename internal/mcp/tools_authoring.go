package mcp

import (
	"context"
	"encoding/json"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// scaffoldSourceInput is the scaffold_source tool input. Its JSON shape matches
// the scaffold payload the CLI consumes ({dataSource, dataSet}).
type scaffoldSourceInput struct {
	DataSource map[string]any `json:"dataSource" jsonschema:"DataSource fields: name (required), type (csv/excel/parquet/json/postgres_query/...), path or connection, plus options"`
	DataSet    map[string]any `json:"dataSet,omitempty" jsonschema:"optional DataSet: name, columns, sql (verbatim, else a typed SELECT is generated)"`
}

func (h *handlers) registerAuthoringTools(srv *mcpsdk.Server) {
	a := h.deps.Authoring
	if a == nil {
		return
	}

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "create_manifest",
		Description: "Create a new manifest of any kind from a spec object: builds the apiVersion/kind/metadata/spec envelope, validates it against the schema, and writes it atomically (auto-placing the file by project convention unless `file` is given). Read bino://schema/{kind} for the spec shape; validate_draft first if unsure.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in CreateManifestInput) (*mcpsdk.CallToolResult, WriteResult, error) {
		res, err := a.CreateManifest(ctx, in)
		if err != nil {
			return errorResult(err), WriteResult{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "write_manifest",
		Description: "Persist a full manifest document (one apiVersion/kind/metadata/spec YAML document), validated against the schema before writing. Set append=true to add it to an existing multi-document file. Writes are atomic.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in WriteManifestInput) (*mcpsdk.CallToolResult, WriteResult, error) {
		res, err := a.WriteManifest(ctx, in)
		if err != nil {
			return errorResult(err), WriteResult{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "scaffold_source",
		Description: "Scaffold a DataSource (and optionally a typed DataSet that selects from it) in one step, choosing file placement by project convention. Validates before writing.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in scaffoldSourceInput) (*mcpsdk.CallToolResult, ScaffoldResult, error) {
		payload, err := json.Marshal(in)
		if err != nil {
			return errorResult(err), ScaffoldResult{}, nil
		}
		res, err := a.ScaffoldSource(ctx, payload)
		if err != nil {
			return errorResult(err), ScaffoldResult{}, nil
		}
		return nil, res, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "init_bundle",
		Description: "Bootstrap a new bino report bundle (bino.toml + sample manifests + ignore files) in a target directory, so the project can be built or previewed immediately.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in InitBundleInput) (*mcpsdk.CallToolResult, InitResult, error) {
		res, err := a.InitBundle(ctx, in)
		if err != nil {
			return errorResult(err), InitResult{}, nil
		}
		return nil, res, nil
	})
}

// errorResult wraps an error as a tool result with IsError set, so the agent
// sees the message and can self-correct rather than receiving a protocol error.
func errorResult(err error) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		IsError: true,
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: err.Error()}},
	}
}
