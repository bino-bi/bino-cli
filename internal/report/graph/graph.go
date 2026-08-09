package graph

import (
	"context"
	"sort"

	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/spec"
)

// NodeKind enumerates the supported graph node types.
type NodeKind string

const (
	NodeReportArtefact   NodeKind = "ReportArtefact"
	NodeDocumentArtefact NodeKind = "DocumentArtefact"
	NodeLayoutPage       NodeKind = "LayoutPage"
	NodeLayoutCard       NodeKind = "LayoutCard"
	NodeComponent        NodeKind = "Component"
	NodeDataSet          NodeKind = "DataSet"
	NodeDataSource       NodeKind = "DataSource"
	NodeMarkdownFile     NodeKind = "MarkdownFile"
)

// Node captures a manifest object, its metadata, and hashed dependencies.
type Node struct {
	ID         string
	Kind       NodeKind
	Name       string
	Label      string
	File       string
	Hash       string
	DependsOn  []string
	Attributes map[string]string

	baseDigest []byte
}

// Graph is the dependency graph produced from a manifest bundle.
type Graph struct {
	Nodes             map[string]*Node
	ReportArtefacts   []*Node
	DocumentArtefacts []*Node

	artefactIndex         map[string]*Node
	documentArtefactIndex map[string]*Node
}

// NodeByID returns the node for the given ID.
func (g *Graph) NodeByID(id string) (*Node, bool) {
	if g == nil {
		return nil, false
	}
	node, ok := g.Nodes[id]
	return node, ok
}

// NodeByManifest resolves the graph node for a manifest kind + name.
// Component kinds are stored as NodeComponent with the manifest kind in the
// node attributes (the set is derived from the builder's own componentKinds
// list, so it cannot drift); ReportArtefact uses its dedicated index; other
// kinds map directly onto a NodeKind. Returns nil when no node matches.
func (g *Graph) NodeByManifest(kind, name string) *Node {
	if g == nil {
		return nil
	}
	if kind == "ReportArtefact" {
		if node, ok := g.ReportArtefactByName(name); ok {
			return node
		}
		return nil
	}
	if _, isComponent := componentKindSet[kind]; isComponent {
		for _, node := range g.Nodes {
			if node.Kind == NodeComponent &&
				node.Attributes["componentKind"] == kind &&
				node.Name == name {
				return node
			}
		}
		return nil
	}
	targetKind := NodeKind(kind)
	for _, node := range g.Nodes {
		if node.Kind == targetKind && node.Name == name {
			return node
		}
	}
	return nil
}

// ReportArtefactByName resolves a ReportArtefact node by metadata.name.
func (g *Graph) ReportArtefactByName(name string) (*Node, bool) {
	if g == nil {
		return nil, false
	}
	node, ok := g.artefactIndex[name]
	return node, ok
}

// DocumentArtefactByName resolves a DocumentArtefact node by metadata.name.
func (g *Graph) DocumentArtefactByName(name string) (*Node, bool) {
	if g == nil {
		return nil, false
	}
	node, ok := g.documentArtefactIndex[name]
	return node, ok
}

// NodesByFile returns nodes whose File field matches any path in the given
// set. Comparison is exact string equality; callers must normalise paths
// (e.g. via pathutil.RelPath) on both sides before calling.
func (g *Graph) NodesByFile(files map[string]struct{}) []*Node {
	if g == nil || len(files) == 0 {
		return nil
	}
	matches := make([]*Node, 0)
	for _, n := range g.Nodes {
		if n == nil || n.File == "" {
			continue
		}
		if _, ok := files[n.File]; ok {
			matches = append(matches, n)
		}
	}
	return matches
}

// AffectedArtefacts walks reverse-dependency edges from each seed and returns
// the names of all ReportArtefact and DocumentArtefact nodes that
// transitively depend on any seed (including seeds that are themselves
// artefacts). Names are sorted and de-duplicated. Cost is O(N+E) per call —
// the reverse-edge index is built on demand because each preview refresh
// constructs a fresh Graph.
func (g *Graph) AffectedArtefacts(seeds []*Node) (reports, docs []string) {
	if g == nil || len(seeds) == 0 {
		return nil, nil
	}

	reverse := make(map[string][]string, len(g.Nodes))
	for id, n := range g.Nodes {
		if n == nil {
			continue
		}
		for _, dep := range n.DependsOn {
			reverse[dep] = append(reverse[dep], id)
		}
	}

	visited := make(map[string]struct{}, len(g.Nodes))
	queue := make([]string, 0, len(seeds))
	for _, s := range seeds {
		if s == nil {
			continue
		}
		if _, ok := visited[s.ID]; ok {
			continue
		}
		visited[s.ID] = struct{}{}
		queue = append(queue, s.ID)
	}

	reportSet := make(map[string]struct{})
	docSet := make(map[string]struct{})
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		node, ok := g.Nodes[id]
		if !ok {
			continue
		}
		switch node.Kind {
		case NodeReportArtefact:
			reportSet[node.Name] = struct{}{}
		case NodeDocumentArtefact:
			docSet[node.Name] = struct{}{}
		case NodeLayoutPage, NodeLayoutCard, NodeComponent, NodeDataSet, NodeDataSource, NodeMarkdownFile:
			// Intermediate kinds: keep walking up to find the artefact roots.
		}
		for _, parent := range reverse[id] {
			if _, seen := visited[parent]; seen {
				continue
			}
			visited[parent] = struct{}{}
			queue = append(queue, parent)
		}
	}

	return sortedSetKeys(reportSet), sortedSetKeys(docSet)
}

func sortedSetKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// BuildOptions configures graph construction.
type BuildOptions struct {
	// Mode is the current execution mode (build or preview).
	// Used for evaluating mode-based constraints.
	Mode spec.Mode
}

// Build constructs a Graph from validated manifest documents.
// This is a convenience wrapper that builds for all artifacts without constraint filtering.
func Build(ctx context.Context, docs []config.Document) (*Graph, error) {
	b := newBuilder(ctx, docs, BuildOptions{Mode: spec.ModeBuild})
	return b.Build()
}

// BuildWithOptions constructs a Graph with the specified options.
func BuildWithOptions(ctx context.Context, docs []config.Document, opts BuildOptions) (*Graph, error) {
	b := newBuilder(ctx, docs, opts)
	return b.Build()
}

// CollectReachable returns every node reachable from the given roots by
// following DependsOn edges, keyed by node ID.
func (g *Graph) CollectReachable(roots []*Node) map[string]*Node {
	visited := make(map[string]*Node)
	var walk func(node *Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if _, ok := visited[node.ID]; ok {
			return
		}
		visited[node.ID] = node
		for _, dep := range node.DependsOn {
			child, ok := g.NodeByID(dep)
			if !ok {
				continue
			}
			walk(child)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return visited
}

// DisplayName returns the node's human-readable label, falling back to its
// metadata name. Safe to call on a nil node.
func (n *Node) DisplayName() string {
	if n == nil {
		return ""
	}
	if n.Label != "" {
		return n.Label
	}
	return n.Name
}
