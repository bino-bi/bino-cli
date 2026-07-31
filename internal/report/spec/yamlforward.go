package spec

import (
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Range is a 1-based source span. EndCol is exclusive. It mirrors the
// coordinate system of yaml.v3 nodes (1-based Line/Column) so it composes with
// ResolvePathPosition; the LSP layer converts to/from 0-based LSP positions.
type Range struct {
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// PositionKind classifies what the cursor sits on, so a completion/hover caller
// knows which candidate set applies without re-deriving context.
type PositionKind int

const (
	// PosUnknown means the cursor is outside any document body (on a separator,
	// past EOF, or in unparseable content).
	PosUnknown PositionKind = iota
	// PosKey is a mapping key position — suggest field names from the schema.
	PosKey
	// PosKindValue is the value of the top-level `kind:` field.
	PosKindValue
	// PosScenarioItem is an item under a `scenarios:` sequence.
	PosScenarioItem
	// PosVarianceItem is an item under a `variances:` sequence.
	PosVarianceItem
	// PosDatasetRef is a value that references another manifest by name
	// (dataset/source/ref/page/...). RefKind names the valid target kind.
	PosDatasetRef
	// PosQueryScalar is inside a `query:`/`prql:` block scalar (phase 2).
	PosQueryScalar
	// PosFreeValue is a scalar value with no special completion semantics; the
	// completion layer may upgrade it to an enum when the schema says so.
	PosFreeValue
	// PosParamKey is a key position inside a ref child's `params:` mapping —
	// suggest the target document's declared param names.
	PosParamKey
	// PosParamValue is a value position inside a ref child's `params:` mapping.
	PosParamValue
)

// PositionContext is the resolver's verdict for a cursor: the dotted path, the
// enclosing manifest kind, and enough context to assemble candidates.
type PositionContext struct {
	Kind          PositionKind
	Path          string   // dotted path (same vocabulary as ResolvePathPosition)
	EnclosingKind string   // the deepest enclosing `kind:` value on the cursor's path
	FieldName     string   // the immediate field name under the cursor
	RefKind       string   // for PosDatasetRef / param positions: the valid target kind
	RefName       string   // for `ref` values and param positions: the target ref/page name
	PresentKeys   []string // for PosKey / PosParamKey / `ref` values: keys already present on the mapping
	Prefix        string   // already-typed value text (for filtering / replace)
	ReplaceRange  Range    // where an accepted completion should be written
	DocIndex      int      // 0-based document ordinal within a multi-doc file
	BoundDatasets []string
	// KindsByPath maps each mapping's dotted path on the cursor's descent to
	// that mapping's `kind:` value ("" key = document root). Schema resolution
	// needs these discriminators to pick the right if/then branch per level.
	KindsByPath map[string]string
}

// refFieldKinds maps a simple reference field name to the kind it targets. The
// special fields (dataset, ref, layoutPages) are resolved contextually below.
// This is the single source of truth shared with the name→location index.
var refFieldKinds = map[string]string{
	"source":         "DataSource",
	"dependencies":   "DataSource",
	"secret":         "ConnectionSecret",
	"page":           "LayoutPage",
	"signingProfile": "SigningProfile",
	"selectedStyle":  "ComponentStyle",
	"ruleset":        "RuleSet",
}

// ResolvePositionPath maps a 1-based cursor (line, col) within raw multi-document
// YAML to a PositionContext. It parses the RAW buffer — never env-expanded text —
// so positions stay honest (see loader's ${VAR} expansion).
//
// Typing resilience: documents are isolated per `---` slice, so a syntax error
// in one document never darkens resolution in the others; a slice the
// whole-buffer parse did not reach is parsed alone and its positions shifted
// back. Empty buffers, fresh slices after a trailing `---`, and a bare scalar
// being typed at the root all resolve as root key positions instead of
// failing. ok=false only when the cursor's own document is unparseable.
func ResolvePositionPath(content string, line, col int) (PositionContext, bool) {
	slices := splitDocSlices(content)
	idx := sliceIndexFor(slices, line)
	sl := slices[idx]

	// The whole-buffer parse is authoritative where it succeeds: its nodes
	// carry absolute lines and define the DocIndex ordinals (which skip empty
	// sections — the edit pipeline counts documents the same way). A parse
	// error keeps the prefix nodes.
	nodes, _ := ParseYAMLNodes(content)
	var root *yaml.Node
	docIdx := len(nodes)
	lineOffset := 0
	for i, n := range nodes {
		if n != nil && n.Line >= sl.startLine && n.Line <= sl.endLine {
			root = n
			docIdx = i
			break
		}
	}
	if root == nil {
		if strings.TrimSpace(sl.text) == "" {
			return rootlessContext(nil, line, col, docIdx, 0)
		}
		subNodes, subErr := ParseYAMLNodes(sl.text)
		if len(subNodes) == 0 {
			if subErr != nil {
				return PositionContext{}, false // the cursor's own document is unparseable
			}
			return rootlessContext(nil, line, col, docIdx, 0)
		}
		root = subNodes[0]
		lineOffset = sl.startLine - 1
	}
	if root.Kind != yaml.MappingNode {
		return rootlessContext(root, line, col, docIdx, lineOffset)
	}

	w := &walker{cursorLine: line - lineOffset, cursorCol: col, enclosingKind: docKind(root), docIndex: docIdx, kindsByPath: map[string]string{}}
	ctx, ok := w.descend(root, nil, sl.endLine+1-lineOffset)
	if !ok {
		return PositionContext{}, false
	}
	ctx.EnclosingKind = w.enclosingKind
	ctx.DocIndex = docIdx
	ctx.KindsByPath = w.kindsByPath
	switch ctx.Kind {
	case PosScenarioItem, PosVarianceItem, PosQueryScalar:
		ctx.BoundDatasets = w.boundDatasets
	default:
	}
	ctx.ReplaceRange.StartLine += lineOffset
	ctx.ReplaceRange.EndLine += lineOffset
	return ctx, true
}

// rootlessContext resolves a cursor in a document with no mapping root yet: an
// empty buffer, a fresh slice after `---`, or a bare scalar being typed at the
// root ("kin"). All are root key positions, so completion can offer the
// document skeleton.
func rootlessContext(root *yaml.Node, line, col, docIdx, lineOffset int) (PositionContext, bool) {
	ctx := PositionContext{
		Kind:         PosKey,
		Path:         "(root)",
		DocIndex:     docIdx,
		ReplaceRange: atCursor(line, col),
		KindsByPath:  map[string]string{},
	}
	if root != nil && root.Kind == yaml.ScalarNode && root.Value != "" {
		ctx.Prefix = root.Value
		r := valueRange(root, line, col)
		r.StartLine += lineOffset
		r.EndLine += lineOffset
		ctx.ReplaceRange = r
	}
	return ctx, true
}

// docSlice is one document's region of a multi-doc buffer, split on `---`
// separator lines. startLine/endLine are 1-based inclusive (endLine may be
// startLine-1 for an empty slice); text carries the slice's own lines with no
// separators, so a slice-local parse can be shifted back by startLine-1.
type docSlice struct {
	startLine, endLine int
	text               string
}

// splitDocSlices cuts a buffer into per-document slices on `---` separator
// lines. Separator lines belong to no slice. The result always has at least
// one slice (possibly empty), covering line 1.
func splitDocSlices(content string) []docSlice {
	lines := strings.Split(content, "\n")
	var out []docSlice
	start := 1
	for i, ln := range lines {
		if isDocSeparator(ln) {
			out = append(out, makeSlice(lines, start, i)) // lines start..i (1-based, i = separator-1+1)
			start = i + 2
		}
	}
	out = append(out, makeSlice(lines, start, len(lines)))
	return out
}

// makeSlice builds the slice covering 1-based lines start..end inclusive.
func makeSlice(lines []string, start, end int) docSlice {
	if end < start {
		return docSlice{startLine: start, endLine: end}
	}
	return docSlice{startLine: start, endLine: end, text: strings.Join(lines[start-1:end], "\n")}
}

// isDocSeparator matches a YAML document separator line (`---`, optionally
// with trailing whitespace).
func isDocSeparator(line string) bool {
	rest, ok := strings.CutPrefix(line, "---")
	return ok && strings.TrimSpace(rest) == ""
}

// sliceIndexFor picks the slice owning the cursor line; a cursor on a
// separator line belongs to the preceding slice, one past EOF to the last.
func sliceIndexFor(slices []docSlice, line int) int {
	for i, sl := range slices {
		if line < sl.startLine {
			if i > 0 {
				return i - 1
			}
			return 0
		}
		if line <= sl.endLine {
			return i
		}
	}
	return len(slices) - 1
}

// RepairUnquotedAt scans the cursor line for an unquoted `@...` scalar value
// (`ref: @scope/name`) — invalid YAML, since `@` is a reserved indicator, so
// the whole document fails to parse and position resolution goes dark exactly
// while an author types a registry ref. It returns the content with that token
// quoted, the token itself, and the token's raw (1-based, end-exclusive) span
// in the original content. ok=false when the line doesn't match.
func RepairUnquotedAt(content string, line int) (repaired, token string, raw Range, ok bool) {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return "", "", Range{}, false
	}
	m := unquotedAtValueRe.FindStringSubmatchIndex(lines[line-1])
	if m == nil {
		return "", "", Range{}, false
	}
	src := lines[line-1]
	start, end := m[2], m[3] // the @token submatch, 0-based byte offsets
	token = src[start:end]
	lines[line-1] = src[:start] + `"` + token + `"` + src[end:]
	raw = Range{StartLine: line, StartCol: start + 1, EndLine: line, EndCol: end + 1}
	return strings.Join(lines, "\n"), token, raw, true
}

// unquotedAtValueRe matches a mapping value starting with the YAML-reserved
// `@` (optionally inside a sequence item), e.g. `ref: @acme/kpi`.
var unquotedAtValueRe = regexp.MustCompile(`^\s*(?:-\s+)?[A-Za-z][A-Za-z0-9_-]*:\s+(@\S*)\s*$`)

// refTarget identifies the component reference (sibling kind + ref/page name)
// a `params:` mapping belongs to.
type refTarget struct {
	kind string
	name string
}

// walker carries the per-resolution cursor and document context through the
// recursive descent.
type walker struct {
	cursorLine    int
	cursorCol     int
	enclosingKind string
	docIndex      int
	kindsByPath   map[string]string
	boundDatasets []string
	refTarget     refTarget
}

// descend walks node looking for the deepest node owning the cursor, building
// the dotted path as it goes. parentEnd is the exclusive line upper-bound the
// node's content may occupy.
func (w *walker) descend(node *yaml.Node, path []string, parentEnd int) (PositionContext, bool) {
	switch node.Kind {
	case yaml.MappingNode:
		return w.descendMapping(node, path, parentEnd)
	case yaml.SequenceNode:
		return w.descendSequence(node, path, parentEnd)
	default:
		return w.classifyScalar(node, path), true
	}
}

func (w *walker) descendMapping(node *yaml.Node, path []string, parentEnd int) (PositionContext, bool) {
	// Track the nearest enclosing dataset binding and component kind as we
	// descend, so a scenario / variance / query / field inside a layout child
	// resolves against that child (kind: Table, its dataset), not the document
	// root (kind: LayoutPage). The deepest mapping on the path wins.
	if ds := bindingDatasets(node); len(ds) > 0 {
		w.boundDatasets = ds
	}
	if k := mappingChildValue(node, "kind"); k != "" {
		w.enclosingKind = k
		w.kindsByPath[kindsKey(path)] = k
	}
	// A mapping with `kind`+`ref` (layout/grid/tree child) or `page`+`params`
	// (layoutPages object form) is a component reference: remember it so a
	// nested `params:` position resolves against the referenced document.
	if r := mappingChildValue(node, "ref"); r != "" {
		if k := mappingChildValue(node, "kind"); k != "" {
			w.refTarget = refTarget{kind: k, name: r}
		}
	} else if p := mappingChildValue(node, "page"); p != "" && mappingChild(node, "params") != nil {
		w.refTarget = refTarget{kind: "LayoutPage", name: p}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		val := node.Content[i+1]
		upper := parentEnd
		if i+2 < len(node.Content) {
			upper = node.Content[i+2].Line
		}
		if w.cursorLine < key.Line || w.cursorLine >= upper {
			continue
		}
		childPath := append(appendCopy(path), key.Value)

		if w.cursorLine == key.Line {
			// On the key's line: decide key-side vs value-side by the colon column.
			if val.Line == key.Line && w.cursorCol >= val.Column {
				// A flow container value (e.g. scenarios: ["ac1", ...]) — descend so
				// the cursor lands on the right item context, not a free value.
				if isContainer(val) {
					return w.descend(val, childPath, upper)
				}
				return w.classifyValue(node, val, key.Value, childPath, path), true
			}
			keyEnd := key.Column + len(key.Value)
			if w.cursorCol <= keyEnd {
				kctx := w.keyContext(node, path)
				kctx.Prefix = key.Value // the key token under the cursor (hover/filtering)
				return kctx, true
			}
			// Between the key token and an empty / next-line value.
			if isContainer(val) {
				return w.descend(val, childPath, upper)
			}
			return w.classifyValue(node, val, key.Value, childPath, path), true
		}

		// Cursor is on a line below the key.
		if isContainer(val) && w.cursorLine >= val.Line {
			return w.descend(val, childPath, upper)
		}
		if isMultilineScalar(val) && w.cursorLine >= val.Line {
			return w.classifyValue(node, val, key.Value, childPath, path), true
		}
		// An empty `params:` parses as a null scalar, so a cursor indented beneath
		// it is the first param key, not a new sibling of the child mapping.
		if key.Value == "params" && w.refTarget.name != "" && w.cursorCol > key.Column {
			return PositionContext{
				Kind:         PosParamKey,
				Path:         joinPath(childPath),
				FieldName:    "params",
				RefKind:      w.refTarget.kind,
				RefName:      w.refTarget.name,
				ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
			}, true
		}
		// The same shape generally: any empty value with the cursor indented
		// beneath its key is the FIRST key inside that (still null) mapping —
		// e.g. a blank line under `spec:` invites spec fields, not root keys.
		if isEmptyScalar(val) && w.cursorCol > key.Column {
			return PositionContext{
				Kind:         PosKey,
				Path:         joinPath(childPath),
				FieldName:    key.Value,
				ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
			}, true
		}
		// A single-line scalar value with the cursor on a blank line beneath it:
		// the author is starting a new sibling key under this mapping.
		return w.keyContext(node, path), true
	}
	// Cursor falls in the mapping's body but on no pair (empty mapping or a blank
	// line before the first key) — a new-key position.
	return w.keyContext(node, path), true
}

func (w *walker) descendSequence(node *yaml.Node, path []string, parentEnd int) (PositionContext, bool) {
	field := lastSegment(path)
	for j, elem := range node.Content {
		upper := parentEnd
		if j+1 < len(node.Content) {
			upper = node.Content[j+1].Line
		}
		if w.cursorLine < elem.Line || w.cursorLine >= upper {
			continue
		}
		childPath := append(appendCopy(path), strconv.Itoa(j))
		// An existing item the cursor sits on.
		if w.cursorLine == elem.Line {
			if isContainer(elem) {
				return w.descend(elem, childPath, upper)
			}
			return w.classifySequenceItem(field, elem, childPath), true
		}
		// Below a scalar item → a new item slot for the same sequence.
		if isContainer(elem) && w.cursorLine >= elem.Line {
			return w.descend(elem, childPath, upper)
		}
		return w.newSequenceItem(field, path, j+1), true
	}
	// Cursor below the last item (or empty sequence) → a new item slot.
	return w.newSequenceItem(field, path, len(node.Content)), true
}

// keyContext builds a PosKey result for the given mapping path, or a
// PosParamKey when the mapping is a component reference's `params:` block.
func (w *walker) keyContext(node *yaml.Node, path []string) PositionContext {
	if lastSegment(path) == "params" && w.refTarget.name != "" {
		return PositionContext{
			Kind:         PosParamKey,
			Path:         joinPath(path),
			FieldName:    "params",
			RefKind:      w.refTarget.kind,
			RefName:      w.refTarget.name,
			PresentKeys:  mappingKeys(node),
			ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
		}
	}
	return PositionContext{
		Kind:         PosKey,
		Path:         joinPath(path),
		FieldName:    lastSegment(path),
		PresentKeys:  mappingKeys(node),
		ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
	}
}

// classifyValue classifies a value node reached via key `field`. parent is the
// enclosing mapping node (used to read sibling keys, e.g. a child's `kind` for
// a `ref`); parentPath is its dotted path.
func (w *walker) classifyValue(parent, val *yaml.Node, field string, valPath, parentPath []string) PositionContext {
	ctx := PositionContext{
		Path:         joinPath(valPath),
		FieldName:    field,
		Prefix:       scalarText(val),
		ReplaceRange: valueRange(val, w.cursorLine, w.cursorCol),
	}

	if field == "kind" && lastSegment(parentPath) != "params" {
		// At the document root this is the manifest kind; nested (e.g. a layout
		// child's `kind:`) the schema position decides the candidate enum. A
		// param literally named "kind" stays a param value.
		ctx.Kind = PosKindValue
		return ctx
	}
	if field == "query" || field == "prql" {
		ctx.Kind = PosQueryScalar
		return ctx
	}
	if lastSegment(parentPath) == "params" && w.refTarget.name != "" {
		ctx.Kind = PosParamValue
		ctx.RefKind = w.refTarget.kind
		ctx.RefName = w.refTarget.name
		return ctx
	}
	if field == "ref" {
		// The target kind is the sibling `kind:` of the same child mapping; the
		// sibling `params:` keys feed the add-required-params quick fix.
		ctx.Kind = PosDatasetRef
		ctx.RefKind = mappingChildValue(parent, "kind")
		ctx.RefName = scalarText(val)
		ctx.PresentKeys = mappingKeys(mappingChild(parent, "params"))
		return ctx
	}
	if rk, ok := refKindFor(field); ok {
		ctx.Kind = PosDatasetRef
		ctx.RefKind = rk
		return ctx
	}
	ctx.Kind = PosFreeValue
	return ctx
}

// classifySequenceItem classifies an existing sequence element under `field`.
func (w *walker) classifySequenceItem(field string, elem *yaml.Node, itemPath []string) PositionContext {
	ctx := PositionContext{
		Path:         joinPath(itemPath),
		FieldName:    field,
		Prefix:       scalarText(elem),
		ReplaceRange: valueRange(elem, w.cursorLine, w.cursorCol),
	}
	switch field {
	case "scenarios":
		ctx.Kind = PosScenarioItem
	case "variances":
		ctx.Kind = PosVarianceItem
	case "dataset", "dependencies":
		ctx.Kind = PosDatasetRef
		ctx.RefKind = datasetRefKind(field, scalarText(elem))
	default:
		ctx.Kind = PosFreeValue
	}
	return ctx
}

// newSequenceItem classifies a fresh (not-yet-typed) sequence slot under
// `field`. path is the sequence node's path (already ending in field); idx is
// the 0-based slot the new item would occupy.
func (w *walker) newSequenceItem(field string, path []string, idx int) PositionContext {
	ctx := PositionContext{
		Path:         joinPath(append(appendCopy(path), strconv.Itoa(idx))),
		FieldName:    field,
		ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
	}
	switch field {
	case "scenarios":
		ctx.Kind = PosScenarioItem
	case "variances":
		ctx.Kind = PosVarianceItem
	case "dataset", "dependencies":
		ctx.Kind = PosDatasetRef
		ctx.RefKind = datasetRefKind(field, "")
	default:
		ctx.Kind = PosFreeValue
	}
	return ctx
}

// refKindFor reports the target kind a simple reference field points at.
// `dataset` resolves by the `$`-prefix convention at use sites; `ref` is
// resolved contextually in classifyValue (sibling `kind`).
func refKindFor(field string) (string, bool) {
	switch field {
	case "dataset":
		return "DataSet", true // refined to DataSource by the $-prefix at use sites
	case "layoutPages":
		return "LayoutPage", true
	}
	if k, ok := refFieldKinds[field]; ok {
		return k, true
	}
	return "", false
}

// datasetRefKind picks DataSource for `$`-prefixed dataset references, else DataSet.
func datasetRefKind(field, value string) string {
	if field == "dependencies" {
		return "DataSource"
	}
	if strings.HasPrefix(value, "$") {
		return "DataSource"
	}
	return "DataSet"
}

func (w *walker) classifyScalar(node *yaml.Node, path []string) PositionContext {
	return PositionContext{
		Kind:         PosFreeValue,
		Path:         joinPath(path),
		FieldName:    lastSegment(path),
		Prefix:       scalarText(node),
		ReplaceRange: valueRange(node, w.cursorLine, w.cursorCol),
	}
}

// --- helpers ---

func isContainer(n *yaml.Node) bool {
	return n != nil && (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode)
}

// isEmptyScalar reports a null / not-yet-typed value node.
func isEmptyScalar(n *yaml.Node) bool {
	return n == nil || (n.Kind == yaml.ScalarNode && n.Value == "")
}

func isMultilineScalar(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode &&
		(n.Style == yaml.LiteralStyle || n.Style == yaml.FoldedStyle)
}

func scalarText(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func valueRange(n *yaml.Node, curLine, curCol int) Range {
	if n == nil || n.Kind != yaml.ScalarNode || n.Value == "" {
		return atCursor(curLine, curCol)
	}
	col := n.Column
	if n.Style == yaml.SingleQuotedStyle || n.Style == yaml.DoubleQuotedStyle {
		col++ // Column points at the opening quote; the range covers the content only
	}
	return Range{StartLine: n.Line, StartCol: col, EndLine: n.Line, EndCol: col + len(n.Value)}
}

func atCursor(line, col int) Range {
	return Range{StartLine: line, StartCol: col, EndLine: line, EndCol: col}
}

// docKind reads the top-level `kind:` scalar from a document's root mapping.
func docKind(root *yaml.Node) string {
	if root == nil || root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "kind" {
			return root.Content[i+1].Value
		}
	}
	return ""
}

// bindingDatasets reads the upstream a mapping binds to from its DIRECT children:
// a component's `dataset` (string or sequence) for scenario/variance completion,
// and a DataSet's `source` + `dependencies` for in-query column completion. It is
// called on each mapping along the descent, so the nearest enclosing binding wins
// — `dataset` lives beside `scenarios` whether at the document root (a top-level
// Table) or inside a layout child's `spec`.
func bindingDatasets(node *yaml.Node) []string {
	out := make([]string, 0, 4)
	out = append(out, scalarOrSeqValues(mappingChild(node, "dataset"))...)
	out = append(out, scalarOrSeqValues(mappingChild(node, "source"))...)
	out = append(out, scalarOrSeqValues(mappingChild(node, "dependencies"))...)
	return out
}

// scalarOrSeqValues collects the non-empty scalar values of a node that is
// either a scalar or a sequence of scalars.
func scalarOrSeqValues(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value != "" {
			return []string{n.Value}
		}
	case yaml.SequenceNode:
		out := make([]string, 0, len(n.Content))
		for _, e := range n.Content {
			if e.Kind == yaml.ScalarNode && e.Value != "" {
				out = append(out, e.Value)
			}
		}
		return out
	default:
	}
	return nil
}

// mappingKeys returns the key names of a mapping node (nil-safe).
func mappingKeys(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]string, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != "" {
			out = append(out, n.Content[i].Value)
		}
	}
	return out
}

func mappingChild(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func appendCopy(path []string) []string {
	out := make([]string, len(path), len(path)+1)
	copy(out, path)
	return out
}

func joinPath(path []string) string {
	if len(path) == 0 {
		return "(root)"
	}
	return strings.Join(path, ".")
}

// kindsKey is the KindsByPath key for a mapping path: "" for the root, else
// the dotted path (no "(root)" sentinel — resolvers index by plain paths).
func kindsKey(path []string) string {
	return strings.Join(path, ".")
}

func lastSegment(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}
