package daemon

import (
	"context"
	"fmt"

	"bino.bi/bino/internal/report/graph"
)

// Canonical introspection result shapes, shared by the HTTP daemon handlers and
// the MCP server. The JSON tags match the stable lsp-helper contract that VS
// Code and sandbox already consume — do not invent new shapes for the same data.

// IndexDocument is a single entry in the project index.
type IndexDocument struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	File     string `json:"file"`
	Position int    `json:"position"`
}

// IndexResult lists every document discovered in the project.
type IndexResult struct {
	Documents []IndexDocument `json:"documents"`
}

// ValidateResult reports whether the project validates plus its diagnostics.
type ValidateResult struct {
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// ColumnsResult holds the columns of a DataSource or DataSet.
type ColumnsResult struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Error   string   `json:"error,omitempty"`
}

// RowsResult holds a preview of rows for a DataSource or DataSet.
type RowsResult struct {
	Name      string           `json:"name"`
	Kind      string           `json:"kind"`
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Limit     int              `json:"limit"`
	Truncated bool             `json:"truncated"`
	Error     string           `json:"error,omitempty"`
}

// GraphNode is a node in a dependency-graph traversal result.
type GraphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Hash string `json:"hash,omitempty"`
}

// GraphEdge is an edge in a dependency-graph traversal result.
type GraphEdge struct {
	FromID    string `json:"fromId"`
	ToID      string `json:"toId"`
	Direction string `json:"direction"`
}

