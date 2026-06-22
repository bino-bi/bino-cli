package spec

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EditYAMLDocument applies dotted-path edits to a single document (1-based
// position) within multi-document YAML content, preserving the comments, key
// order, and formatting of every other document and of the unedited keys in the
// target document. Each patch key is a dotted path (e.g. "spec.title" or
// "spec.columns[0]"); missing intermediate mappings are created. It returns the
// rewritten full content and the edited document on its own (for validation).
//
// This is the fidelity-preserving counterpart to ParseYAMLNodes. yaml.v3
// round-trips Head/Line/Foot comments and preserves mapping key order, so only
// the values the caller changes are rewritten.
func EditYAMLDocument(content string, position int, patch map[string]any) (full string, edited string, err error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var docs []*yaml.Node // DocumentNodes
	for {
		var n yaml.Node
		decErr := decoder.Decode(&n)
		if decErr != nil {
			if decErr.Error() == "EOF" {
				break
			}
			return "", "", fmt.Errorf("parse yaml: %w", decErr)
		}
		nodeCopy := n
		docs = append(docs, &nodeCopy)
	}

	if position < 1 || position > len(docs) {
		return "", "", fmt.Errorf("document position %d out of range (file has %d document(s))", position, len(docs))
	}

	target := docs[position-1]
	if target.Kind != yaml.DocumentNode || len(target.Content) == 0 {
		return "", "", fmt.Errorf("document %d is empty", position)
	}
	root := target.Content[0]
	if root.Kind != yaml.MappingNode {
		return "", "", fmt.Errorf("document %d is not a mapping", position)
	}

	// Apply edits in sorted path order for determinism.
	paths := make([]string, 0, len(patch))
	for p := range patch {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := setNodePath(root, p, patch[p]); err != nil {
			return "", "", fmt.Errorf("set %s: %w", p, err)
		}
	}

	full, err = encodeDocuments(docs)
	if err != nil {
		return "", "", err
	}
	edited, err = encodeDocuments(docs[position-1 : position])
	if err != nil {
		return "", "", err
	}
	return full, edited, nil
}

// RemoveYAMLPaths deletes one or more dotted paths from a single document
// (1-based position) within multi-document YAML content, preserving the
// comments, key order, and formatting of every other document and of the
// untouched keys in the target document. Each path is a dotted path; a trailing
// [index] removes a sequence element (e.g. "spec.columns[1]"), otherwise the
// path's mapping key (and its value) is removed (e.g. "spec.title"). It returns
// the rewritten full content and the edited document on its own (for validation).
//
// This is the deletion counterpart to EditYAMLDocument: the set-only patch map
// has no "absence" sentinel, so removal is a sibling operation. When several
// paths address elements of the same sequence, the higher index is removed
// first so a leading splice never invalidates a later index.
func RemoveYAMLPaths(content string, position int, paths []string) (full string, edited string, err error) {
	docs, err := decodeDocuments(content)
	if err != nil {
		return "", "", err
	}
	root, err := documentRoot(docs, position)
	if err != nil {
		return "", "", err
	}

	for _, p := range orderRemovals(paths) {
		if err := removeNodePath(root, p); err != nil {
			return "", "", fmt.Errorf("remove %s: %w", p, err)
		}
	}

	full, err = encodeDocuments(docs)
	if err != nil {
		return "", "", err
	}
	edited, err = encodeDocuments(docs[position-1 : position])
	if err != nil {
		return "", "", err
	}
	return full, edited, nil
}

// orderRemovals returns paths sorted so that elements of the same sequence are
// removed highest-index first (a leading splice would otherwise shift a later
// index), and is otherwise stable/deterministic. Paths that share the segment
// prefix before a trailing [index] are compared by descending numeric index.
func orderRemovals(paths []string) []string {
	ordered := append([]string(nil), paths...)
	sort.SliceStable(ordered, func(i, j int) bool {
		bi, ii, oki := splitTrailingIndex(ordered[i])
		bj, ij, okj := splitTrailingIndex(ordered[j])
		if oki && okj && bi == bj {
			return ii > ij // same sequence: higher index first
		}
		return ordered[i] > ordered[j] // deterministic; deeper/later first
	})
	return ordered
}

// splitTrailingIndex splits a path into its part before a trailing [index] and
// that index, e.g. "spec.columns[2]" -> ("spec.columns", 2, true).
func splitTrailingIndex(path string) (base string, idx int, ok bool) {
	open := strings.LastIndexByte(path, '[')
	if open < 0 || !strings.HasSuffix(path, "]") {
		return path, 0, false
	}
	n, err := strconv.Atoi(path[open+1 : len(path)-1])
	if err != nil {
		return path, 0, false
	}
	return path[:open], n, true
}

// ReorderYAMLSequence moves the element at index from to index to within the
// sequence at path in a single document (1-based position), preserving comments
// and the order of every other document and key. The moved element carries its
// own Head/Line/Foot comments. from and to are 0-based indices into the original
// sequence; the element ends up at index to. A no-op (from == to) re-encodes to
// byte-identical content. It returns the rewritten full content and the edited
// document on its own (for validation).
func ReorderYAMLSequence(content string, position int, path string, from, to int) (full string, edited string, err error) {
	docs, err := decodeDocuments(content)
	if err != nil {
		return "", "", err
	}
	root, err := documentRoot(docs, position)
	if err != nil {
		return "", "", err
	}

	seq, err := descendPath(root, path)
	if err != nil {
		return "", "", fmt.Errorf("reorder %s: %w", path, err)
	}
	if seq.Kind != yaml.SequenceNode {
		return "", "", fmt.Errorf("reorder %s: not a sequence", path)
	}
	n := len(seq.Content)
	if from < 0 || from >= n {
		return "", "", fmt.Errorf("reorder %s: from index %d out of range (len %d)", path, from, n)
	}
	if to < 0 || to >= n {
		return "", "", fmt.Errorf("reorder %s: to index %d out of range (len %d)", path, to, n)
	}

	elem := seq.Content[from]
	rest := make([]*yaml.Node, 0, n-1)
	rest = append(rest, seq.Content[:from]...)
	rest = append(rest, seq.Content[from+1:]...)
	moved := make([]*yaml.Node, 0, n)
	moved = append(moved, rest[:to]...)
	moved = append(moved, elem)
	moved = append(moved, rest[to:]...)
	seq.Content = moved

	full, err = encodeDocuments(docs)
	if err != nil {
		return "", "", err
	}
	edited, err = encodeDocuments(docs[position-1 : position])
	if err != nil {
		return "", "", err
	}
	return full, edited, nil
}

// AppendYAMLSequence appends value to the end of the sequence at the dotted path
// in a single document (1-based position), preserving the comments, key order,
// and formatting of every other document and of the untouched keys. Missing
// intermediate mappings — and the sequence itself — are created, so appending to
// an absent array yields a one-element sequence. It returns the rewritten full
// content and the edited document on its own (for validation).
//
// This is the growth counterpart to EditYAMLDocument, whose set-only paths reject
// an out-of-range index (so they cannot append past a sequence's end). It is the
// single engine for the designer's array-append mutations.
func AppendYAMLSequence(content string, position int, path string, value any) (full string, edited string, err error) {
	docs, err := decodeDocuments(content)
	if err != nil {
		return "", "", err
	}
	root, err := documentRoot(docs, position)
	if err != nil {
		return "", "", err
	}

	seq, err := descendOrCreateSequence(root, path)
	if err != nil {
		return "", "", fmt.Errorf("append %s: %w", path, err)
	}
	elem, err := encodeValueNode(value)
	if err != nil {
		return "", "", fmt.Errorf("append %s: %w", path, err)
	}
	seq.Content = append(seq.Content, elem)

	full, err = encodeDocuments(docs)
	if err != nil {
		return "", "", err
	}
	edited, err = encodeDocuments(docs[position-1 : position])
	if err != nil {
		return "", "", err
	}
	return full, edited, nil
}

// descendOrCreateSequence walks a dotted path from a mapping root to a sequence
// node, creating missing intermediate mappings and the terminal sequence as
// needed (mirroring setNodePath's auto-vivification). [index] suffixes select an
// existing sequence element to descend into. It errors if a non-final segment
// resolves to a non-mapping, or if the terminal node exists but is not a sequence.
func descendOrCreateSequence(root *yaml.Node, path string) (*yaml.Node, error) {
	parts := strings.Split(path, ".")
	current := root
	for i, part := range parts {
		key, idx, hasIdx := parseSegment(part)
		last := i == len(parts)-1

		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("cannot descend into %q: parent is not a mapping", key)
		}
		valNode := mapValue(current, key)

		if valNode == nil {
			if hasIdx {
				return nil, fmt.Errorf("index %d out of range for %q (len 0)", idx, key)
			}
			valNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			setMapValue(current, key, valNode)
		}

		if hasIdx {
			if valNode.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%q is not a sequence", key)
			}
			if idx < 0 || idx >= len(valNode.Content) {
				return nil, fmt.Errorf("index %d out of range for %q (len %d)", idx, key, len(valNode.Content))
			}
			current = valNode.Content[idx]
			continue
		}

		if last {
			if valNode.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%q is not a sequence", key)
			}
			return valNode, nil
		}
		current = valNode
	}
	return nil, fmt.Errorf("empty path")
}

