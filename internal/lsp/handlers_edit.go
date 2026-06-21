package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
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

// CodeAction offers quick-fixes over the diagnostics in range. v1 of phase 2
// handles the missing-env-var fix (append the variable to the project .env).
func (s *Server) CodeAction(_ context.Context, params *protocol.CodeActionParams) ([]protocol.CommandOrCodeAction, error) {
	var actions []protocol.CommandOrCodeAction
	for i := range params.Context.Diagnostics {
		d := params.Context.Diagnostics[i]
		if diagCode(d.Code) != "missing-env-var" {
			continue
		}
		name := envVarName(diagMessage(d.Message))
		if name == "" {
			continue
		}
		if a := s.addEnvVarAction(name, d); a != nil {
			actions = append(actions, a)
		}
	}
	return actions, nil
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
