package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/schema"
)

// cliAuthoring implements mcp.Authoring using the CLI's manifest builders,
// schema validation, and atomic write path. It is bound to a single project
// root. It lives in the cli package (which owns those builders) and is injected
// into the MCP server, so internal/mcp need not import internal/cli.
type cliAuthoring struct {
	root string
}

func newCLIAuthoring(root string) *cliAuthoring { return &cliAuthoring{root: root} }

// CreateManifest builds the {apiVersion, kind, metadata, spec} envelope from a
// spec object, validates it against the schema, and writes it atomically. The
// file is auto-placed by project convention unless an explicit path is given.
func (a *cliAuthoring) CreateManifest(ctx context.Context, in mcp.CreateManifestInput) (mcp.WriteResult, error) {
	if in.Kind == "" {
		return mcp.WriteResult{}, fmt.Errorf("kind is required")
	}
	if in.Name == "" {
		return mcp.WriteResult{}, fmt.Errorf("name is required")
	}

	manifests, _ := ScanManifests(ctx, a.root)
	if !IsNameUnique(manifests, in.Kind, in.Name) {
		return mcp.WriteResult{}, fmt.Errorf("a %s named %q already exists", in.Kind, in.Name)
	}

	doc := &schema.Document{
		APIVersion: schema.APIVersion,
		Kind:       in.Kind,
		Metadata:   schema.Metadata{Name: in.Name, Description: in.Description},
		Spec:       in.Spec,
	}

	outputPath := in.File
	appendMode := false
	if outputPath == "" {
		pattern := DetectFilePattern(manifests, in.Kind)
		outputPath = SuggestOutputPath(pattern, in.Name, in.Kind)
		appendMode = pattern.Mode == "multi-document"
	} else {
		abs := outputPath
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(a.root, abs)
		}
		if _, err := os.Stat(abs); err == nil {
			appendMode = true // existing file -> append a document to it
		}
	}

	if err := WriteSchemaDocument(doc, a.root, outputPath, appendMode, io.Discard); err != nil {
		return mcp.WriteResult{}, err
	}
	return mcp.WriteResult{File: outputPath, Action: actionFor(appendMode)}, nil
}

// WriteManifest persists a full manifest document verbatim after validating it.
func (a *cliAuthoring) WriteManifest(_ context.Context, in mcp.WriteManifestInput) (mcp.WriteResult, error) {
	if in.File == "" {
		return mcp.WriteResult{}, fmt.Errorf("file is required")
	}
	if err := schema.Validate([]byte(in.YAML)); err != nil {
		return mcp.WriteResult{}, fmt.Errorf("invalid manifest: %w", err)
	}
	abs := in.File
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.root, abs)
	}
	if in.Append {
		if err := AppendToManifest(abs, in.YAML); err != nil {
			return mcp.WriteResult{}, err
		}
		return mcp.WriteResult{File: in.File, Action: "appended"}, nil
	}
	if err := WriteManifest(abs, in.YAML); err != nil {
		return mcp.WriteResult{}, err
	}
	return mcp.WriteResult{File: in.File, Action: "created"}, nil
}

// EditManifest applies dotted-path edits to one document in a manifest file,
// preserving comments and key order, validates the edited document, then writes
// the whole file atomically.
func (a *cliAuthoring) EditManifest(_ context.Context, in mcp.EditManifestInput) (mcp.WriteResult, error) {
	if in.File == "" {
		return mcp.WriteResult{}, fmt.Errorf("file is required")
	}
	if len(in.Patch) == 0 {
		return mcp.WriteResult{}, fmt.Errorf("patch is required")
	}
	pos := in.Position
	if pos == 0 {
		pos = 1
	}

	abs := in.File
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.root, abs)
	}
	content, err := os.ReadFile(abs) //nolint:gosec // G304: path under the project root, supplied by the local agent
	if err != nil {
		return mcp.WriteResult{}, fmt.Errorf("read %s: %w", in.File, err)
	}

	full, edited, err := spec.EditYAMLDocument(string(content), pos, in.Patch)
	if err != nil {
		return mcp.WriteResult{}, err
	}
	if err := schema.Validate([]byte(edited)); err != nil {
		return mcp.WriteResult{}, fmt.Errorf("edit would make the document invalid: %w", err)
	}
	if err := atomicWriteFile(abs, []byte(full), 0o644); err != nil {
		return mcp.WriteResult{}, fmt.Errorf("write %s: %w", in.File, err)
	}
	return mcp.WriteResult{File: in.File, Action: "edited"}, nil
}

// ScaffoldSource scaffolds a DataSource (and optional DataSet) from a payload,
// reusing the same core as the lsp-helper scaffold command.
func (a *cliAuthoring) ScaffoldSource(ctx context.Context, payload json.RawMessage) (mcp.ScaffoldResult, error) {
	var p scaffoldPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return mcp.ScaffoldResult{}, fmt.Errorf("parse payload: %w", err)
	}
	res := scaffoldManifests(ctx, a.root, p)
	out := mcp.ScaffoldResult{OK: res.OK, Error: res.Error, Files: make([]mcp.ScaffoldFile, 0, len(res.Files))}
	for _, f := range res.Files {
		out.Files = append(out.Files, mcp.ScaffoldFile{Path: f.Path, Appended: f.Appended})
	}
	return out, nil
}

// InitBundle bootstraps a new report bundle, reusing the `bino init` core.
func (a *cliAuthoring) InitBundle(_ context.Context, in mcp.InitBundleInput) (mcp.InitResult, error) {
	ans := initAnswers{
		Directory:   in.Directory,
		ReportName:  in.Name,
		ReportTitle: in.Title,
		Language:    in.Language,
	}
	applyInitDefaults(&ans)
	data, err := buildInitTemplateData(ans)
	if err != nil {
		return mcp.InitResult{}, err
	}
	created, dir, err := writeInitBundle(data, in.Force)
	if err != nil {
		return mcp.InitResult{}, err
	}
	return mcp.InitResult{Directory: dir, Files: created}, nil
}

func actionFor(appendMode bool) string {
	if appendMode {
		return "appended"
	}
	return "created"
}
