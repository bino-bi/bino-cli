package graph

import (
	"context"

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

// FilterDocumentsByConstraints filters documents based on their metadata.constraints
// against the given constraint context (artifact labels, spec, and mode).
// ReportArtefact documents are never filtered.
// Returns the filtered documents and an error if any constraint evaluation fails.
func FilterDocumentsByConstraints(docs []config.Document, ctx *spec.ConstraintContext) ([]config.Document, error) {
	if ctx == nil {
		return docs, nil
	}

	result := make([]config.Document, 0, len(docs))
	for _, doc := range docs {
		// ReportArtefacts are never filtered by constraints
		if doc.Kind == "ReportArtefact" {
			result = append(result, doc)
			continue
		}

		// No constraints means always included
		if len(doc.Constraints) == 0 {
			result = append(result, doc)
			continue
		}

		// Evaluate constraints
		match, err := spec.EvaluateParsedConstraintsWithContext(doc.Constraints, ctx, doc.Kind, doc.Name)
		if err != nil {
			return nil, err
		}

		if match {
			result = append(result, doc)
		}
	}

	return result, nil
}

// BuildForArtefact constructs a Graph for a specific artifact, applying constraint filtering.
// Only documents whose constraints match the artefact's context are included.
func BuildForArtefact(ctx context.Context, docs []config.Document, artifact config.Artifact, mode spec.Mode) (*Graph, error) {
	// Build constraint context from artifact
	specMap, err := spec.ToMap(artifact.Document.Raw)
	if err != nil {
		return nil, err
	}

	constraintCtx := &spec.ConstraintContext{
		Labels: artifact.Labels,
		Spec:   specMap,
		Mode:   mode,
	}

	// Filter documents by constraints
	filtered, err := FilterDocumentsByConstraints(docs, constraintCtx)
	if err != nil {
		return nil, err
	}

	// Validate name uniqueness for this artifact after filtering
	if err := config.ValidateArtefactNames(artifact.Document.Name, filtered); err != nil {
		return nil, err
	}

	// Build the graph with filtered documents
	return BuildWithOptions(ctx, filtered, BuildOptions{Mode: mode})
}
