package lsp

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"bino.bi/bino/internal/logx"
)

const registryPackageDoc = `kind: Table
apiVersion: bino.bi/v1
metadata:
  name: "@acme/kpi-card"
  params:
    - name: REGION
      type: select
      required: true
      description: Region to report on.
      options:
        items:
          - value: eu
            label: Europe
          - value: us
            label: United States
    - name: LIMIT
      type: number
      default: "10"
spec:
  dataset: sales
  title: KPI Card
`

const localTablesDoc = `kind: Table
metadata:
  name: local_table
spec:
  title: Local
---
kind: DataSet
metadata:
  name: sales
spec:
  query: select 1
`

const lockfileDoc = `lockfile_version = 1

[[package]]
name = "@acme/kpi-card"
version = "1.2.0"
tag = "latest"
digest = "sha256:abc"
kind = "Table"
path = ".bino/registry/acme/kpi-card.yml"
direct = true
dependencies = []
`

// layoutDoc references the registry package from a layout child. 0-based lines:
// 6 = ref value, 8 = REGION param, 9 = blank slot for a new param key.
const layoutDoc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: "@acme/kpi-card"
      params:
        REGION: eu
        ` + `
`

// newRegistryTestServer materializes a project root with one installed registry
// package (locked), a local Table, and a DataSet, and returns a server whose
// index points at those real files.
func newRegistryTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	regFile := filepath.Join(root, ".bino", "registry", "acme", "kpi-card.yml")
	if err := os.MkdirAll(filepath.Dir(regFile), 0o755); err != nil {
		t.Fatal(err)
	}
	localFile := filepath.Join(root, "cards.yaml")
	lockFile := filepath.Join(root, "bino.lock")
	for path, content := range map[string]string{
		regFile:   registryPackageDoc,
		localFile: localTablesDoc,
		lockFile:  lockfileDoc,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	be := &fakeBackend{
		index: []IndexDoc{
			{Kind: "Table", Name: "@acme/kpi-card", File: regFile, Position: 1},
			{Kind: "Table", Name: "local_table", File: localFile, Position: 1},
			{Kind: "DataSet", Name: "sales", File: localFile, Position: 2},
		},
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	return NewServer(be, log, true, root), root
}

func openLayoutDoc(t *testing.T, s *Server, root string) uri.URI {
	t.Helper()
	u := uri.File(filepath.Join(root, "page.yaml"))
	s.docs.Set(u, layoutDoc, 1)
	return u
}

func completionItems(t *testing.T, res protocol.CompletionResult) []protocol.CompletionItem {
	t.Helper()
	switch v := res.(type) {
	case protocol.CompletionItemSlice:
		return v
	case *protocol.CompletionList:
		return v.Items
	case nil:
		return nil
	default:
		t.Fatalf("unexpected completion result type %T", res)
		return nil
	}
}

func findItem(items []protocol.CompletionItem, label string) *protocol.CompletionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func TestCompletion_RefFilteredBySiblingKind(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 14)) // inside the ref value
	items := completionItems(t, res)
	labels := completionLabels(t, res)
	if !contains(labels, "@acme/kpi-card") || !contains(labels, "local_table") {
		t.Fatalf("ref completion should offer both Table docs, got %v", labels)
	}
	if contains(labels, "sales") {
		t.Errorf("ref completion must filter to the sibling kind Table; DataSet 'sales' leaked (got %v)", labels)
	}

	reg := findItem(items, "@acme/kpi-card")
	if detail, ok := reg.Detail.Get(); !ok || !strings.Contains(detail, "registry") || !strings.Contains(detail, "1.2.0") {
		t.Errorf("registry package detail should carry origin+version, got %q", detail)
	}
	// The buffer value is already quoted, so the text edit replaces the content
	// without adding quotes.
	edit, ok := reg.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("ref completion should carry a TextEdit, got %T", reg.TextEdit)
	}
	if edit.NewText != "@acme/kpi-card" {
		t.Errorf("quoted buffer: NewText = %q, want the bare name", edit.NewText)
	}
	if edit.Range.Start.Character != 12 || edit.Range.End.Character != 26 {
		t.Errorf("TextEdit range should cover the quoted content only, got %+v", edit.Range)
	}
}

func TestCompletion_RefQuotesRegistryNamesInPlainContext(t *testing.T) {
	s, root := newRegistryTestServer(t)
	// The ref value is a plain scalar; accepting a registry name must add quotes.
	doc := strings.Replace(layoutDoc, `ref: "@acme/kpi-card"`, "ref: intro", 1)
	u := uri.File(filepath.Join(root, "page.yaml"))
	s.docs.Set(u, doc, 1)
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 13))
	reg := findItem(completionItems(t, res), "@acme/kpi-card")
	if reg == nil {
		t.Fatal("registry package missing from ref completion")
	}
	edit, ok := reg.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("expected TextEdit, got %T", reg.TextEdit)
	}
	if edit.NewText != `"@acme/kpi-card"` {
		t.Errorf("plain buffer: NewText = %q, want the quoted name", edit.NewText)
	}
	local := findItem(completionItems(t, res), "local_table")
	if e, ok := local.TextEdit.(*protocol.TextEdit); !ok || e.NewText != "local_table" {
		t.Errorf("local names stay unquoted, got %+v", local.TextEdit)
	}
}

func TestCompletion_UnquotedAtRefStillCompletes(t *testing.T) {
	s, root := newRegistryTestServer(t)
	// The unquoted @ makes the whole document unparseable — exactly while the
	// author types a registry ref. Completion must repair, resolve with the
	// sibling kind, and replace the raw token with a QUOTED value.
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: @acme/kpi-c
`
	u := uri.File(filepath.Join(root, "page.yaml"))
	s.docs.Set(u, doc, 1)
	res, _ := s.Completion(context.Background(), completionParams(u, 6, 22))
	items := completionItems(t, res)
	reg := findItem(items, "@acme/kpi-card")
	if reg == nil {
		t.Fatalf("registry package missing from repaired ref completion, got %v", completionLabels(t, res))
	}
	edit, ok := reg.TextEdit.(*protocol.TextEdit)
	if !ok {
		t.Fatalf("expected TextEdit, got %T", reg.TextEdit)
	}
	if edit.NewText != `"@acme/kpi-card"` {
		t.Errorf("NewText = %q, want the quoted name", edit.NewText)
	}
	wantRange := protocol.Range{
		Start: protocol.Position{Line: 6, Character: 11},
		End:   protocol.Position{Line: 6, Character: 11 + uint32(len("@acme/kpi-c"))},
	}
	if edit.Range != wantRange {
		t.Errorf("TextEdit range = %+v, want the raw token span %+v", edit.Range, wantRange)
	}
}

