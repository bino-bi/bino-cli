package spec

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

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
		b.WriteString(fmt.Sprintf("    %s │ %s\n", prefix, lines[i-1]))
	}

	return b.String()
}