// decodeDocuments parses multi-document YAML content into DocumentNodes,
// preserving comments and order for fidelity-preserving re-encoding.
func decodeDocuments(content string) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		decErr := decoder.Decode(&n)
		if decErr != nil {
			if decErr.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("parse yaml: %w", decErr)
		}
		nodeCopy := n
		docs = append(docs, &nodeCopy)
	}
	return docs, nil
}

// documentRoot returns the mapping node at the root of the 1-based document
// position, with the same range/shape guards as EditYAMLDocument.
func documentRoot(docs []*yaml.Node, position int) (*yaml.Node, error) {
	if position < 1 || position > len(docs) {
		return nil, fmt.Errorf("document position %d out of range (file has %d document(s))", position, len(docs))
	}
	target := docs[position-1]
	if target.Kind != yaml.DocumentNode || len(target.Content) == 0 {
		return nil, fmt.Errorf("document %d is empty", position)
	}
	root := target.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("document %d is not a mapping", position)
	}
	return root, nil
}

// descendPath walks a dotted path from a mapping root to the addressed node,
// honoring [index] suffixes on segments. It does not create missing nodes.
func descendPath(root *yaml.Node, path string) (*yaml.Node, error) {
	current := root
	for _, part := range strings.Split(path, ".") {
		key, idx, hasIdx := parseSegment(part)
		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("cannot descend into %q: parent is not a mapping", key)
		}
		valNode := mapValue(current, key)
		if valNode == nil {
			return nil, fmt.Errorf("key %q not found", key)
		}
		if hasIdx {
			if valNode.Kind != yaml.SequenceNode {
				return nil, fmt.Errorf("%q is not a sequence", key)
			}
			if idx < 0 || idx >= len(valNode.Content) {
				return nil, fmt.Errorf("index %d out of range for %q (len %d)", idx, key, len(valNode.Content))
			}
			current = valNode.Content[idx]
			continue
		}
		current = valNode
	}
	return current, nil
}

