package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"bino.bi/bino/internal/report/spec"
)

// MaterializeInlineDefinitions scans all documents for inline DataSet and DataSource
// definitions, generates hash-based names, creates synthetic Document structs,
// and rewrites references to use the generated names.
//
// This function is called during YAML loading (parse-time materialization).
// Returns the augmented document list with synthetic documents appended.
//
// Documents that may contain inline definitions:
//   - ChartStructure, ChartTime, Table, Text: spec.dataset can be inline
//   - ChartTree: spec.nodes[].spec.dataset can be inline (for Table, ChartStructure, ChartTime nodes)
//   - DataSet: spec.dependencies can contain inline DataSources, spec.source can be inline
//   - LayoutPage, LayoutCard: children[].spec.dataset can be inline, children[].spec.nodes[].spec.dataset for ChartTree
func MaterializeInlineDefinitions(docs []Document) ([]Document, error) {
	registry := newInlineRegistry()

	// First pass: scan for inline definitions and create synthetic documents
	for i := range docs {
		doc := &docs[i]
		switch doc.Kind {
		case "ChartStructure", "ChartTime", "Table", "Text":
			if err := materializeComponentInlines(doc, registry); err != nil {
				return nil, fmt.Errorf("%s %q: %w", doc.Kind, doc.Name, err)
			}
		case "ChartTree":
			if err := materializeChartTreeDocInlines(doc, registry); err != nil {
				return nil, fmt.Errorf("ChartTree %q: %w", doc.Name, err)
			}
		case "DataSet":
			if err := materializeDataSetInlines(doc, registry); err != nil {
				return nil, fmt.Errorf("DataSet %q: %w", doc.Name, err)
			}
		case "LayoutPage", "LayoutCard":
			if err := materializeLayoutChildrenInlines(doc, registry); err != nil {
				return nil, fmt.Errorf("%s %q: %w", doc.Kind, doc.Name, err)
			}
		}
	}

	// Append synthetic documents to the list
	return append(docs, registry.synthetics...), nil
}

// inlineRegistry tracks inline definitions by hash to enable deduplication.
type inlineRegistry struct {
	// hash -> generated name
	dataSourceByHash map[string]string
	dataSetByHash    map[string]string

	// All generated synthetic documents
	synthetics []Document
}

func newInlineRegistry() *inlineRegistry {
	return &inlineRegistry{
		dataSourceByHash: make(map[string]string),
		dataSetByHash:    make(map[string]string),
	}
}

// registerDataSource registers an inline DataSource spec and returns its generated name.
// If an identical spec was already registered, returns the existing name (deduplication).
func (r *inlineRegistry) registerDataSource(
	inlineSpec *spec.InlineDataSource,
	loc spec.InlineLocation,
) (string, error) {
	specJSON, err := json.Marshal(inlineSpec)
	if err != nil {
		return "", fmt.Errorf("marshal inline datasource: %w", err)
	}

	hash := computeSpecHash(specJSON)

	// Check for existing registration (deduplication)
	if existing, ok := r.dataSourceByHash[hash]; ok {
		return existing, nil
	}

	// Generate name and create synthetic document
	name := generateInlineName("datasource", hash)
	doc := createSyntheticDataSourceDocument(name, specJSON, loc)

	r.dataSourceByHash[hash] = name
	r.synthetics = append(r.synthetics, doc)
	return name, nil
}

// registerDataSet registers an inline DataSet spec and returns its generated name.
// If an identical spec was already registered, returns the existing name (deduplication).
func (r *inlineRegistry) registerDataSet(
	inlineSpec *spec.InlineDataSet,
	loc spec.InlineLocation,
) (string, error) {
	specJSON, err := json.Marshal(inlineSpec)
	if err != nil {
		return "", fmt.Errorf("marshal inline dataset: %w", err)
	}

	hash := computeSpecHash(specJSON)

	// Check for existing registration (deduplication)
	if existing, ok := r.dataSetByHash[hash]; ok {
		return existing, nil
	}

	// Generate name and create synthetic document
	name := generateInlineName("dataset", hash)
	doc := createSyntheticDataSetDocument(name, specJSON, loc)

	r.dataSetByHash[hash] = name
	r.synthetics = append(r.synthetics, doc)
	return name, nil
}

