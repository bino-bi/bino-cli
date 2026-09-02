package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"bino.bi/bino/internal/mcp"
	"bino.bi/bino/internal/pathutil"
	"bino.bi/bino/internal/report/spec"
	"bino.bi/bino/internal/schema"
	tmpl "bino.bi/bino/internal/template"
	"bino.bi/bino/internal/version"
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

	manifests, _ := ScanManifests(ctx, a.root) //nolint:errcheck // best-effort listing; an unreadable tree yields no matches
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
	if len(in.Patch) == 0 {
		return mcp.WriteResult{}, fmt.Errorf("patch is required")
	}
	return a.rewriteManifest(in.File, in.Position, in.DryRun, func(content string, pos int) (string, string, error) {
		return spec.EditYAMLDocument(content, pos, in.Patch)
	})
}

// RemoveManifestPaths deletes dotted paths from one document in a manifest file,
// preserving comments and key order, validates the result, then writes the whole
// file atomically.
func (a *cliAuthoring) RemoveManifestPaths(_ context.Context, in mcp.RemoveManifestPathsInput) (mcp.WriteResult, error) {
	if len(in.Paths) == 0 {
		return mcp.WriteResult{}, fmt.Errorf("paths is required")
	}
	return a.rewriteManifest(in.File, in.Position, in.DryRun, func(content string, pos int) (string, string, error) {
		return spec.RemoveYAMLPaths(content, pos, in.Paths)
	})
}

// ReorderManifestSequence moves an element within a sequence in one document of a
// manifest file, preserving comments and key order, validates the result, then
// writes the whole file atomically.
func (a *cliAuthoring) ReorderManifestSequence(_ context.Context, in mcp.ReorderManifestSequenceInput) (mcp.WriteResult, error) {
	if in.Path == "" {
		return mcp.WriteResult{}, fmt.Errorf("path is required")
	}
	return a.rewriteManifest(in.File, in.Position, in.DryRun, func(content string, pos int) (string, string, error) {
		return spec.ReorderYAMLSequence(content, pos, in.Path, in.From, in.To)
	})
}

// rewriteManifest reads a manifest, applies a fidelity-preserving op to the
// 1-based document position, validates the edited document, and either returns
// the rewritten file (DryRun) or writes it atomically. It is the shared core for
// the edit/remove/reorder authoring tools.
func (a *cliAuthoring) rewriteManifest(file string, position int, dryRun bool, op func(content string, pos int) (full string, edited string, err error)) (mcp.WriteResult, error) {
	if file == "" {
		return mcp.WriteResult{}, fmt.Errorf("file is required")
	}
	pos := position
	if pos == 0 {
		pos = 1
	}

	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(a.root, abs)
	}
	content, err := os.ReadFile(abs) //nolint:gosec // G304: path under the project root, supplied by the local agent
	if err != nil {
		return mcp.WriteResult{}, fmt.Errorf("read %s: %w", file, err)
	}

	full, edited, err := op(string(content), pos)
	if err != nil {
		return mcp.WriteResult{}, err
	}
	if err := schema.Validate([]byte(edited)); err != nil {
		return mcp.WriteResult{}, fmt.Errorf("edit would make the document invalid: %w", err)
	}
	if dryRun {
		// Compute-only: return the rewritten file without touching disk.
		return mcp.WriteResult{File: file, Action: "computed", Content: full}, nil
	}
	if err := atomicWriteFile(abs, []byte(full)); err != nil {
		return mcp.WriteResult{}, fmt.Errorf("write %s: %w", file, err)
	}
	return mcp.WriteResult{File: file, Action: "edited"}, nil
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
//
// Unlike the `bino init` CLI (which a human runs in their shell and which
// defaults to a ./rainbow-report subfolder), the agent-facing tool scaffolds in
// place at the project root by default, and resolves a relative directory
// against the project root rather than the process working directory — so it
// stays consistent with the other authoring tools regardless of the server's cwd.
//
// A bare call (no source) renders the built-in minimal scaffold with zero
// network. A remote source is fetched (cached by SHA) and honors ctx for
// cancellation; an uncurated owner requires trust=true.
func (a *cliAuthoring) InitBundle(ctx context.Context, in mcp.InitBundleInput) (mcp.InitResult, error) {
	src, err := tmpl.ParseSource(in.Source)
	if err != nil {
		return mcp.InitResult{}, err
	}
	dir := in.Directory
	switch {
	case dir == "":
		dir = a.root
	case !filepath.IsAbs(dir):
		dir = filepath.Join(a.root, dir)
	}

	if src.Kind == tmpl.SourceBuiltin {
		ans := initAnswers{Directory: dir, ReportName: in.Name, ReportTitle: in.Title, Language: in.Language}
		applyInitDefaults(&ans)
		if src.Name == tmpl.BuiltinPredef {
			if in.Name == "" {
				ans.ReportName = sanitizeManifestName(filepath.Base(dir), ans.ReportName)
			}
			if err := applyPackageDefaults(&ans, in.Set, in.Title != ""); err != nil {
				return mcp.InitResult{}, err
			}
		}
		data, err := buildInitTemplateData(ans)
		if err != nil {
			return mcp.InitResult{}, err
		}
		created, absDir, err := renderBuiltinBundle(src.Name, data, in.Force)
		if err != nil {
			return mcp.InitResult{}, err
		}
		return mcp.InitResult{
			Directory: absDir, Files: created,
			Template: "builtin:" + src.Name, Folders: foldersOf(created),
		}, nil
	}

	if src.Kind == tmpl.SourceShorthand && !in.Trust {
		return mcp.InitResult{}, fmt.Errorf("refusing to fetch the uncurated template github.com/%s/%s without trust=true", src.Owner, src.Repo)
	}
	mgr, err := tmpl.NewManager()
	if err != nil {
		return mcp.InitResult{}, err
	}
	resolved, err := mgr.Resolve(ctx, src, in.Offline)
	if err != nil {
		return mcp.InitResult{}, err
	}
	defer resolved.Close()
	if err := resolved.Manifest.Validate(version.Version); err != nil {
		return mcp.InitResult{}, err
	}
	vars, err := collectFieldsHeadless(resolved.Manifest, in.Set)
	if err != nil {
		return mcp.InitResult{}, err
	}
	created, err := tmpl.RenderTree(resolved.Root, resolved.Manifest, dir, vars, in.Force)
	if err != nil {
		return mcp.InitResult{}, err
	}
	if err := pathutil.StampTemplateProvenance(dir, resolved.Provenance); err != nil {
		return mcp.InitResult{}, err
	}
	return mcp.InitResult{
		Directory: dir, Files: created,
		Template: resolved.Provenance, ResolvedSource: resolved.Provenance, ResolvedSHA: resolved.SHA,
		Folders: foldersOf(created),
	}, nil
}

func actionFor(appendMode bool) string {
	if appendMode {
		return "appended"
	}
	return "created"
}
