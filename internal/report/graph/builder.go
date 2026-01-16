package graph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
)

type builder struct {
	ctx  context.Context
	docs []config.Document
	opts BuildOptions

	nodes map[string]*Node

	dataSourceDocs []config.Document
	dataSetDocs    []config.Document
	layoutPageDocs []config.Document
	layoutCardDocs []config.Document
	componentDocs  map[string][]config.Document
	artefactDocs   []config.Document

	dataSourceIndex map[string]string
	dataSetIndex    map[string]string

	// docIndex maps kind+name to the document for ref resolution.
	// Key format: "Kind:Name" (e.g., "ChartTime:sampleTimeChart").
	docIndex map[string]config.Document

	layoutRootIDs          []string
	standaloneComponentIDs []string
	artefactIDs            []string
}

func newBuilder(ctx context.Context, docs []config.Document, opts BuildOptions) *builder {
	return &builder{
		ctx:                    ctx,
		docs:                   docs,
		opts:                   opts,
		nodes:                  make(map[string]*Node),
		componentDocs:          make(map[string][]config.Document),
		dataSourceIndex:        make(map[string]string),
		dataSetIndex:           make(map[string]string),
		docIndex:               make(map[string]config.Document),
		layoutRootIDs:          nil,
		artefactIDs:            nil,
		standaloneComponentIDs: nil,
	}
}

// Build constructs the dependency graph from the loaded documents.
func (b *builder) Build() (*Graph, error) {
	b.categorize()

	if err := b.buildDataSources(); err != nil {
		return nil, err
	}
	if err := b.buildDataSets(); err != nil {
		return nil, err
	}
	if err := b.buildStandaloneComponents(); err != nil {
		return nil, err
	}
	if err := b.buildLayouts(NodeLayoutPage, b.layoutPageDocs); err != nil {
		return nil, err
	}
	if err := b.buildLayouts(NodeLayoutCard, b.layoutCardDocs); err != nil {
		return nil, err
	}
	if err := b.buildReportArtefacts(); err != nil {
		return nil, err
	}
	if err := b.computeHashes(); err != nil {
		return nil, err
	}

	graph := &Graph{
		Nodes:           b.nodes,
		ReportArtefacts: make([]*Node, 0, len(b.artefactIDs)),
		artefactIndex:   make(map[string]*Node),
	}
	for _, id := range b.artefactIDs {
		node, ok := b.nodes[id]
		if !ok {
			continue
		}
		graph.ReportArtefacts = append(graph.ReportArtefacts, node)
		graph.artefactIndex[node.Name] = node
	}
	sort.Slice(graph.ReportArtefacts, func(i, j int) bool {
		return graph.ReportArtefacts[i].Name < graph.ReportArtefacts[j].Name
	})
	return graph, nil
}

func (b *builder) categorize() {
	for _, doc := range b.docs {
		// Build docIndex for ref resolution (all kinds except DataSource/DataSet/ReportArtefact).
		switch doc.Kind {
		case "LayoutPage", "LayoutCard", "Text", "Table", "ChartStructure", "ChartTime", "Image", "Asset":
			key := doc.Kind + ":" + doc.Name
			b.docIndex[key] = doc
		}

		switch doc.Kind {
		case "DataSource":
			b.dataSourceDocs = append(b.dataSourceDocs, doc)
		case "DataSet":
			b.dataSetDocs = append(b.dataSetDocs, doc)
		case "LayoutPage":
			b.layoutPageDocs = append(b.layoutPageDocs, doc)
		case "LayoutCard":
			b.layoutCardDocs = append(b.layoutCardDocs, doc)
		case "Text", "Table", "ChartStructure", "ChartTime", "Image", "Asset":
			b.componentDocs[doc.Kind] = append(b.componentDocs[doc.Kind], doc)
		case "ReportArtefact":
			b.artefactDocs = append(b.artefactDocs, doc)
		}
	}
}

func (b *builder) buildDataSources() error {
	for _, doc := range b.dataSourceDocs {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		spec, err := parseDataSourceSpec(doc.Raw)
		if err != nil {
			return fmt.Errorf("datasource %s: %w", doc.Name, err)
		}
		digest, attrs, err := b.hashDataSource(doc, spec)
		if err != nil {
			return fmt.Errorf("datasource %s: %w", doc.Name, err)
		}
		id := makeNodeID(NodeDataSource, doc.Name)
		node := &Node{
			ID:         id,
			Kind:       NodeDataSource,
			Name:       doc.Name,
			Label:      doc.Name,
			File:       doc.File,
			DependsOn:  nil,
			Attributes: attrs,
			baseDigest: digest,
		}
		b.nodes[id] = node
		b.dataSourceIndex[doc.Name] = id
	}
	return nil
}