// computeSpecHash computes a stable SHA256 hash of a spec.
// The spec is normalized (sorted keys via json.Marshal) before hashing.
func computeSpecHash(specJSON []byte) string {
	// Normalize by unmarshaling and remarshaling (json.Marshal sorts map keys)
	var normalized any
	if err := json.Unmarshal(specJSON, &normalized); err != nil {
		// Fallback: hash raw bytes
		sum := sha256.Sum256(specJSON)
		return hex.EncodeToString(sum[:])
	}

	canonical, _ := json.Marshal(normalized)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// generateInlineName creates a deterministic name for an inline definition.
// Format: _inline_<kind>_<hash16> where hash16 is first 16 chars of SHA256.
func generateInlineName(kind, hash string) string {
	hashPrefix := hash
	if len(hashPrefix) > 16 {
		hashPrefix = hashPrefix[:16]
	}
	return fmt.Sprintf("_inline_%s_%s", kind, hashPrefix)
}

// createSyntheticDataSourceDocument creates a Document struct for a materialized inline DataSource.
func createSyntheticDataSourceDocument(name string, specJSON []byte, loc spec.InlineLocation) Document {
	// Build complete document JSON
	docMap := map[string]any{
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       "DataSource",
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]string{
				"bino.bi/generated": "true",
				"bino.bi/inline":    "true",
			},
			"annotations": map[string]string{
				"bino.bi/source-file":     loc.File,
				"bino.bi/source-position": fmt.Sprintf("%d", loc.Position),
				"bino.bi/source-parent":   fmt.Sprintf("%s/%s", loc.ParentKind, loc.ParentName),
				"bino.bi/source-path":     loc.Path,
			},
		},
	}

	// Parse the spec JSON and add to document
	var specMap map[string]any
	if err := json.Unmarshal(specJSON, &specMap); err == nil {
		docMap["spec"] = specMap
	}

	raw, _ := json.Marshal(docMap)

	return Document{
		File:     loc.File,
		Position: -1, // Negative indicates synthetic
		Kind:     "DataSource",
		Name:     name,
		Labels: map[string]string{
			"bino.bi/generated": "true",
			"bino.bi/inline":    "true",
		},
		Raw: raw,
	}
}

// createSyntheticDataSetDocument creates a Document struct for a materialized inline DataSet.
func createSyntheticDataSetDocument(name string, specJSON []byte, loc spec.InlineLocation) Document {
	// Build complete document JSON
	docMap := map[string]any{
		"apiVersion": "bino.bi/v1alpha1",
		"kind":       "DataSet",
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]string{
				"bino.bi/generated": "true",
				"bino.bi/inline":    "true",
			},
			"annotations": map[string]string{
				"bino.bi/source-file":     loc.File,
				"bino.bi/source-position": fmt.Sprintf("%d", loc.Position),
				"bino.bi/source-parent":   fmt.Sprintf("%s/%s", loc.ParentKind, loc.ParentName),
				"bino.bi/source-path":     loc.Path,
			},
		},
	}

	// Parse the spec JSON and add to document
	var specMap map[string]any
	if err := json.Unmarshal(specJSON, &specMap); err == nil {
		docMap["spec"] = specMap
	}

	raw, _ := json.Marshal(docMap)

	return Document{
		File:     loc.File,
		Position: -1, // Negative indicates synthetic
		Kind:     "DataSet",
		Name:     name,
		Labels: map[string]string{
			"bino.bi/generated": "true",
			"bino.bi/inline":    "true",
		},
		Raw: raw,
	}
}

// componentSpec is a minimal struct to parse the dataset field from component specs.
type componentSpec struct {
	Spec struct {
		Dataset spec.DatasetList `json:"dataset"`
	} `json:"spec"`
}

