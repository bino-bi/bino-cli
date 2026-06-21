package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	reportspec "bino.bi/bino/internal/report/spec"
)

// PrepareRename validates the cursor sits on a renameable symbol and returns its
// range + current name as the rename placeholder.
func (s *Server) PrepareRename(_ context.Context, params *protocol.PrepareRenameParams) (protocol.PrepareRenameResult, error) {
	pc, ok := s.resolve(params.TextDocument.URI, params.Position)
	if !ok {
		return nil, nil
	}
	_, name := s.symbolUnderCursor(pc)
	if name == "" {
		return nil, nil
	}
	return &protocol.PrepareRenamePlaceholder{
		Range:       RangeToProtocol(pc.ReplaceRange),
		Placeholder: name,
	}, nil
}

// Rename renames a manifest and every reference to it across the project. Edits
// are precise range replacements (preserving a $ shorthand on DataSource refs).
func (s *Server) Rename(ctx context.Context, params *protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	pc, ok := s.resolve(params.TextDocument.URI, params.Position)
	if !ok {
		return nil, nil
	}
	kind, name := s.symbolUnderCursor(pc)
	if name == "" {
		return nil, nil
	}
	ni := s.getNameIndex(ctx)
	changes := make(map[uri.URI][]protocol.TextEdit)

	if def, found := ni.Definition(kind, name); found {
		u := uri.File(def.File)
		changes[u] = append(changes[u], protocol.TextEdit{Range: RangeToProtocol(def.NameRange), NewText: params.NewName})
	}
	for _, r := range ni.References(kind, name) {
		u := uri.File(r.File)
		newText := params.NewName
		if r.Dollar {
			newText = "$" + newText
		}
		changes[u] = append(changes[u], protocol.TextEdit{Range: RangeToProtocol(r.Range), NewText: newText})
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return &protocol.WorkspaceEdit{Changes: changes}, nil
}

// CodeAction offers quick-fixes: append a missing ${VAR} to .env, insert a
// missing required field, and scaffold a dangling reference's target manifest.
func (s *Server) CodeAction(ctx context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	var actions []protocol.CommandOrCodeAction
	docURI := params.TextDocument.URI
	for i := range params.Context.Diagnostics {
		d := params.Context.Diagnostics[i]
		switch {
		case diagCode(d.Code) == "missing-env-var":
			if name := envVarName(diagMessage(d.Message)); name != "" {
				if a := s.addEnvVarAction(name, d); a != nil {
					actions = append(actions, a)
				}
			}
		case len(d.Data) > 0:
			if a := s.insertFieldAction(docURI, d); a != nil {
				actions = append(actions, a)
			}
		}
	}
	if a := s.scaffoldRefAction(ctx, docURI, params.Range); a != nil {
		actions = append(actions, a)
	}
	return actions, nil
}

// insertFieldAction inserts a missing required field, reading the parent path and
// property from the diagnostic's round-tripped Data and rewriting the document
// through EditYAMLDocument (which preserves comments, order, and indentation).
func (s *Server) insertFieldAction(u uri.URI, d protocol.Diagnostic) *protocol.CodeAction {
	var fd struct {
		Field string `json:"field"`
		Doc   int    `json:"doc"`
		Prop  string `json:"prop"`
	}
	if json.Unmarshal([]byte(d.Data), &fd) != nil || fd.Prop == "" || fd.Doc < 1 {
		return nil
	}
	doc, ok := s.docs.Get(u)
	if !ok {
		return nil
	}
	path := fd.Prop
	if fd.Field != "" && fd.Field != "(root)" {
		path = fd.Field + "." + fd.Prop
	}
	full, _, err := reportspec.EditYAMLDocument(doc.Text, fd.Doc, map[string]any{path: ""})
	if err != nil {
		return nil
	}
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title:       "Add missing field '" + fd.Prop + "'",
		Kind:        &quickFix,
		Diagnostics: []protocol.Diagnostic{d},
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{u: {{Range: wholeDocRange(doc), NewText: full}}},
		},
	}
}

