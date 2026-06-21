package lsp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

func codeActionTitles(actions []protocol.CommandOrCodeAction) map[string]*protocol.CodeAction {
	out := make(map[string]*protocol.CodeAction)
	for _, a := range actions {
		if ca, ok := a.(*protocol.CodeAction); ok {
			out[ca.Title] = ca
		}
	}
	return out
}

func TestCodeAction_ScaffoldMissingDataset(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/r.yaml")
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: missing_ds\n", 1)

	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{Start: protocol.Position{Line: 4, Character: 12}, End: protocol.Position{Line: 4, Character: 12}},
	})
	ca, ok := codeActionTitles(actions)["Create DataSet 'missing_ds'"]
	if !ok {
		t.Fatalf("expected a scaffold action, got %d actions", len(actions))
	}
	edits := ca.Edit.Changes[u]
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "kind: DataSet") || !strings.Contains(edits[0].NewText, "name: missing_ds") {
		t.Errorf("scaffold edit should append a DataSet stub, got %q", edits[0].NewText)
	}
}

func TestCodeAction_ScaffoldSkipsExisting(t *testing.T) {
	s, _, reportURI := navServer(t) // report.yaml references the existing DataSet "sales"
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: reportURI},
		Range:        protocol.Range{Start: protocol.Position{Line: 4, Character: 12}, End: protocol.Position{Line: 4, Character: 12}},
	})
	if _, ok := codeActionTitles(actions)["Create DataSet 'sales'"]; ok {
		t.Error("should not offer to scaffold an already-declared dataset")
	}
}

func TestCodeAction_InsertMissingField(t *testing.T) {
	s := newTestServer()
	u := uri.File("/proj/r.yaml")
	s.docs.Set(u, "kind: Table\nmetadata:\n  name: t\nspec:\n  dataset: sales\n", 1)

	data, _ := json.Marshal(map[string]any{"field": "spec", "doc": 1, "prop": "title"})
	diag := protocol.Diagnostic{
		Range:   protocol.Range{Start: protocol.Position{Line: 3, Character: 0}, End: protocol.Position{Line: 3, Character: 4}},
		Message: protocol.String("missing property 'title'"),
		Code:    protocol.String("schema-validation"),
		Data:    protocol.LSPAny(data),
	}
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        diag.Range,
		Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diag}},
	})
	ca, ok := codeActionTitles(actions)["Add missing field 'title'"]
	if !ok {
		t.Fatalf("expected an insert-field action, got %d actions", len(actions))
	}
	edits := ca.Edit.Changes[u]
	if len(edits) != 1 || !strings.Contains(edits[0].NewText, "title:") {
		t.Errorf("insert-field edit should add 'title:', got %q", edits[0].NewText)
	}
}