// materializeComponentInlines processes inline definitions in component specs
// (ChartStructure, ChartTime, Table, Text).
func materializeComponentInlines(doc *Document, registry *inlineRegistry) error {
	var comp componentSpec
	if err := json.Unmarshal(doc.Raw, &comp); err != nil {
		return nil // Not a component with dataset field, skip
	}

	if !comp.Spec.Dataset.HasInline() {
		return nil // No inline definitions, nothing to do
	}

	entries := comp.Spec.Dataset.Entries()
	resolvedNames := make([]string, 0, len(entries))

	for i, entry := range entries {
		if entry.IsRef() {
			resolvedNames = append(resolvedNames, entry.Ref)
			continue
		}

		if entry.Inline == nil {
			continue
		}

		// Process inline DataSet
		loc := spec.InlineLocation{
			File:       doc.File,
			Position:   doc.Position,
			ParentKind: doc.Kind,
			ParentName: doc.Name,
			Path:       fmt.Sprintf("spec.dataset[%d]", i),
		}

		// First, materialize any inline DataSources in the dependencies
		if err := materializeInlineDataSources(entry.Inline, loc, registry); err != nil {
			return fmt.Errorf("inline dataset[%d]: %w", i, err)
		}

		// Register the inline DataSet
		name, err := registry.registerDataSet(entry.Inline, loc)
		if err != nil {
			return fmt.Errorf("inline dataset[%d]: %w", i, err)
		}

		resolvedNames = append(resolvedNames, name)
	}

	// Rewrite the document to use resolved names
	return rewriteDocumentDataset(doc, resolvedNames)
}

// dataSetDocSpec is a minimal struct to parse DataSet documents.
type dataSetDocSpec struct {
	Spec struct {
		Query        spec.QueryField     `json:"query"`
		Prql         spec.QueryField     `json:"prql"`
		Source       *spec.DataSourceRef `json:"source"`
		Dependencies []spec.DataSourceRef `json:"dependencies"`
	} `json:"spec"`
}

// materializeDataSetInlines processes inline definitions in DataSet documents.
func materializeDataSetInlines(doc *Document, registry *inlineRegistry) error {
	var dsDoc dataSetDocSpec
	if err := json.Unmarshal(doc.Raw, &dsDoc); err != nil {
		return nil // Parse error, skip
	}

	hasInline := false

	// Check for inline DataSources in dependencies
	for _, dep := range dsDoc.Spec.Dependencies {
		if dep.IsInline() {
			hasInline = true
			break
		}
	}

	// Check for inline DataSource in source field
	if dsDoc.Spec.Source != nil && dsDoc.Spec.Source.IsInline() {
		hasInline = true
	}

	if !hasInline {
		return nil // No inline definitions, nothing to do
	}

	loc := spec.InlineLocation{
		File:       doc.File,
		Position:   doc.Position,
		ParentKind: doc.Kind,
		ParentName: doc.Name,
	}

	// Materialize inline DataSources in dependencies
	resolvedDeps := make([]string, 0, len(dsDoc.Spec.Dependencies))
	for i, dep := range dsDoc.Spec.Dependencies {
		if dep.IsRef() {
			resolvedDeps = append(resolvedDeps, dep.Ref)
			continue
		}

		if dep.Inline == nil {
			continue
		}

		depLoc := loc
		depLoc.Path = fmt.Sprintf("spec.dependencies[%d]", i)

		name, err := registry.registerDataSource(dep.Inline, depLoc)
		if err != nil {
			return fmt.Errorf("inline dependency[%d]: %w", i, err)
		}

		resolvedDeps = append(resolvedDeps, name)
	}

	// Materialize inline DataSource in source field
	var resolvedSource string
	if dsDoc.Spec.Source != nil {
		if dsDoc.Spec.Source.IsRef() {
			resolvedSource = dsDoc.Spec.Source.Ref
		} else if dsDoc.Spec.Source.IsInline() {
			sourceLoc := loc
			sourceLoc.Path = "spec.source"

			name, err := registry.registerDataSource(dsDoc.Spec.Source.Inline, sourceLoc)
			if err != nil {
				return fmt.Errorf("inline source: %w", err)
			}

			resolvedSource = name
		}
	}

	// Rewrite the document to use resolved names
	return rewriteDataSetDocument(doc, resolvedDeps, resolvedSource)
}