// GraphDepsResult is the result of a dependency-graph traversal.
type GraphDepsResult struct {
	RootID    string      `json:"rootId"`
	Direction string      `json:"direction"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Error     string      `json:"error,omitempty"`
}

// Index returns the project document index.
func (s *State) Index() IndexResult {
	docs := s.Documents()
	out := IndexResult{Documents: make([]IndexDocument, 0, len(docs))}
	for _, doc := range docs {
		out.Documents = append(out.Documents, IndexDocument{
			Kind:     doc.Kind,
			Name:     doc.Name,
			File:     doc.File,
			Position: doc.Position,
		})
	}
	return out
}

// Validate returns the project diagnostics. When execQueries is true, datasets
// are executed and data-validation warnings are appended (slower).
func (s *State) Validate(ctx context.Context, execQueries bool) ValidateResult {
	var diags []Diagnostic
	if execQueries {
		diags = s.ValidateWithQueries(ctx)
	} else {
		diags = s.Diagnostics()
	}
	return ValidateResult{Valid: len(diags) == 0, Diagnostics: diags}
}

// Columns wraps IntrospectColumns into the canonical result shape; errors are
// captured in the Error field rather than returned.
func (s *State) Columns(ctx context.Context, name string) ColumnsResult {
	cols, err := s.IntrospectColumns(ctx, name)
	res := ColumnsResult{Name: name, Columns: cols}
	if res.Columns == nil {
		res.Columns = []string{}
	}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

// Rows wraps QueryRows into the canonical result shape; errors are captured in
// the Error field rather than returned.
func (s *State) Rows(ctx context.Context, name string, limit int) RowsResult {
	cols, rows, truncated, kind, err := s.QueryRows(ctx, name, limit)
	res := RowsResult{Name: name, Kind: kind, Columns: cols, Rows: rows, Limit: limit, Truncated: truncated}
	if res.Columns == nil {
		res.Columns = []string{}
	}
	if res.Rows == nil {
		res.Rows = []map[string]any{}
	}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

// GraphDeps traverses the dependency graph from the node identified by kind+name.
// direction is "in" (dependents), "out" (dependencies), or "both" (default).
// Logical errors (bad params, node not found) are captured in the Error field.
func (s *State) GraphDeps(ctx context.Context, kind, name, direction string, maxDepth int) GraphDepsResult {
	if direction == "" {
		direction = "both"
	}
	res := GraphDepsResult{Direction: direction, Nodes: []GraphNode{}, Edges: []GraphEdge{}}

	if kind == "" || name == "" {
		res.Error = "both kind and name parameters are required"
		return res
	}
	if direction != "in" && direction != "out" && direction != "both" {
		res.Error = "direction must be 'in', 'out', or 'both'"
		return res
	}

	g, err := s.BuildGraph(ctx)
	if err != nil {
		res.Error = fmt.Sprintf("build graph: %v", err)
		return res
	}

	rootNode := findGraphNode(g, kind, name)
	if rootNode == nil {
		res.Error = fmt.Sprintf("node not found: %s:%s", kind, name)
		return res
	}
	res.RootID = rootNode.ID

	reverseAdj := make(map[string][]string)
	for _, node := range g.Nodes {
		for _, depID := range node.DependsOn {
			reverseAdj[depID] = append(reverseAdj[depID], node.ID)
		}
	}

	visited := make(map[string]bool)
	var edges []GraphEdge
	if direction == "out" || direction == "both" {
		traverseGraph(g, rootNode.ID, "out", maxDepth, visited, &edges, nil)
	}
	if direction == "in" || direction == "both" {
		traverseGraph(g, rootNode.ID, "in", maxDepth, visited, &edges, reverseAdj)
	}

	for nodeID := range visited {
		node, ok := g.NodeByID(nodeID)
		if !ok {
			continue
		}
		res.Nodes = append(res.Nodes, GraphNode{
			ID:   node.ID,
			Kind: string(node.Kind),
			Name: node.Name,
			File: node.File,
			Hash: node.Hash,
		})
	}
	res.Edges = edges
	return res
}

// findGraphNode resolves a graph node by manifest kind + name.
func findGraphNode(g *graph.Graph, kind, name string) *graph.Node {
	if kind == "ReportArtefact" {
		if node, ok := g.ReportArtefactByName(name); ok {
			return node
		}
		return nil
	}

	componentKinds := map[string]bool{
		"Text": true, "Table": true, "ChartStructure": true,
		"ChartTime": true, "ChartScatter": true, "ChartBubble": true, "ChartBullet": true, "Tree": true, "Grid": true, "Image": true, "Asset": true,
	}
	if componentKinds[kind] {
		for _, node := range g.Nodes {
			if node.Kind == graph.NodeComponent &&
				node.Attributes["componentKind"] == kind &&
				node.Name == name {
				return node
			}
		}
		return nil
	}

	targetKind := graph.NodeKind(kind)
	for _, node := range g.Nodes {
		if node.Kind == targetKind && node.Name == name {
			return node
		}
	}
	return nil
}

// traverseGraph performs a BFS in the requested direction, recording edges and
// visited nodes. dir "out" follows DependsOn; "in" follows the reverse adjacency.
func traverseGraph(
	g *graph.Graph,
	rootID string,
	dir string,
	maxDepth int,
	visited map[string]bool,
	edges *[]GraphEdge,
	reverseAdj map[string][]string,
) {
	type queueItem struct {
		id    string
		depth int
	}
	queue := []queueItem{{id: rootID, depth: 0}}
	visited[rootID] = true

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if maxDepth > 0 && item.depth >= maxDepth {
			continue
		}

		var neighbors []string
		if dir == "out" {
			if node, ok := g.NodeByID(item.id); ok {
				neighbors = node.DependsOn
			}
		} else {
			neighbors = reverseAdj[item.id]
		}

		for _, neighborID := range neighbors {
			if dir == "out" {
				*edges = append(*edges, GraphEdge{FromID: item.id, ToID: neighborID, Direction: "out"})
			} else {
				*edges = append(*edges, GraphEdge{FromID: neighborID, ToID: item.id, Direction: "in"})
			}

			if !visited[neighborID] {
				visited[neighborID] = true
				queue = append(queue, queueItem{id: neighborID, depth: item.depth + 1})
			}
		}
	}
}
