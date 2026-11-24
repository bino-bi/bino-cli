package render

import (
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// sourceLines finds the YAML line of a component for the preview's
// click-to-source. It parses each manifest file once per render.
type sourceLines struct {
	nodes map[string][]*yaml.Node
}

func newSourceLines() *sourceLines {
	return &sourceLines{nodes: map[string][]*yaml.Node{}}
}

// docNode returns the root node of the 1-based position-th document in file.
func (s *sourceLines) docNode(file string, position int) *yaml.Node {
	if file == "" || position < 1 {
		return nil
	}
	nodes, ok := s.nodes[file]
	if !ok {
		content, err := os.ReadFile(file)
		if err == nil {
			// A parse error keeps the documents before it.
			nodes, _ = spec.ParseYAMLNodes(string(content)) //nolint:errcheck // partial result is enough
		}
		s.nodes[file] = nodes
	}
	if position > len(nodes) {
		return nil
	}
	return nodes[position-1]
}

// docLine returns the 1-based start line of a document, 0 when unknown.
func (s *sourceLines) docLine(doc *config.Document) int {
	if n := s.docNode(doc.File, doc.Position); n != nil {
		return n.Line
	}
	return 0
}

// childLine returns the 1-based line of the inline child with kind and name
// inside doc, or the document start when the child is not found.
func (s *sourceLines) childLine(doc *config.Document, kind, name string) int {
	root := s.docNode(doc.File, doc.Position)
	if root == nil {
		return 0
	}
	if n := findInlineChild(mappingValue(root, "spec"), kind, name); n != nil {
		return n.Line
	}
	return root.Line
}

// findInlineChild finds the mapping with the given kind and metadata.name.
func findInlineChild(n *yaml.Node, kind, name string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		if k := mappingValue(n, "kind"); k != nil && k.Value == kind {
			if nm := mappingValue(mappingValue(n, "metadata"), "name"); nm != nil && nm.Value == name {
				return n
			}
		}
	}
	for _, c := range n.Content {
		if found := findInlineChild(c, kind, name); found != nil {
			return found
		}
	}
	return nil
}

func mappingValue(n *yaml.Node, key string) *yaml.Node {
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

// withSourceDoc returns a render context whose inline children are looked up
// in doc. Returns rc unchanged outside the preview.
func (rc *renderCtx) withSourceDoc(doc config.Document) *renderCtx {
	if rc.srcLines == nil {
		return rc
	}
	c := *rc
	c.srcDoc = &doc
	return &c
}

// withRefSourceDoc switches the source document to the one a ref child names,
// so the children of a referenced container resolve in its own file.
func (rc *renderCtx) withRefSourceDoc(child layoutChild) *renderCtx {
	if rc.srcLines == nil || child.Ref == "" {
		return rc
	}
	doc, ok := rc.globalIndex[child.Kind+":"+child.Ref]
	if !ok {
		return rc
	}
	return rc.withSourceDoc(doc)
}

// sourceLocation returns the file and 1-based line a layout child is
// defined at, or "" and 0 when unknown (always outside the preview).
func (rc *renderCtx) sourceLocation(child layoutChild) (file string, line int) {
	if rc.srcLines == nil {
		return "", 0
	}
	if child.Ref != "" {
		doc, ok := rc.globalIndex[child.Kind+":"+child.Ref]
		if !ok {
			return "", 0
		}
		if line = rc.srcLines.docLine(&doc); line > 0 {
			return doc.File, line
		}
		return "", 0
	}
	if rc.srcDoc == nil || child.Metadata.Name == "" {
		return "", 0
	}
	if line = rc.srcLines.childLine(rc.srcDoc, child.Kind, child.Metadata.Name); line > 0 {
		return rc.srcDoc.File, line
	}
	return "", 0
}

// writeSourceLocationAttrs writes data-bino-source-file and
// data-bino-source-line for click-to-source in the preview.
func (rc *renderCtx) writeSourceLocationAttrs(b *strings.Builder, child layoutChild) {
	file, line := rc.sourceLocation(child)
	if line == 0 {
		return
	}
	writeAttr(b, "data-bino-source-file", file)
	writeAttr(b, "data-bino-source-line", strconv.Itoa(line))
}