func TestCodeAction_QuoteAtValue(t *testing.T) {
	s, root := newRegistryTestServer(t)
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: @acme/kpi-card
`
	u := uri.File(filepath.Join(root, "page.yaml"))
	s.docs.Set(u, doc, 1)
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{Start: protocol.Position{Line: 6, Character: 14}, End: protocol.Position{Line: 6, Character: 14}},
	})
	var quote *protocol.CodeAction
	for _, a := range actions {
		if ca, ok := a.(*protocol.CodeAction); ok && strings.HasPrefix(ca.Title, "Quote '@acme/kpi-card'") {
			quote = ca
		}
	}
	if quote == nil {
		t.Fatalf("expected a quote quick fix, got %d actions", len(actions))
	}
	edits := quote.Edit.Changes[u]
	if len(edits) != 1 || edits[0].NewText != `"@acme/kpi-card"` {
		t.Errorf("quote edit should wrap the value in quotes, got %+v", edits)
	}
}

func TestCompletion_ParamKeys(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	res, _ := s.Completion(context.Background(), completionParams(u, 9, 8)) // blank line under REGION
	items := completionItems(t, res)
	labels := completionLabels(t, res)
	if !contains(labels, "LIMIT") {
		t.Fatalf("param-key completion should offer the declared LIMIT, got %v", labels)
	}
	if contains(labels, "REGION") {
		t.Errorf("already-present REGION should be skipped, got %v", labels)
	}
	limit := findItem(items, "LIMIT")
	if detail, ok := limit.Detail.Get(); !ok || !strings.Contains(detail, "number") || !strings.Contains(detail, "default: 10") {
		t.Errorf("LIMIT detail should carry type and default, got %q", detail)
	}
}

func TestCompletion_ParamKeysReflectUnsavedTargetEdits(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	// Open the installed package with an unsaved extra declaration; the overlay
	// must win over the on-disk file.
	edited := strings.Replace(registryPackageDoc,
		"    - name: LIMIT",
		"    - name: CURRENCY\n    - name: LIMIT", 1)
	s.docs.Set(uri.File(filepath.Join(root, ".bino", "registry", "acme", "kpi-card.yml")), edited, 1)
	res, _ := s.Completion(context.Background(), completionParams(u, 9, 8))
	if labels := completionLabels(t, res); !contains(labels, "CURRENCY") {
		t.Errorf("param-key completion should reflect unsaved target edits, got %v", labels)
	}
}

func TestCompletion_ParamValues(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	res, _ := s.Completion(context.Background(), completionParams(u, 8, 17)) // on "eu"
	items := completionItems(t, res)
	labels := completionLabels(t, res)
	if !contains(labels, "eu") || !contains(labels, "us") {
		t.Fatalf("select param completion should offer declared options, got %v", labels)
	}
	if eu := findItem(items, "eu"); eu.Documentation == nil {
		t.Errorf("select option should carry its label as documentation")
	}
}

func TestHover_RefTarget(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	h, _ := s.Hover(context.Background(), hoverParams(u, 6, 14))
	if h == nil {
		t.Fatal("expected hover on the ref value")
	}
	mc, ok := h.Contents.(*protocol.MarkupContent)
	if !ok {
		t.Fatalf("unexpected hover contents type %T", h.Contents)
	}
	for _, want := range []string{"@acme/kpi-card", "Table", "registry 1.2.0", "tag: latest", "REGION", "Region to report on."} {
		if !strings.Contains(mc.Value, want) {
			t.Errorf("ref hover missing %q:\n%s", want, mc.Value)
		}
	}
}

func TestHover_ParamValue(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	h, _ := s.Hover(context.Background(), hoverParams(u, 8, 17))
	if h == nil {
		t.Fatal("expected hover on a param value")
	}
	mc, ok := h.Contents.(*protocol.MarkupContent)
	if !ok {
		t.Fatalf("unexpected hover contents type %T", h.Contents)
	}
	for _, want := range []string{"REGION", "select", "required", "`eu`", "Europe"} {
		if !strings.Contains(mc.Value, want) {
			t.Errorf("param hover missing %q:\n%s", want, mc.Value)
		}
	}
}

func TestCodeAction_AddRequiredParams(t *testing.T) {
	s, root := newRegistryTestServer(t)
	// The child passes no params; REGION (required, no default) must be offered,
	// LIMIT (has a default) must not.
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: "@acme/kpi-card"
`
	u := uri.File(filepath.Join(root, "page.yaml"))
	s.docs.Set(u, doc, 1)
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{Start: protocol.Position{Line: 6, Character: 14}, End: protocol.Position{Line: 6, Character: 14}},
	})
	ca, ok := codeActionTitles(actions)["Add required params for '@acme/kpi-card'"]
	if !ok {
		t.Fatalf("expected an add-required-params action, got %d actions", len(actions))
	}
	edits := ca.Edit.Changes[u]
	if len(edits) != 1 {
		t.Fatalf("expected one whole-document edit, got %d", len(edits))
	}
	text := edits[0].NewText
	if !strings.Contains(text, "params:") || !strings.Contains(text, "REGION:") {
		t.Errorf("edit should create the params mapping with REGION, got:\n%s", text)
	}
	if strings.Contains(text, "LIMIT") {
		t.Errorf("defaulted LIMIT must not be inserted, got:\n%s", text)
	}
}