// materializeInlineDataSources processes inline DataSources in an InlineDataSet's dependencies.
func materializeInlineDataSources(inlineDS *spec.InlineDataSet, parentLoc spec.InlineLocation, registry *inlineRegistry) error {
	if len(inlineDS.Dependencies) == 0 && inlineDS.Source == nil {
		return nil
	}

	// Process dependencies
	resolvedDeps := make([]spec.DataSourceRef, 0, len(inlineDS.Dependencies))
	for i, dep := range inlineDS.Dependencies {
		if dep.IsRef() {
			resolvedDeps = append(resolvedDeps, dep)
			continue
		}

		if dep.Inline == nil {
			continue
		}

		loc := parentLoc
		loc.Path = fmt.Sprintf("%s.dependencies[%d]", parentLoc.Path, i)

		name, err := registry.registerDataSource(dep.Inline, loc)
		if err != nil {
			return fmt.Errorf("inline dependency[%d]: %w", i, err)
		}

		// Replace inline with string ref
		resolvedDeps = append(resolvedDeps, spec.DataSourceRef{Ref: name})
	}

	inlineDS.Dependencies = resolvedDeps

	// Process source field
	if inlineDS.Source != nil && inlineDS.Source.IsInline() {
		loc := parentLoc
		loc.Path = parentLoc.Path + ".source"

		name, err := registry.registerDataSource(inlineDS.Source.Inline, loc)
		if err != nil {
			return fmt.Errorf("inline source: %w", err)
		}

		inlineDS.Source = &spec.DataSourceRef{Ref: name}
	}

	return nil
}

// rewriteDocumentDataset rewrites a component document to use resolved dataset names.
func rewriteDocumentDataset(doc *Document, resolvedNames []string) error {
	var docMap map[string]any
	if err := json.Unmarshal(doc.Raw, &docMap); err != nil {
		return fmt.Errorf("unmarshal document: %w", err)
	}

	specMap, ok := docMap["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Replace dataset field with resolved names
	if len(resolvedNames) == 1 {
		specMap["dataset"] = resolvedNames[0]
	} else {
		specMap["dataset"] = resolvedNames
	}

	raw, err := json.Marshal(docMap)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}

	doc.Raw = raw
	return nil
}

// rewriteDataSetDocument rewrites a DataSet document to use resolved dependency/source names.
func rewriteDataSetDocument(doc *Document, resolvedDeps []string, resolvedSource string) error {
	var docMap map[string]any
	if err := json.Unmarshal(doc.Raw, &docMap); err != nil {
		return fmt.Errorf("unmarshal document: %w", err)
	}

	specMap, ok := docMap["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Replace dependencies field
	if len(resolvedDeps) > 0 {
		specMap["dependencies"] = resolvedDeps
	}

	// Replace source field
	if resolvedSource != "" {
		specMap["source"] = resolvedSource
	}

	raw, err := json.Marshal(docMap)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}

	doc.Raw = raw
	return nil
}

// IsGeneratedInline returns true if the document is a generated inline definition.
func IsGeneratedInline(doc Document) bool {
	return doc.Labels["bino.bi/generated"] == "true" && doc.Labels["bino.bi/inline"] == "true"
}

// layoutSpec is used to parse LayoutPage/LayoutCard children.
type layoutSpec struct {
	Spec struct {
		Children []layoutChild `json:"children"`
	} `json:"spec"`
}

// layoutChild represents an inline component within a layout.
type layoutChild struct {
	Kind string          `json:"kind"`
	Ref  string          `json:"ref,omitempty"`
	Spec json.RawMessage `json:"spec,omitempty"`
}

// childSpec is used to extract the dataset field from a child's spec.
type childSpec struct {
	Dataset spec.DatasetList `json:"dataset"`
}