// removeNodePath deletes the node addressed by a dotted path. A trailing [index]
// removes a sequence element; otherwise the final mapping key (and its value) is
// removed. Orphaned block comments are hoisted onto an adjacent sibling so they
// are not swallowed by the splice.
func removeNodePath(root *yaml.Node, path string) error {
	parts := strings.Split(path, ".")
	last := parts[len(parts)-1]
	parent := root
	if len(parts) > 1 {
		p, err := descendPath(root, strings.Join(parts[:len(parts)-1], "."))
		if err != nil {
			return err
		}
		parent = p
	}

	key, idx, hasIdx := parseSegment(last)
	if hasIdx {
		seq := mapValue(parent, key)
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return fmt.Errorf("%q is not a sequence", key)
		}
		if idx < 0 || idx >= len(seq.Content) {
			return fmt.Errorf("index %d out of range for %q (len %d)", idx, key, len(seq.Content))
		}
		removeSequenceElement(seq, idx)
		return nil
	}

	if parent.Kind != yaml.MappingNode {
		return fmt.Errorf("cannot remove %q: parent is not a mapping", key)
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			removeMappingPair(parent, i)
			return nil
		}
	}
	return fmt.Errorf("key %q not found", key)
}

// removeMappingPair drops the (key, value) couple at content index i, keeping the
// alternating key/value invariant and hoisting the removed key's leading
// (HeadComment) or trailing (FootComment) block comment onto a surviving sibling.
func removeMappingPair(m *yaml.Node, i int) {
	keyNode := m.Content[i]
	valNode := m.Content[i+1]
	switch {
	case i+2 < len(m.Content):
		next := m.Content[i+2]
		next.HeadComment = joinComment(keyNode.HeadComment, next.HeadComment)
	case i-2 >= 0:
		prior := m.Content[i-2]
		foot := keyNode.FootComment
		if foot == "" {
			foot = valNode.FootComment
		}
		prior.FootComment = joinComment(prior.FootComment, foot)
	}
	m.Content = append(m.Content[:i], m.Content[i+2:]...)
}