func TestCodeAction_AddRequiredParamsSkipsSatisfiedRef(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root) // layoutDoc already passes REGION
	actions, _ := s.CodeAction(context.Background(), &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: u},
		Range:        protocol.Range{Start: protocol.Position{Line: 6, Character: 14}, End: protocol.Position{Line: 6, Character: 14}},
	})
	if _, ok := codeActionTitles(actions)["Add required params for '@acme/kpi-card'"]; ok {
		t.Error("should not offer required params when all are already passed")
	}
}

func TestDefinition_RefChildSiblingKind(t *testing.T) {
	s, root := newRegistryTestServer(t)
	u := openLayoutDoc(t, s, root)
	res, err := s.Definition(context.Background(), &protocol.DefinitionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: u},
			Position:     protocol.Position{Line: 6, Character: 14},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	locs, ok := res.(protocol.LocationSlice)
	if !ok || len(locs) != 1 {
		t.Fatalf("expected one definition location, got %#v", res)
	}
	wantFile := filepath.Join(root, ".bino", "registry", "acme", "kpi-card.yml")
	if got := locs[0].URI.FsPath(); got != wantFile {
		t.Errorf("definition file = %s, want the installed package %s", got, wantFile)
	}
}

// Every document of a multi-file package must carry the registry annotation,
// not only the one bino.lock names as the package's primary document.
func TestPackageOriginsCoversEveryFileOfATree(t *testing.T) {
	root := t.TempDir()
	lock := `lockfile_version = 2

[[package]]
name = "@acme/kit"
version = "2.0.0"
tag = "latest"
digest = "sha256:manifest"
format = "tree"
kind = "LayoutPage"
path = ".bino/registry/acme/kit/kit.yaml"
direct = true
dependencies = []
kinds = ["LayoutPage", "Table"]

[[package.files]]
path = "kit.yaml"
type = "document"
digest = "sha256:a"

[[package.files]]
path = "components/sales.yaml"
type = "document"
digest = "sha256:b"
`
	if err := os.WriteFile(filepath.Join(root, "bino.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	log := logx.NewTerminalWithColor(io.Discard, io.Discard, false, true).Channel("test")
	s := NewServer(&fakeBackend{}, log, true, root)
	origins := s.packageOrigins()
	for _, rel := range []string{".bino/registry/acme/kit/kit.yaml", ".bino/registry/acme/kit/components/sales.yaml"} {
		key := normPath(filepath.Join(root, filepath.FromSlash(rel)))
		o, ok := origins[key]
		if !ok {
			t.Fatalf("no origin for %s; got %v", rel, origins)
		}
		if o.Name != "@acme/kit" || o.Version != "2.0.0" || o.Tag != "latest" {
			t.Errorf("%s origin = %+v", rel, o)
		}
	}
}
