package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
	"gopkg.in/yaml.v3"

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
	doc, ok := s.docs.Get(params.TextDocument.URI)
	if !ok {
		return nil, nil
	}
	return &protocol.PrepareRenamePlaceholder{
		Range:       doc.RangeToProtocol(pc.ReplaceRange),
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
		changes[u] = append(changes[u], protocol.TextEdit{Range: s.rangeToProtocolFor(def.File, def.NameRange), NewText: params.NewName})
	}
	for _, r := range ni.References(kind, name) {
		u := uri.File(r.File)
		newText := params.NewName
		if r.Dollar {
			newText = "$" + newText
		}
		changes[u] = append(changes[u], protocol.TextEdit{Range: s.rangeToProtocolFor(r.File, r.Range), NewText: newText})
	}
	if len(changes) == 0 {
		return nil, nil
	}
	return &protocol.WorkspaceEdit{Changes: changes}, nil
}

// CodeAction offers quick-fixes: append a missing ${VAR} to .env, add a
// DataSource the query reads to spec.dependencies, insert a missing required
// field, and scaffold a dangling reference's target manifest.
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
		case diagCode(d.Code) == "dataset-dependency-undeclared":
			if name := dependencyName(diagMessage(d.Message)); name != "" {
				if a := s.addDependencyAction(docURI, d, name); a != nil {
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
	if a := s.addMissingParamsAction(ctx, docURI, params.Range); a != nil {
		actions = append(actions, a)
	}
	if a := s.quoteAtValueAction(docURI, params.Range); a != nil {
		actions = append(actions, a)
	}
	return actions, nil
}

// quoteAtValueAction offers to quote an unquoted `@...` value on the cursor
// line — a YAML parse error (`@` is a reserved indicator) that every hand-typed
// registry ref hits.
func (s *Server) quoteAtValueAction(u uri.URI, rng protocol.Range) *protocol.CodeAction {
	doc, ok := s.docs.Get(u)
	if !ok {
		return nil
	}
	_, token, raw, ok := reportspec.RepairUnquotedAt(doc.Text, int(rng.Start.Line)+1)
	if !ok {
		return nil
	}
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title: "Quote '" + token + "' (YAML reserves '@')",
		Kind:  &quickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{u: {{Range: doc.RangeToProtocol(raw), NewText: "\"" + token + "\""}}},
		},
	}
}

// addMissingParamsAction offers to insert the params a ref target requires
// (required, no default) that the child does not pass yet. It is position-driven
// like scaffoldRefAction: ref-params lint findings carry no position to anchor a
// diagnostic-driven fix on.
func (s *Server) addMissingParamsAction(ctx context.Context, u uri.URI, rng protocol.Range) *protocol.CodeAction {
	pc, ok := s.resolve(u, rng.Start)
	if !ok || pc.RefName == "" {
		return nil
	}
	onRefValue := pc.Kind == reportspec.PosDatasetRef && pc.FieldName == "ref"
	if !onRefValue && pc.Kind != reportspec.PosParamKey {
		return nil
	}
	decls, ok := s.paramsForTarget(ctx, pc.RefKind, pc.RefName)
	if !ok {
		return nil
	}
	present := keySet(pc.PresentKeys)
	base := paramsPatchBase(pc)
	patch := make(map[string]any)
	for _, p := range decls {
		if p.Required && p.Default == nil && !present[p.Name] {
			patch[base+"."+p.Name] = ""
		}
	}
	if len(patch) == 0 {
		return nil
	}
	doc, ok := s.docs.Get(u)
	if !ok {
		return nil
	}
	full, _, err := reportspec.EditYAMLDocument(doc.Text, pc.DocIndex+1, patch)
	if err != nil {
		return nil
	}
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title: "Add required params for '" + pc.RefName + "'",
		Kind:  &quickFix,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{u: {{Range: wholeDocRange(doc), NewText: full}}},
		},
	}
}

