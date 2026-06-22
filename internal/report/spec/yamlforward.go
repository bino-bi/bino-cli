package spec

import (
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
)

// PositionContext is the resolver's verdict for a cursor: the dotted path, the
// enclosing manifest kind, and enough context to assemble candidates.
type PositionContext struct {
	Kind          PositionKind
	Path          string // dotted path (same vocabulary as ResolvePathPosition)
	EnclosingKind string // the document's `kind:` value
	FieldName     string // the immediate field name under the cursor
	RefKind       string // for PosDatasetRef: the valid target kind
	Prefix        string // already-typed value text (for filtering / replace)
	ReplaceRange  Range  // where an accepted completion should be written
	DocIndex      int    // 0-based document ordinal within a multi-doc file
	BoundDatasets []string
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
}

// ResolvePositionPath maps a 1-based cursor (line, col) within raw multi-document
// YAML to a PositionContext. It parses the RAW buffer — never env-expanded text —
// so positions stay honest (see loader's ${VAR} expansion). It returns ok=false
// when the cursor is outside any document body.
func ResolvePositionPath(content string, line, col int) (PositionContext, bool) {
	nodes, err := ParseYAMLNodes(content)
	if err != nil || len(nodes) == 0 {
		return PositionContext{}, false
	}

	docIdx, root, endLine := selectDocument(nodes, line)
	if root == nil || root.Kind != yaml.MappingNode {
		return PositionContext{}, false
	}

	w := &walker{cursorLine: line, cursorCol: col, enclosingKind: docKind(root), docIndex: docIdx}
	ctx, ok := w.descend(root, nil, endLine)
	if !ok {
		return PositionContext{}, false
	}
	ctx.EnclosingKind = w.enclosingKind
	ctx.DocIndex = docIdx
	switch ctx.Kind {
	case PosScenarioItem, PosVarianceItem, PosQueryScalar:
		ctx.BoundDatasets = w.boundDatasets
	default:
	}
	return ctx, true
}

// selectDocument picks the document whose content owns the cursor line and
// returns its 0-based index, root mapping node, and the exclusive upper line
// bound (the next document's start, or a sentinel past EOF).
func selectDocument(nodes []*yaml.Node, line int) (idx int, root *yaml.Node, endLine int) {
	const eof = 1 << 30
	chosen := -1
	for i, n := range nodes {
		if n == nil {
			continue
		}
		if n.Line <= line {
			chosen = i
		}
	}
	if chosen < 0 {
		chosen = 0 // cursor sits above the first document's content
	}
	root = nodes[chosen]
	endLine = eof
	for i := chosen + 1; i < len(nodes); i++ {
		if nodes[i] != nil {
			endLine = nodes[i].Line
			break
		}
	}
	return chosen, root, endLine
}

// walker carries the per-resolution cursor and document context through the
// recursive descent.
type walker struct {
	cursorLine    int
	cursorCol     int
	enclosingKind string
	docIndex      int
	boundDatasets []string
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
				return w.classifyValue(val, key.Value, childPath, path), true
			}
			keyEnd := key.Column + len(key.Value)
			if w.cursorCol <= keyEnd {
				return w.keyContext(node, path), true
			}
			// Between the key token and an empty / next-line value.
			if isContainer(val) {
				return w.descend(val, childPath, upper)
			}
			return w.classifyValue(val, key.Value, childPath, path), true
		}

		// Cursor is on a line below the key.
		if isContainer(val) && w.cursorLine >= val.Line {
			return w.descend(val, childPath, upper)
		}
		if isMultilineScalar(val) && w.cursorLine >= val.Line {
			return w.classifyValue(val, key.Value, childPath, path), true
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
		return w.newSequenceItem(field, path), true
	}
	// Cursor below the last item (or empty sequence) → a new item slot.
	return w.newSequenceItem(field, path), true
}

// keyContext builds a PosKey result for the given mapping path.
func (w *walker) keyContext(_ *yaml.Node, path []string) PositionContext {
	return PositionContext{
		Kind:         PosKey,
		Path:         joinPath(path),
		FieldName:    lastSegment(path),
		ReplaceRange: atCursor(w.cursorLine, w.cursorCol),
	}
}

// classifyValue classifies a value node reached via key `field`. parentPath is
// the path of the enclosing mapping (used to read sibling keys, e.g. a child's
// `kind` for a `ref`).
func (w *walker) classifyValue(val *yaml.Node, field string, valPath, parentPath []string) PositionContext {
	ctx := PositionContext{
		Path:         joinPath(valPath),
		FieldName:    field,
		Prefix:       scalarText(val),
		ReplaceRange: valueRange(val, w.cursorLine, w.cursorCol),
	}

	if field == "kind" && len(parentPath) == 0 {
		ctx.Kind = PosKindValue
		return ctx
	}
	if field == "query" || field == "prql" {
		ctx.Kind = PosQueryScalar
		return ctx
	}
	if rk, ok := w.refKindFor(field, parentPath); ok {
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

// newSequenceItem classifies a fresh (not-yet-typed) sequence slot under `field`.
func (w *walker) newSequenceItem(field string, parentPath []string) PositionContext {
	ctx := PositionContext{
		Path:         joinPath(parentPath) + "." + field,
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

// refKindFor reports the target kind a reference field points at. `ref` resolves
// to the sibling `kind` value of its child object; `dataset` resolves by the
// `$`-prefix convention; the rest come from refFieldKinds.
func (w *walker) refKindFor(field string, parentPath []string) (string, bool) {
	switch field {
	case "dataset":
		return "DataSet", true // refined to DataSource by the $-prefix at use sites
	case "ref":
		return "", true // target kind is the sibling `kind`; left for the caller/index
	case "layoutPages":
		return "LayoutPage", true
	}
	if k, ok := refFieldKinds[field]; ok {
		return k, true
	}
	_ = parentPath
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
	return Range{StartLine: n.Line, StartCol: n.Column, EndLine: n.Line, EndCol: n.Column + len(n.Value)}
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

func lastSegment(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}
