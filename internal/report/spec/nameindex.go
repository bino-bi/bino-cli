package spec

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// SymbolDef is a manifest's identity: where its metadata.name is declared.
type SymbolDef struct {
	Kind      string
	Name      string
	File      string
	NameRange Range // the metadata.name value span
	DocRange  Range // the whole document span
	DocIndex  int   // 0-based ordinal within the file
}

// SymbolRef is one site that references a manifest by name.
type SymbolRef struct {
	File       string
	Range      Range // the reference value span (includes a leading $ when present)
	TargetKind string
	TargetName string
	Field      string // the field that carried the reference (dataset/source/ref/...)
	DocIndex   int
	Dollar     bool // the value carried a $ prefix (DataSource shorthand)
}

// NameIndex is the project-wide name→location map that powers definition,
// references, rename, and workspace-symbol. It is built from raw buffers (so it
// also covers unsaved edits) and carries per-name source ranges that
// config.Document deliberately omits.
type NameIndex struct {
	defs   map[string]SymbolDef   // key: Kind + "\x00" + Name
	byName map[string][]SymbolDef // key: Name
	refs   []SymbolRef
}

// BuildNameIndex parses every file (path → raw content) and records each
// document's definition and reference sites. Parse failures skip the offending
// file rather than failing the whole index.
func BuildNameIndex(files map[string]string) (*NameIndex, error) {
	idx := &NameIndex{
		defs:   make(map[string]SymbolDef),
		byName: make(map[string][]SymbolDef),
	}
	for file, content := range files {
		nodes, err := ParseYAMLNodes(content)
		if err != nil {
			continue
		}
		for docIdx, root := range nodes {
			if root == nil || root.Kind != yaml.MappingNode {
				continue
			}
			idx.indexDocument(file, docIdx, root, docEndLine(nodes, docIdx))
		}
	}
	return idx, nil
}

func (idx *NameIndex) indexDocument(file string, docIdx int, root *yaml.Node, endLine int) {
	kind := docKind(root)

	if meta := mappingChild(root, "metadata"); meta != nil {
		if nameNode := mappingChild(meta, "name"); nameNode != nil && nameNode.Value != "" {
			def := SymbolDef{
				Kind:      kind,
				Name:      nameNode.Value,
				File:      file,
				NameRange: nodeRange(nameNode),
				DocRange:  Range{StartLine: root.Line, StartCol: root.Column, EndLine: endLine, EndCol: 1},
				DocIndex:  docIdx,
			}
			idx.defs[kind+"\x00"+nameNode.Value] = def
			idx.byName[nameNode.Value] = append(idx.byName[nameNode.Value], def)
		}
	}

	if spec := mappingChild(root, "spec"); spec != nil {
		idx.walkRefs(file, docIdx, spec)
	}
}

// walkRefs recursively records reference sites anywhere in a spec subtree, so it
// uniformly covers top-level fields, layout children, tree nodes, and grid
// children without special-casing each container.
func (idx *NameIndex) walkRefs(file string, docIdx int, node *yaml.Node) {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			val := node.Content[i+1]
			switch key {
			case "dataset", "dependencies":
				idx.recordScalarOrSeq(file, docIdx, key, val, "")
			case "source", "secret", "page", "signingProfile":
				idx.recordScalarOrSeq(file, docIdx, key, val, refFieldKinds[key])
			case "layoutPages":
				idx.recordLayoutPages(file, docIdx, val)
			case "ref":
				if val.Kind == yaml.ScalarNode && val.Value != "" {
					idx.refs = append(idx.refs, SymbolRef{
						File: file, DocIndex: docIdx, Range: nodeRange(val),
						TargetKind: mappingChildValue(node, "kind"), TargetName: val.Value, Field: "ref",
					})
				}
			}
			idx.walkRefs(file, docIdx, val)
		}
	case yaml.SequenceNode:
		for _, e := range node.Content {
			idx.walkRefs(file, docIdx, e)
		}
	default:
	}
}

func (idx *NameIndex) recordScalarOrSeq(file string, docIdx int, field string, val *yaml.Node, kind string) {
	add := func(n *yaml.Node) {
		if n.Kind != yaml.ScalarNode || n.Value == "" {
			return
		}
		k := kind
		if field == "dataset" || field == "dependencies" {
			k = datasetRefKind(field, n.Value)
		}
		idx.refs = append(idx.refs, SymbolRef{
			File: file, DocIndex: docIdx, Range: nodeRange(n),
			TargetKind: k, TargetName: strings.TrimPrefix(n.Value, "$"), Field: field,
			Dollar: strings.HasPrefix(n.Value, "$"),
		})
	}
	switch val.Kind {
	case yaml.ScalarNode:
		add(val)
	case yaml.SequenceNode:
		for _, e := range val.Content {
			add(e)
		}
	default:
	}
}

func (idx *NameIndex) recordLayoutPages(file string, docIdx int, val *yaml.Node) {
	if val.Kind != yaml.SequenceNode {
		return
	}
	for _, e := range val.Content {
		if e.Kind == yaml.ScalarNode && e.Value != "" {
			idx.refs = append(idx.refs, SymbolRef{
				File: file, DocIndex: docIdx, Range: nodeRange(e),
				TargetKind: "LayoutPage", TargetName: e.Value, Field: "layoutPages",
			})
		}
		// Object form ({page, params}) is caught by the recursive `page` walk.
	}
}

// Definition returns the defining symbol for a kind+name, falling back to a
// unique name match when the kind is unknown.
func (idx *NameIndex) Definition(kind, name string) (SymbolDef, bool) {
	if kind != "" {
		if d, ok := idx.defs[kind+"\x00"+name]; ok {
			return d, true
		}
	}
	if defs := idx.byName[name]; len(defs) == 1 {
		return defs[0], true
	}
	return SymbolDef{}, false
}

// References returns every reference site targeting name (optionally constrained
// to a kind), plus the defining site.
func (idx *NameIndex) References(kind, name string) []SymbolRef {
	var out []SymbolRef
	for _, r := range idx.refs {
		if r.TargetName != name {
			continue
		}
		if kind != "" && r.TargetKind != "" && r.TargetKind != kind {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Defs returns every definition (for documentSymbol / workspaceSymbol).
func (idx *NameIndex) Defs() []SymbolDef {
	out := make([]SymbolDef, 0, len(idx.defs))
	for _, d := range idx.defs {
		out = append(out, d)
	}
	return out
}

// DefsInFile returns the definitions declared in a file (for documentSymbol).
func (idx *NameIndex) DefsInFile(file string) []SymbolDef {
	var out []SymbolDef
	for _, d := range idx.defs {
		if d.File == file {
			out = append(out, d)
		}
	}
	return out
}

// ByName returns all definitions sharing a name (for workspace-symbol fuzz).
func (idx *NameIndex) ByName(name string) []SymbolDef {
	return idx.byName[name]
}

func docEndLine(nodes []*yaml.Node, docIdx int) int {
	const eof = 1 << 30
	for i := docIdx + 1; i < len(nodes); i++ {
		if nodes[i] != nil {
			return nodes[i].Line
		}
	}
	return eof
}

func nodeRange(n *yaml.Node) Range {
	end := n.Column
	if n.Kind == yaml.ScalarNode {
		end = n.Column + len(n.Value)
	}
	return Range{StartLine: n.Line, StartCol: n.Column, EndLine: n.Line, EndCol: end}
}

func mappingChildValue(n *yaml.Node, key string) string {
	if c := mappingChild(n, key); c != nil && c.Kind == yaml.ScalarNode {
		return c.Value
	}
	return ""
}