// scaffoldRefAction offers to create the target manifest when the cursor sits on
// a reference whose target does not yet exist.
func (s *Server) scaffoldRefAction(ctx context.Context, u uri.URI, rng protocol.Range) *protocol.CodeAction {
	pc, ok := s.resolve(u, rng.Start)
	if !ok || pc.Kind != reportspec.PosDatasetRef {
		return nil
	}
	name := strings.TrimPrefix(pc.Prefix, "$")
	if name == "" {
		return nil
	}
	kind := pc.RefKind
	if strings.HasPrefix(pc.Prefix, "$") {
		kind = "DataSource" // the $ shorthand always targets a DataSource
	}
	stub := scaffoldStub(kind, name)
	if stub == "" {
		return nil // no stub template for this kind
	}
	if _, found := s.getNameIndex(ctx).Definition(kind, name); found {
		return nil // already declared
	}
	doc, ok := s.docs.Get(u)
	if !ok {
		return nil
	}
	insert := stub
	if doc.Text != "" && !strings.HasSuffix(doc.Text, "\n") {
		insert = "\n" + insert
	}
	end := doc.OffsetToPosition(len(doc.Text))
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title: "Create " + kind + " '" + name + "'",
		Kind:  &quickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{u: {{Range: protocol.Range{Start: end, End: end}, NewText: insert}}},
		},
	}
}

// scaffoldStub returns a minimal manifest (as a new `---` document) for the kinds
// a dangling reference can target.
func scaffoldStub(kind, name string) string {
	switch kind {
	case "DataSet":
		return "\n---\napiVersion: bino.bi/v1alpha1\nkind: DataSet\nmetadata:\n  name: " + name +
			"\nspec:\n  query: SELECT 1\n"
	case "DataSource":
		return "\n---\napiVersion: bino.bi/v1alpha1\nkind: DataSource\nmetadata:\n  name: " + name +
			"\nspec:\n  type: csv\n  path: data.csv\n"
	case "LayoutPage":
		return "\n---\napiVersion: bino.bi/v1alpha1\nkind: LayoutPage\nmetadata:\n  name: " + name +
			"\nspec:\n  children: []\n"
	default:
		return ""
	}
}

// wholeDocRange spans the entire buffer (for a full-document replacement edit).
func wholeDocRange(doc *Document) protocol.Range {
	end := doc.OffsetToPosition(len(doc.Text))
	return protocol.Range{Start: protocol.Position{Line: 0, Character: 0}, End: end}
}

// addEnvVarAction builds a quick-fix appending `NAME=` to an existing project
// .env. It is offered only when the .env exists, so the edit target is known.
func (s *Server) addEnvVarAction(name string, diag protocol.Diagnostic) *protocol.CodeAction {
	if s.root == "" {
		return nil
	}
	envPath := filepath.Join(s.root, ".env")
	content, err := os.ReadFile(envPath)
	if err != nil {
		return nil // no .env to append to
	}
	doc := &Document{Text: string(content)}
	end := doc.OffsetToPosition(len(content))
	insert := name + "=\n"
	if len(content) > 0 && content[len(content)-1] != '\n' {
		insert = "\n" + insert
	}
	edit := protocol.TextEdit{
		Range:   protocol.Range{Start: end, End: end},
		NewText: insert,
	}
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title:       "Add " + name + " to .env",
		Kind:        &quickFix,
		Diagnostics: []protocol.Diagnostic{diag},
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{uri.File(envPath): {edit}},
		},
	}
}

// envVarName extracts the variable name from a missing-env-var message.
func envVarName(message string) string {
	const prefix = "Unresolved environment variable:"
	if !strings.Contains(message, prefix) {
		return ""
	}
	return strings.TrimSpace(message[strings.Index(message, prefix)+len(prefix):])
}

// diagCode reads the string form of a diagnostic code token.
func diagCode(code protocol.ProgressToken) string {
	if s, ok := code.(protocol.String); ok {
		return string(s)
	}
	return ""
}

// diagMessage reads the string form of a diagnostic message (union since 3.18).
func diagMessage(msg protocol.InlayHintTooltip) string {
	switch m := msg.(type) {
	case protocol.String:
		return string(m)
	case *protocol.MarkupContent:
		return m.Value
	default:
		return ""
	}
}