// removeSequenceElement drops the element at idx, hoisting its leading or
// trailing block comment onto a surviving neighbor.
func removeSequenceElement(s *yaml.Node, idx int) {
	elem := s.Content[idx]
	switch {
	case idx+1 < len(s.Content):
		next := s.Content[idx+1]
		next.HeadComment = joinComment(elem.HeadComment, next.HeadComment)
	case idx > 0:
		prior := s.Content[idx-1]
		prior.FootComment = joinComment(prior.FootComment, elem.FootComment)
	}
	s.Content = append(s.Content[:idx], s.Content[idx+1:]...)
}

// joinComment concatenates two comment blocks, dropping empties, so a hoisted
// comment is prepended to (or appended after) the sibling's own comment.
func joinComment(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + "\n" + b
	}
}

// encodeDocuments re-encodes a slice of DocumentNodes back to YAML text.
func encodeDocuments(docs []*yaml.Node) (string, error) {
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			_ = enc.Close()
			return "", fmt.Errorf("encode yaml: %w", err)
		}
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}
	return buf.String(), nil
}

// encodeValueNode turns a caller-supplied value into the yaml.Node to splice
// into the tree. A *yaml.Node is used as-is so its key order and scalar styles
// survive verbatim (DecodeJSONValue produces such order-preserving nodes for the
// JSON edit boundary); any other Go value is normalized and encoded, which —
// for a map[string]any — alphabetizes keys, so callers that must preserve object
// key order pass a *yaml.Node, not a Go map.
func encodeValueNode(value any) (*yaml.Node, error) {
	if n, ok := value.(*yaml.Node); ok {
		return n, nil
	}
	newNode := &yaml.Node{}
	if err := newNode.Encode(normalizeJSONNumbers(value)); err != nil {
		return nil, err
	}
	return newNode, nil
}

