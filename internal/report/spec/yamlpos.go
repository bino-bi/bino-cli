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
		if err := setNodePath(root, p, normalizeJSONNumbers(patch[p])); err != nil {
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
			newNode := &yaml.Node{}
			if err := newNode.Encode(value); err != nil {
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
				newNode := &yaml.Node{}
				if err := newNode.Encode(value); err != nil {
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