func (b *builder) buildDataSets() error {
	for _, doc := range b.dataSetDocs {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		spec, err := parseDataSetSpec(doc.Raw)
		if err != nil {
			return fmt.Errorf("dataset %s: %w", doc.Name, err)
		}
		digest, attrs, deps, err := b.hashDataSet(doc, spec)
		if err != nil {
			return fmt.Errorf("dataset %s: %w", doc.Name, err)
		}
		id := makeNodeID(NodeDataSet, doc.Name)
		node := &Node{
			ID:         id,
			Kind:       NodeDataSet,
			Name:       doc.Name,
			Label:      doc.Name,
			File:       doc.File,
			DependsOn:  deps,
			Attributes: attrs,
			baseDigest: digest,
		}
		b.nodes[id] = node
		b.dataSetIndex[doc.Name] = id
	}
	return nil
}

func (b *builder) buildStandaloneComponents() error {
	kinds := []string{"Text", "Table", "ChartStructure", "ChartTime", "Image", "Asset"}
	for _, kind := range kinds {
		docs := b.componentDocs[kind]
		for _, doc := range docs {
			if err := b.ctx.Err(); err != nil {
				return err
			}
			node, err := b.buildComponentNode(kind, doc.Raw, doc.File, doc.Name, doc.Name)
			if err != nil {
				return fmt.Errorf("component %s (%s): %w", doc.Name, kind, err)
			}
			b.nodes[node.ID] = node
			b.standaloneComponentIDs = append(b.standaloneComponentIDs, node.ID)
		}
	}
	return nil
}

func (b *builder) buildLayouts(kind NodeKind, docs []config.Document) error {
	for _, doc := range docs {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		var payload struct {
			Spec layoutSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return fmt.Errorf("%s %s: %w", doc.Kind, doc.Name, err)
		}
		id := makeNodeID(kind, doc.Name)
		node := &Node{
			ID:         id,
			Kind:       kind,
			Name:       doc.Name,
			Label:      doc.Name,
			File:       doc.File,
			Attributes: map[string]string{"componentKind": string(kind)},
			baseDigest: hashBytes(doc.Raw),
		}
		children, err := b.buildLayoutChildren(doc.Name, doc.File, payload.Spec.Children, nil)
		if err != nil {
			return err
		}
		node.DependsOn = append(node.DependsOn, children...)
		b.nodes[id] = node
		b.layoutRootIDs = append(b.layoutRootIDs, id)
	}
	return nil
}

func (b *builder) buildLayoutChildren(parentName, file string, children []layoutChild, path []int) ([]string, error) {
	if len(children) == 0 {
		return nil, nil
	}
	deps := make([]string, 0, len(children))
	for idx, child := range children {
		childPath := append(append([]int(nil), path...), idx)
		id, err := b.buildLayoutChild(parentName, file, child, childPath)
		if err != nil {
			return nil, err
		}
		if id != "" {
			deps = append(deps, id)
		}
	}
	return deps, nil
}

func (b *builder) buildLayoutChild(parentName, file string, child layoutChild, path []int) (string, error) {
	// Resolve ref to get the effective spec (base from referenced doc + overrides from child).
	effectiveSpec, effectiveFile, err := b.resolveChildSpec(parentName, child)
	if err != nil {
		return "", err
	}
	// If ref resolution returned nil (missing ref), skip this child.
	if effectiveSpec == nil {
		return "", nil
	}
	// Use resolved file if available, otherwise fall back to parent file.
	if effectiveFile == "" {
		effectiveFile = file
	}

	switch child.Kind {
	case "LayoutCard":
		var spec layoutSpec
		if err := json.Unmarshal(effectiveSpec, &spec); err != nil {
			var wrapper struct {
				Spec layoutSpec `json:"spec"`
			}
			if errWrap := json.Unmarshal(effectiveSpec, &wrapper); errWrap != nil {
				return "", fmt.Errorf("layout card child: %w", err)
			}
			spec = wrapper.Spec
		}
		name := fmt.Sprintf("%s#%s", parentName, pathKey(path))
		id := makeNodeID(NodeLayoutCard, name)
		node := &Node{
			ID:         id,
			Kind:       NodeLayoutCard,
			Name:       name,
			Label:      fmt.Sprintf("LayoutCard %s", name),
			File:       effectiveFile,
			Attributes: map[string]string{"parent": parentName},
			baseDigest: hashBytes(effectiveSpec),
		}
		children, err := b.buildLayoutChildren(parentName, effectiveFile, spec.Children, path)
		if err != nil {
			return "", err
		}
		node.DependsOn = append(node.DependsOn, children...)
		b.nodes[id] = node
		return id, nil
	case "Text", "Table", "ChartStructure", "ChartTime", "Image":
		label := fmt.Sprintf("%s %s#%s", child.Kind, parentName, pathKey(path))
		node, err := b.buildComponentNode(child.Kind, effectiveSpec, effectiveFile, label, fmt.Sprintf("%s#%s", parentName, pathKey(path)))
		if err != nil {
			return "", err
		}
		node.Attributes["parent"] = parentName
		b.nodes[node.ID] = node
		return node.ID, nil
	default:
		return "", fmt.Errorf("unsupported child kind %q", child.Kind)
	}
}