// setNodePath sets the value at a dotted path within a mapping node, creating
// intermediate mappings as needed and preserving sibling order and comments.
// A segment may carry a [index] suffix to address a sequence element.
func setNodePath(root *yaml.Node, path string, value any) error {
	parts := strings.Split(path, ".")
	current := root

	for i, part := range parts {
		key, idx, hasIdx := parseSegment(part)
		last := i == len(parts)-1

		if current.Kind != yaml.MappingNode {
			return fmt.Errorf("cannot descend into %q: parent is not a mapping", key)
		}

		valNode := mapValue(current, key)

		if last && !hasIdx {
			newNode, err := encodeValueNode(value)
			if err != nil {
				return err
			}
			setMapValue(current, key, newNode)
			return nil
		}

		if valNode == nil {
			if hasIdx {
				valNode = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			} else {
				valNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			setMapValue(current, key, valNode)
		}

		if hasIdx {
			if valNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("%q is not a sequence", key)
			}
			if idx < 0 || idx >= len(valNode.Content) {
				return fmt.Errorf("index %d out of range for %q (len %d)", idx, key, len(valNode.Content))
			}
			if last {
				newNode, err := encodeValueNode(value)
				if err != nil {
					return err
				}
				valNode.Content[idx] = newNode
				return nil
			}
			current = valNode.Content[idx]
			continue
		}

		current = valNode
	}
	return nil
}

// mapValue returns the value node for key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapValue replaces the value node for key, or appends a new key/value pair
// (preserving existing order).
func setMapValue(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m.Content = append(m.Content, keyNode, val)
}

// parseSegment splits a path segment into its key and an optional [index].
func parseSegment(seg string) (key string, idx int, hasIdx bool) {
	open := strings.IndexByte(seg, '[')
	if open < 0 || !strings.HasSuffix(seg, "]") {
		return seg, 0, false
	}
	n, err := strconv.Atoi(seg[open+1 : len(seg)-1])
	if err != nil {
		return seg, 0, false
	}
	return seg[:open], n, true
}

// normalizeJSONNumbers converts integral float64 values (produced by JSON
// decoding) to int64 so they re-encode as integers, not "5" vs "5.0", matching
// integer-typed schema fields.
func normalizeJSONNumbers(v any) any {
	switch t := v.(type) {
	case float64:
		if !math.IsInf(t, 0) && !math.IsNaN(t) && t == math.Trunc(t) {
			return int64(t)
		}
		return t
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeJSONNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeJSONNumbers(val)
		}
		return t
	default:
		return v
	}
}

// DecodeJSONValue parses a JSON value into a *yaml.Node that preserves object key
// order (Go's map[string]any does not) and re-encodes in natural block YAML.
// JSON is a subset of YAML, so yaml.v3 decodes it into a node tree whose mapping
// content keeps source order; the tree is then naturalized (flow style cleared,
// JSON's blanket double-quoting reset to YAML's canonical per-scalar style,
// integral floats retagged as ints) so a value read from disk and written back
// unchanged round-trips byte-for-byte. The returned node is meant to be passed
// straight into EditYAMLDocument's patch values or AppendYAMLSequence's value,
// where encodeValueNode splices it verbatim. This is the order-preserving JSON
// edit boundary for the designer's array-of-object widgets (edges, attributes,
// thereof/partof/columnthereof) whose intra-object key order is meaningful.
func DecodeJSONValue(raw []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode json value: %w", err)
	}
	var val *yaml.Node
	switch {
	case doc.Kind == yaml.DocumentNode && len(doc.Content) == 1:
		val = doc.Content[0]
	case doc.Kind == yaml.DocumentNode:
		// Empty input (e.g. JSON null with no content) -> an explicit null node.
		val = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	default:
		val = &doc
	}
	naturalizeNode(val)
	return val, nil
}