// materializeLayoutChildrenInlines processes inline definitions in LayoutPage/LayoutCard children.
// This handles cases where components (Table, ChartStructure, etc.) are defined inline
// within a layout's children array with inline dataset definitions.
func materializeLayoutChildrenInlines(doc *Document, registry *inlineRegistry) error {
	var layout layoutSpec
	if err := json.Unmarshal(doc.Raw, &layout); err != nil {
		return nil // Parse error, skip
	}

	if len(layout.Spec.Children) == 0 {
		return nil
	}

	modified := false

	// Process each child
	for i, child := range layout.Spec.Children {
		// Skip children that reference other documents (no inline spec)
		if child.Ref != "" || len(child.Spec) == 0 {
			continue
		}

		// Only process component kinds that can have datasets
		switch child.Kind {
		case "ChartStructure", "ChartTime", "Table", "Text":
			// Parse the child's spec to check for inline datasets
			var cs childSpec
			if err := json.Unmarshal(child.Spec, &cs); err != nil {
				continue
			}

			if !cs.Dataset.HasInline() {
				continue
			}

			// Process inline datasets
			entries := cs.Dataset.Entries()
			resolvedNames := make([]string, 0, len(entries))

			for j, entry := range entries {
				if entry.IsRef() {
					resolvedNames = append(resolvedNames, entry.Ref)
					continue
				}

				if entry.Inline == nil {
					continue
				}

				// Process inline DataSet
				loc := spec.InlineLocation{
					File:       doc.File,
					Position:   doc.Position,
					ParentKind: doc.Kind,
					ParentName: doc.Name,
					Path:       fmt.Sprintf("spec.children[%d].spec.dataset[%d]", i, j),
				}

				// First, materialize any inline DataSources in the dependencies
				if err := materializeInlineDataSources(entry.Inline, loc, registry); err != nil {
					return fmt.Errorf("child[%d].dataset[%d]: %w", i, j, err)
				}

				// Register the inline DataSet
				name, err := registry.registerDataSet(entry.Inline, loc)
				if err != nil {
					return fmt.Errorf("child[%d].dataset[%d]: %w", i, j, err)
				}

				resolvedNames = append(resolvedNames, name)
			}

			// Rewrite the child's spec
			if len(resolvedNames) > 0 {
				if err := rewriteLayoutChildDataset(doc, i, resolvedNames); err != nil {
					return fmt.Errorf("child[%d]: %w", i, err)
				}
				modified = true
			}

		case "ChartTree":
			// Process ChartTree nodes that may contain inline datasets
			if err := materializeChartTreeNodesInlines(doc, i, child.Spec, registry); err != nil {
				return fmt.Errorf("child[%d] (ChartTree): %w", i, err)
			}
			modified = true
		}
	}

	// Re-parse if modified to ensure consistency
	if modified {
		// Already modified in rewriteLayoutChildDataset
	}

	return nil
}

// chartTreeSpec is used to parse ChartTree spec for inline processing.
type chartTreeSpec struct {
	Nodes []chartTreeNode `json:"nodes"`
}

// chartTreeNode represents a node in a ChartTree.
type chartTreeNode struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Ref  string          `json:"ref,omitempty"`
	Spec json.RawMessage `json:"spec,omitempty"`
}

// chartTreeDocSpec wraps the spec for parsing standalone ChartTree documents.
type chartTreeDocSpec struct {
	Spec chartTreeSpec `json:"spec"`
}