// resolveChildSpec resolves a layout child's effective spec.
// For inline children (no ref), it returns child.Spec directly.
// For ref children, it looks up the referenced document and merges any spec overrides.
// Returns (nil, "", nil) if optional=true and ref is missing (skip gracefully).
// Returns an error if a required ref is missing or points to LayoutPage (disallowed).
func (b *builder) resolveChildSpec(parentName string, child layoutChild) (json.RawMessage, string, error) {
	// Inline child: no ref, just return spec directly.
	if child.Ref == "" {
		return child.Spec, "", nil
	}

	log := logx.FromContext(b.ctx).Channel("graph")

	// Ref child: look up the referenced document.
	key := child.Kind + ":" + child.Ref
	refDoc, found := b.docIndex[key]
	if !found {
		// Check if they're trying to reference a LayoutPage (explicitly disallowed).
		if lpDoc, lpFound := b.docIndex["LayoutPage:"+child.Ref]; lpFound {
			return nil, "", fmt.Errorf("layout child in %q: ref %q points to LayoutPage %q which cannot be referenced; only Text, Table, ChartStructure, ChartTime, LayoutCard, and Image can be referenced",
				parentName, child.Ref, lpDoc.Name)
		}

		// Ref not found.
		if child.Optional {
			// Optional ref: skip gracefully.
			log.Infof("layout child in %q: optional ref %q of kind %q not found, skipping child", parentName, child.Ref, child.Kind)
			return nil, "", nil
		}

		// Required ref is missing - error.
		return nil, "", fmt.Errorf("layout child in %q: required reference %q of kind %q not found (use optional: true to allow missing refs)", parentName, child.Ref, child.Kind)
	}

	// Extract the spec from the referenced document.
	var refPayload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(refDoc.Raw, &refPayload); err != nil {
		return nil, "", fmt.Errorf("layout child in %q: failed to parse ref %q spec: %w", parentName, child.Ref, err)
	}

	// If no overrides, return the referenced spec directly.
	if len(child.Spec) == 0 || string(child.Spec) == "null" {
		return refPayload.Spec, refDoc.File, nil
	}

	// Merge: referenced spec as base, child.Spec as overrides.
	mergedSpec, err := mergeJSONObjects(refPayload.Spec, child.Spec)
	if err != nil {
		return nil, "", fmt.Errorf("layout child in %q: failed to merge ref %q with overrides: %w", parentName, child.Ref, err)
	}
	return mergedSpec, refDoc.File, nil
}

// mergeJSONObjects performs a deep merge of two JSON objects.
// Values from override replace values in base. Objects are merged recursively.
// Arrays are replaced entirely (not merged element-by-element).
func mergeJSONObjects(base, override json.RawMessage) (json.RawMessage, error) {
	var baseMap map[string]json.RawMessage
	var overrideMap map[string]json.RawMessage

	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, fmt.Errorf("base is not a JSON object: %w", err)
	}
	if err := json.Unmarshal(override, &overrideMap); err != nil {
		return nil, fmt.Errorf("override is not a JSON object: %w", err)
	}

	result := make(map[string]json.RawMessage, len(baseMap)+len(overrideMap))
	for k, v := range baseMap {
		result[k] = v
	}

	for k, overrideVal := range overrideMap {
		baseVal, hasBase := result[k]
		if !hasBase {
			result[k] = overrideVal
			continue
		}

		// Check if both are objects for recursive merge.
		var baseObj map[string]json.RawMessage
		var overrideObj map[string]json.RawMessage
		baseIsObj := json.Unmarshal(baseVal, &baseObj) == nil && baseObj != nil
		overrideIsObj := json.Unmarshal(overrideVal, &overrideObj) == nil && overrideObj != nil

		if baseIsObj && overrideIsObj {
			merged, err := mergeJSONObjects(baseVal, overrideVal)
			if err != nil {
				return nil, err
			}
			result[k] = merged
		} else {
			// Override replaces base (including arrays).
			result[k] = overrideVal
		}
	}

	return json.Marshal(result)
}