// naturalizeNode strips the artifacts of yaml.v3's JSON decode so the node
// re-encodes as idiomatic block YAML: flow-style collections become block, and
// scalars (which JSON marks double-quoted) revert to style 0 so the encoder
// picks the canonical style. Integral !!float scalars are retagged !!int to
// match normalizeJSONNumbers, so "5" not "5.0".
func naturalizeNode(n *yaml.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		n.Style = 0
	case yaml.ScalarNode:
		n.Style = 0
		if n.Tag == "!!float" {
			var f float64
			if err := n.Decode(&f); err == nil && !math.IsInf(f, 0) && !math.IsNaN(f) && f == math.Trunc(f) {
				n.Tag = "!!int"
				n.Value = strconv.FormatInt(int64(f), 10)
			}
		}
	default:
		// DocumentNode/AliasNode never occur in a JSON-decoded value tree (the
		// document wrapper is unwrapped in DecodeJSONValue); leave them untouched.
	}
	for _, c := range n.Content {
		naturalizeNode(c)
	}
}

// ParseYAMLNodes parses multi-document YAML content into a slice of root nodes.
// Each element corresponds to one YAML document separated by "---".
func ParseYAMLNodes(content string) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(content))
	var nodes []*yaml.Node

	for {
		var doc yaml.Node
		err := decoder.Decode(&doc)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nodes, err
		}
		// The top-level node is a document node; use its content.
		if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
			nodes = append(nodes, doc.Content[0])
		}
	}

	return nodes, nil
}

// ResolvePathPosition walks a yaml.Node tree to resolve a dotted field path
// (e.g., "spec.children") to its YAML source line and column.
// Returns (line, col, true) on success, or (0, 0, false) if the path is not found.
// Line and column are 1-based.
//
// When a path is only partially resolved (e.g., "spec.children" where "children" doesn't
// exist under "spec"), the position of the last matched key is returned, so the user
// sees the parent where the missing field should be added.
func ResolvePathPosition(node *yaml.Node, path string) (line, col int, ok bool) {
	if node == nil || path == "" || path == "(root)" {
		if node != nil {
			return node.Line, node.Column, true
		}
		return 0, 0, false
	}

	parts := strings.Split(path, ".")
	current := node
	// Track the key node of the last successfully matched segment
	lastKeyLine, lastKeyCol := node.Line, node.Column

	for _, part := range parts {
		found := false

		switch current.Kind {
		case yaml.MappingNode:
			// Mapping nodes have alternating key/value pairs in Content
			for i := 0; i+1 < len(current.Content); i += 2 {
				keyNode := current.Content[i]
				valueNode := current.Content[i+1]
				if keyNode.Value == part {
					lastKeyLine, lastKeyCol = keyNode.Line, keyNode.Column
					current = valueNode
					found = true
					break
				}
			}

		case yaml.SequenceNode:
			// Try to parse as array index
			var idx int
			if _, err := fmt.Sscanf(part, "%d", &idx); err == nil && idx >= 0 && idx < len(current.Content) {
				current = current.Content[idx]
				lastKeyLine, lastKeyCol = current.Line, current.Column
				found = true
			}

		default:
		}

		if !found {
			// Path segment not found — return the position of the last matched key.
			// This gives the user the location of the parent where the field should exist.
			return lastKeyLine, lastKeyCol, true
		}
	}

	return current.Line, current.Column, true
}

// ExtractSourceSnippet extracts lines around the given 1-based line number
// from source content for display. contextLines controls how many lines
// are shown before and after the target line.
// Returns a formatted snippet with line numbers.
func ExtractSourceSnippet(source string, line, contextLines int) string {
	if source == "" || line <= 0 {
		return ""
	}

	lines := strings.Split(source, "\n")
	if line > len(lines) {
		return ""
	}

	start := line - contextLines
	if start < 1 {
		start = 1
	}
	end := line + contextLines
	if end > len(lines) {
		end = len(lines)
	}

	// Determine the width of line numbers for alignment
	width := len(fmt.Sprintf("%d", end))

	var b strings.Builder
	for i := start; i <= end; i++ {
		prefix := fmt.Sprintf("%*d", width, i)
		fmt.Fprintf(&b, "    %s │ %s\n", prefix, lines[i-1])
	}

	return b.String()
}