// materializeChartTreeDocInlines processes inline datasets in standalone ChartTree documents.
func materializeChartTreeDocInlines(doc *Document, registry *inlineRegistry) error {
	var treeDoc chartTreeDocSpec
	if err := json.Unmarshal(doc.Raw, &treeDoc); err != nil {
		return nil // Parse error, skip
	}

	if len(treeDoc.Spec.Nodes) == 0 {
		return nil
	}

	// Process each node
	for nodeIdx, node := range treeDoc.Spec.Nodes {
		// Skip nodes that reference other documents (no inline spec)
		if node.Ref != "" || len(node.Spec) == 0 {
			continue
		}

		// Only process component kinds that can have datasets
		switch node.Kind {
		case "Table", "ChartStructure", "ChartTime":
			// Parse the node's spec to check for inline datasets
			var cs childSpec
			if err := json.Unmarshal(node.Spec, &cs); err != nil {
				continue
			}

			if !cs.Dataset.HasInline() {
				continue
			}

			// Process inline datasets
			entries := cs.Dataset.Entries()
			resolvedNames := make([]string, 0, len(entries))

			for j, entry := range entries {
				if entry.IsRef() {
					resolvedNames = append(resolvedNames, entry.Ref)
					continue
				}

				if entry.Inline == nil {
					continue
				}

				// Process inline DataSet
				loc := spec.InlineLocation{
					File:       doc.File,
					Position:   doc.Position,
					ParentKind: doc.Kind,
					ParentName: doc.Name,
					Path:       fmt.Sprintf("spec.nodes[%d].spec.dataset[%d]", nodeIdx, j),
				}

				// First, materialize any inline DataSources in the dependencies
				if err := materializeInlineDataSources(entry.Inline, loc, registry); err != nil {
					return fmt.Errorf("node[%d].dataset[%d]: %w", nodeIdx, j, err)
				}

				// Register the inline DataSet
				name, err := registry.registerDataSet(entry.Inline, loc)
				if err != nil {
					return fmt.Errorf("node[%d].dataset[%d]: %w", nodeIdx, j, err)
				}

				resolvedNames = append(resolvedNames, name)
			}

			// Rewrite the node's dataset
			if len(resolvedNames) > 0 {
				if err := rewriteChartTreeDocNodeDataset(doc, nodeIdx, resolvedNames); err != nil {
					return fmt.Errorf("node[%d]: %w", nodeIdx, err)
				}
			}
		}
	}

	return nil
}

// rewriteChartTreeDocNodeDataset rewrites a standalone ChartTree node's dataset field.
func rewriteChartTreeDocNodeDataset(doc *Document, nodeIndex int, resolvedNames []string) error {
	var docMap map[string]any
	if err := json.Unmarshal(doc.Raw, &docMap); err != nil {
		return fmt.Errorf("unmarshal document: %w", err)
	}

	specMap, ok := docMap["spec"].(map[string]any)
	if !ok {
		return nil
	}

	nodes, ok := specMap["nodes"].([]any)
	if !ok || nodeIndex >= len(nodes) {
		return nil
	}

	node, ok := nodes[nodeIndex].(map[string]any)
	if !ok {
		return nil
	}

	nodeSpecMap, ok := node["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Replace dataset field with resolved names
	if len(resolvedNames) == 1 {
		nodeSpecMap["dataset"] = resolvedNames[0]
	} else {
		nodeSpecMap["dataset"] = resolvedNames
	}

	raw, err := json.Marshal(docMap)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}

	doc.Raw = raw
	return nil
}