func (b *builder) buildComponentNode(kind string, raw json.RawMessage, file, label, name string) (*Node, error) {
	datasets, err := extractDatasets(raw)
	if err != nil {
		return nil, err
	}
	id := makeNodeID(NodeComponent, name)
	node := &Node{
		ID:    id,
		Kind:  NodeComponent,
		Name:  name,
		Label: label,
		File:  file,
		Attributes: map[string]string{
			"componentKind": kind,
		},
		baseDigest: hashBytes(raw),
	}
	if len(datasets) > 0 {
		node.Attributes["dataset"] = strings.Join(datasets, ",")
		var (
			depIDs  []string
			kinds   []string
			missing bool
		)
		for _, dataset := range datasets {
			if targetID, targetKind, ok := b.resolveDataBinding(dataset); ok {
				depIDs = append(depIDs, targetID)
				kinds = append(kinds, string(targetKind))
			} else {
				missing = true
			}
		}
		if len(depIDs) > 0 {
			node.DependsOn = append(node.DependsOn, uniqueStrings(depIDs)...)
		}
		if len(kinds) > 0 {
			node.Attributes["datasetKind"] = strings.Join(uniqueStrings(kinds), ",")
		}
		if missing {
			node.Attributes["datasetMissing"] = "true"
		}
	}
	return node, nil
}

func (b *builder) resolveDataBinding(ref string) (string, NodeKind, bool) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return "", "", false
	}
	if strings.HasPrefix(trimmed, "$") {
		name := strings.TrimPrefix(trimmed, "$")
		target, ok := b.dataSourceIndex[name]
		return target, NodeDataSource, ok
	}
	target, ok := b.dataSetIndex[trimmed]
	return target, NodeDataSet, ok
}

func (b *builder) buildReportArtefacts() error {
	deps := append([]string(nil), b.layoutRootIDs...)
	deps = append(deps, b.standaloneComponentIDs...)
	deps = uniqueStrings(deps)
	sort.Strings(deps)
	for _, doc := range b.artefactDocs {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		id := makeNodeID(NodeReportArtefact, doc.Name)
		node := &Node{
			ID:         id,
			Kind:       NodeReportArtefact,
			Name:       doc.Name,
			Label:      doc.Name,
			File:       doc.File,
			DependsOn:  append([]string(nil), deps...),
			Attributes: map[string]string{},
			baseDigest: hashBytes(doc.Raw),
		}
		b.nodes[id] = node
		b.artefactIDs = append(b.artefactIDs, id)
	}
	return nil
}

func (b *builder) computeHashes() error {
	visited := make(map[string]bool)
	stack := make(map[string]bool)
	for id := range b.nodes {
		if _, err := b.resolveHash(id, visited, stack); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) resolveHash(id string, visited, stack map[string]bool) (string, error) {
	node := b.nodes[id]
	if node == nil {
		return "", fmt.Errorf("graph node %s not found", id)
	}
	if node.Hash != "" {
		return node.Hash, nil
	}
	if stack[id] {
		return "", fmt.Errorf("cycle detected at %s", id)
	}
	stack[id] = true
	childHashes := make([]string, 0, len(node.DependsOn))
	for _, dep := range node.DependsOn {
		depHash, err := b.resolveHash(dep, visited, stack)
		if err != nil {
			return "", err
		}
		childHashes = append(childHashes, depHash)
	}
	sort.Strings(childHashes)
	h := sha256.New()
	h.Write(node.baseDigest)
	for _, depHash := range childHashes {
		h.Write([]byte(depHash))
	}
	node.Hash = hex.EncodeToString(h.Sum(nil))
	stack[id] = false
	visited[id] = true
	return node.Hash, nil
}

func makeNodeID(kind NodeKind, name string) string {
	return fmt.Sprintf("%s:%s", kind, name)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	var result []string
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func pathKey(parts []int) string {
	if len(parts) == 0 {
		return "root"
	}
	sections := make([]string, len(parts))
	for i, p := range parts {
		sections[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(sections, ".")
}