// paramsPatchBase converts a resolver path to the EditYAMLDocument path of the
// child's params mapping: `spec.children.0.ref` → `spec.children[0].params`.
func paramsPatchBase(pc reportspec.PositionContext) string {
	segs := strings.Split(pc.Path, ".")
	if pc.Kind == reportspec.PosDatasetRef {
		segs[len(segs)-1] = "params" // replace the trailing "ref"
	}
	var b strings.Builder
	for _, seg := range segs {
		if _, err := strconv.Atoi(seg); err == nil {
			b.WriteByte('[')
			b.WriteString(seg)
			b.WriteByte(']')
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(seg)
	}
	return b.String()
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

// addDependencyAction builds a quick-fix adding a DataSource the query reads to
// spec.dependencies. The usual shapes get a plain text insert, so blank lines,
// indentation and block scalars stay as written; other shapes fall back to
// rewriting the document.
func (s *Server) addDependencyAction(u uri.URI, d protocol.Diagnostic, name string) *protocol.CodeAction {
	pc, ok := s.resolve(u, d.Range.Start)
	if !ok {
		return nil
	}
	doc, ok := s.docs.Get(u)
	if !ok {
		return nil
	}
	nodes, _ := reportspec.ParseYAMLNodes(doc.Text) //nolint:errcheck // lenient parse; a document the parse did not reach is out of range below
	if pc.DocIndex >= len(nodes) {
		return nil
	}
	// The diagnostic stays until save, so re-check the buffer it points at.
	root := nodes[pc.DocIndex]
	if _, kind := mappingEntry(root, "kind"); kind == nil || kind.Value != "DataSet" {
		return nil
	}
	specKey, spec := mappingEntry(root, "spec")
	// An empty YAML document shifts the finding onto the wrong document; never
	// edit a DataSet whose query does not even mention the name.
	if _, query := mappingEntry(spec, "query"); query != nil && query.Kind == yaml.ScalarNode &&
		!strings.Contains(strings.ToLower(query.Value), strings.ToLower(name)) {
		return nil
	}
	_, deps := mappingEntry(spec, "dependencies")
	if deps != nil {
		for _, item := range deps.Content {
			if item.Kind == yaml.ScalarNode && item.Value == name {
				return nil // already listed
			}
		}
	}
	out, err := yaml.Marshal(name) // quotes names YAML reserves, e.g. @scope/name
	if err != nil {
		return nil
	}
	value := strings.TrimSuffix(string(out), "\n")
	newline := "\n"
	if strings.Contains(doc.Text, "\r\n") {
		newline = "\r\n"
	}

	var edit protocol.TextEdit
	switch {
	case deps != nil && deps.Kind == yaml.SequenceNode && deps.Style&yaml.FlowStyle == 0 && len(deps.Content) > 0 &&
		oneLineScalar(doc, deps.Content[len(deps.Content)-1]):
		// Indent like the last item's own "- " line; the sequence node's column
		// is on the key line when the list carries an anchor or tag.
		last := deps.Content[len(deps.Content)-1]
		line, _ := doc.lineText(last.Line)
		at := lineSpan(doc, last.Line, 1).End
		edit = protocol.TextEdit{
			Range:   protocol.Range{Start: at, End: at},
			NewText: newline + line[:len(line)-len(strings.TrimLeft(line, " "))] + "- " + value,
		}
	case deps == nil && spec != nil && spec.Kind == yaml.MappingNode && spec.Style&yaml.FlowStyle == 0 && len(spec.Content) > 0:
		indent := spec.Content[0].Column - 1
		step := indent - (specKey.Column - 1)
		if step <= 0 {
			step = 2
		}
		at := lineSpan(doc, specKey.Line, 1).End
		edit = protocol.TextEdit{
			Range: protocol.Range{Start: at, End: at},
			NewText: newline + strings.Repeat(" ", indent) + "dependencies:" +
				newline + strings.Repeat(" ", indent+step) + "- " + value,
		}
	default:
		var full string
		if deps != nil && deps.Tag == "!!null" {
			// AppendYAMLSequence rejects a null value; replace it instead.
			full, _, err = reportspec.EditYAMLDocument(doc.Text, pc.DocIndex+1, map[string]any{"spec.dependencies": []any{name}})
		} else {
			full, _, err = reportspec.AppendYAMLSequence(doc.Text, pc.DocIndex+1, "spec.dependencies", name)
		}
		if err != nil {
			return nil
		}
		edit = protocol.TextEdit{Range: wholeDocRange(doc), NewText: full}
	}
	quickFix := protocol.CodeActionKindQuickFix
	return &protocol.CodeAction{
		Title:       "Add " + name + " to dependencies",
		Kind:        &quickFix,
		Diagnostics: []protocol.Diagnostic{d},
		Edit: &protocol.WorkspaceEdit{
			Changes: map[uri.URI][]protocol.TextEdit{u: {edit}},
		},
	}
}

// mappingEntry returns the key and value nodes for key in a mapping node, or
// (nil, nil) when n is not a mapping or has no such key.
func mappingEntry(n *yaml.Node, key string) (k, v *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i], n.Content[i+1]
		}
	}
	return nil, nil
}

// oneLineScalar reports whether a block sequence item is a scalar written whole
// on its own "- " line, so a new item can go right after that line.
func oneLineScalar(doc *Document, item *yaml.Node) bool {
	if item.Kind != yaml.ScalarNode || item.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return false
	}
	line, ok := doc.lineText(item.Line)
	if !ok {
		return false
	}
	var items []string
	return yaml.Unmarshal([]byte(strings.TrimSpace(line)), &items) == nil && len(items) == 1 && items[0] == item.Value
}

// envVarName extracts the variable name from a missing-env-var message.
func envVarName(message string) string {
	const prefix = "Unresolved environment variable:"
	if !strings.Contains(message, prefix) {
		return ""
	}
	return strings.TrimSpace(message[strings.Index(message, prefix)+len(prefix):])
}

// dependencyName extracts the DataSource name from a
// dataset-dependency-undeclared message.
func dependencyName(message string) string {
	_, rest, ok := strings.Cut(message, `query reads DataSource "`)
	if !ok {
		return ""
	}
	name, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return name
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