// materializeChartTreeNodesInlines processes inline datasets in ChartTree nodes.
// ChartTree nodes can contain Table, ChartStructure, ChartTime components with inline datasets.
func materializeChartTreeNodesInlines(doc *Document, childIndex int, treeSpecRaw json.RawMessage, registry *inlineRegistry) error {
	var treeSpec chartTreeSpec
	if err := json.Unmarshal(treeSpecRaw, &treeSpec); err != nil {
		return nil // Parse error, skip
	}

	if len(treeSpec.Nodes) == 0 {
		return nil
	}

	// Track if any modifications were made
	modified := false

	// Process each node
	for nodeIdx, node := range treeSpec.Nodes {
		// Skip nodes that reference other documents (no inline spec)
		if node.Ref != "" || len(node.Spec) == 0 {
			continue
		}

		// Only process component kinds that can have datasets
		switch node.Kind {
		case "Table", "ChartStructure", "ChartTime":
			// Parse the node's spec to check for inline datasets
			var cs childSpec
			if err := json.Unmarshal(node.Spec, &cs); err != nil {
				continue
			}

			if !cs.Dataset.HasInline() {
				continue
			}

			// Process inline datasets
			entries := cs.Dataset.Entries()
			resolvedNames := make([]string, 0, len(entries))

			for j, entry := range entries {
				if entry.IsRef() {
					resolvedNames = append(resolvedNames, entry.Ref)
					continue
				}

				if entry.Inline == nil {
					continue
				}

				// Process inline DataSet
				loc := spec.InlineLocation{
					File:       doc.File,
					Position:   doc.Position,
					ParentKind: doc.Kind,
					ParentName: doc.Name,
					Path:       fmt.Sprintf("spec.children[%d].spec.nodes[%d].spec.dataset[%d]", childIndex, nodeIdx, j),
				}

				// First, materialize any inline DataSources in the dependencies
				if err := materializeInlineDataSources(entry.Inline, loc, registry); err != nil {
					return fmt.Errorf("node[%d].dataset[%d]: %w", nodeIdx, j, err)
				}

				// Register the inline DataSet
				name, err := registry.registerDataSet(entry.Inline, loc)
				if err != nil {
					return fmt.Errorf("node[%d].dataset[%d]: %w", nodeIdx, j, err)
				}

				resolvedNames = append(resolvedNames, name)
			}

			// Rewrite the node's dataset
			if len(resolvedNames) > 0 {
				if err := rewriteChartTreeNodeDataset(doc, childIndex, nodeIdx, resolvedNames); err != nil {
					return fmt.Errorf("node[%d]: %w", nodeIdx, err)
				}
				modified = true
			}
		}
	}

	_ = modified // Prevent unused variable warning
	return nil
}

// rewriteChartTreeNodeDataset rewrites a ChartTree node's dataset field to use resolved names.
func rewriteChartTreeNodeDataset(doc *Document, childIndex, nodeIndex int, resolvedNames []string) error {
	var docMap map[string]any
	if err := json.Unmarshal(doc.Raw, &docMap); err != nil {
		return fmt.Errorf("unmarshal document: %w", err)
	}

	specMap, ok := docMap["spec"].(map[string]any)
	if !ok {
		return nil
	}

	children, ok := specMap["children"].([]any)
	if !ok || childIndex >= len(children) {
		return nil
	}

	child, ok := children[childIndex].(map[string]any)
	if !ok {
		return nil
	}

	childSpecMap, ok := child["spec"].(map[string]any)
	if !ok {
		return nil
	}

	nodes, ok := childSpecMap["nodes"].([]any)
	if !ok || nodeIndex >= len(nodes) {
		return nil
	}

	node, ok := nodes[nodeIndex].(map[string]any)
	if !ok {
		return nil
	}

	nodeSpecMap, ok := node["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Replace dataset field with resolved names
	if len(resolvedNames) == 1 {
		nodeSpecMap["dataset"] = resolvedNames[0]
	} else {
		nodeSpecMap["dataset"] = resolvedNames
	}

	raw, err := json.Marshal(docMap)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}

	doc.Raw = raw
	return nil
}

// rewriteLayoutChildDataset rewrites a layout child's dataset field to use resolved names.
func rewriteLayoutChildDataset(doc *Document, childIndex int, resolvedNames []string) error {
	var docMap map[string]any
	if err := json.Unmarshal(doc.Raw, &docMap); err != nil {
		return fmt.Errorf("unmarshal document: %w", err)
	}

	specMap, ok := docMap["spec"].(map[string]any)
	if !ok {
		return nil
	}

	children, ok := specMap["children"].([]any)
	if !ok || childIndex >= len(children) {
		return nil
	}

	child, ok := children[childIndex].(map[string]any)
	if !ok {
		return nil
	}

	childSpecMap, ok := child["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Replace dataset field with resolved names
	if len(resolvedNames) == 1 {
		childSpecMap["dataset"] = resolvedNames[0]
	} else {
		childSpecMap["dataset"] = resolvedNames
	}

	raw, err := json.Marshal(docMap)
	if err != nil {
		return fmt.Errorf("marshal document: %w", err)
	}

	doc.Raw = raw
	return nil
}
