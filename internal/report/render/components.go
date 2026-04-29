package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/spec"
)

// Data delivery modes for renderDatasources / renderDatasets.
const (
	// DataModeInline embeds gzip+base64 payload bodies directly in the HTML.
	DataModeInline = "inline"
	// DataModeURL emits an HTTP URL body that fetches the JSON from
	// previewhttp.Server. The caller must register payloads on the server.
	DataModeURL = "url"

	// EmittedKindDatasource and EmittedKindDataset match the URL path segment
	// served by previewhttp.Server (/__bino/data/{datasource|dataset}/…) and
	// the value used by previewhttp.PutDatasource / PutDataset.
	EmittedKindDatasource = "datasource"
	EmittedKindDataset    = "dataset"
)

// PluginComponentRenderer renders HTML for plugin-provided component kinds.
type PluginComponentRenderer interface {
	RenderComponent(ctx context.Context, kind, name string, spec []byte, renderMode string) (string, error)
}

// renderCtx holds context needed for rendering layout children with ref support.
type renderCtx struct {
	ctx           context.Context
	docs          []config.Document
	constraintCtx *spec.ConstraintContext
	// docIndex maps kind+name to the document for ref resolution (from filtered docs).
	docIndex map[string]config.Document
	// globalIndex maps kind+name to the document from the unfiltered set.
	// Used to distinguish refs filtered by constraints from refs that don't exist at all.
	globalIndex map[string]config.Document
	// assetURLs maps asset name to resolved URL for asset: image references in markdown.
	assetURLs map[string]string
	// pluginRenderer delegates rendering of plugin component kinds.
	pluginRenderer PluginComponentRenderer
	// renderMode is "build" or "preview".
	renderMode string
}

// newRenderCtx creates a render context with a doc index for ref resolution.
// The allDocs parameter is the unfiltered document set used to distinguish constraint-filtered
// refs from genuinely missing refs. If nil, docs is used (treating all missing refs as errors).
// The assetURLs parameter maps asset names to resolved URLs for asset: image references in markdown.
func newRenderCtx(ctx context.Context, docs []config.Document, constraintCtx *spec.ConstraintContext, allDocs []config.Document, assetURLs map[string]string, pluginRenderer PluginComponentRenderer, renderMode string) *renderCtx {
	rc := &renderCtx{
		ctx:            ctx,
		docs:           docs,
		constraintCtx:  constraintCtx,
		docIndex:       make(map[string]config.Document, len(docs)),
		globalIndex:    make(map[string]config.Document),
		assetURLs:      assetURLs,
		pluginRenderer: pluginRenderer,
		renderMode:     renderMode,
	}
	for _, doc := range docs {
		switch doc.Kind {
		case "LayoutCard", "Text", "Table", "ChartStructure", "ChartTime", "Tree", "Grid", "Image":
			key := doc.Kind + ":" + doc.Name
			rc.docIndex[key] = doc
		}
	}
	// Build globalIndex from allDocs (or fall back to docs if allDocs is nil)
	globalDocs := allDocs
	if globalDocs == nil {
		globalDocs = docs
	}
	for _, doc := range globalDocs {
		switch doc.Kind {
		case "LayoutCard", "Text", "Table", "ChartStructure", "ChartTime", "Tree", "Grid", "Image":
			key := doc.Kind + ":" + doc.Name
			rc.globalIndex[key] = doc
		}
	}
	return rc
}

// collectReferencedDatasources scans component documents for $-prefixed dataset
// references that point directly to datasources. Returns a set of datasource names
// (without the $ prefix) that are referenced by components.
// Both docs (filtered) and globalDocs (unfiltered) are scanned to capture all references.
func collectReferencedDatasources(docs []config.Document, globalDocs []config.Document) map[string]bool {
	refs := make(map[string]bool)
	for _, doc := range docs {
		collectDatasourceRefsFromDoc(doc, refs)
	}
	for _, doc := range globalDocs {
		collectDatasourceRefsFromDoc(doc, refs)
	}
	return refs
}

// collectDatasourceRefsFromDoc extracts $-prefixed dataset references from a single document.
func collectDatasourceRefsFromDoc(doc config.Document, refs map[string]bool) {
	switch doc.Kind {
	case "Text", "Table", "ChartStructure", "ChartTime", "Image":
		var p struct {
			Spec struct {
				Dataset spec.DatasetList `json:"dataset"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &p) == nil {
			addDatasourceRefs(p.Spec.Dataset, refs)
		}
	case "Tree":
		var p struct {
			Spec struct {
				Nodes []struct {
					Spec json.RawMessage `json:"spec"`
				} `json:"nodes"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &p) == nil {
			for _, node := range p.Spec.Nodes {
				extractDatasetFromSpec(node.Spec, refs)
			}
		}
	case "Grid":
		var p struct {
			Spec struct {
				Children []struct {
					Spec json.RawMessage `json:"spec"`
				} `json:"children"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &p) == nil {
			for _, child := range p.Spec.Children {
				extractDatasetFromSpec(child.Spec, refs)
			}
		}
	case "LayoutPage", "LayoutCard":
		var p struct {
			Spec struct {
				Children []json.RawMessage `json:"children"`
			} `json:"spec"`
		}
		if json.Unmarshal(doc.Raw, &p) == nil {
			for _, childRaw := range p.Spec.Children {
				collectDatasourceRefsFromLayoutChild(childRaw, refs)
			}
		}
	}
}

// collectDatasourceRefsFromLayoutChild extracts $-prefixed dataset refs from a layout child's spec.
// Handles nested Tree nodes, Grid children, and LayoutCard children recursively.
func collectDatasourceRefsFromLayoutChild(raw json.RawMessage, refs map[string]bool) {
	var child struct {
		Kind string          `json:"kind"`
		Spec json.RawMessage `json:"spec"`
	}
	if json.Unmarshal(raw, &child) != nil || child.Spec == nil {
		return
	}

	switch child.Kind {
	case "Tree":
		var ts struct {
			Nodes []struct {
				Spec json.RawMessage `json:"spec"`
			} `json:"nodes"`
		}
		if json.Unmarshal(child.Spec, &ts) == nil {
			for _, node := range ts.Nodes {
				extractDatasetFromSpec(node.Spec, refs)
			}
		}
	case "Grid":
		var gs struct {
			Children []struct {
				Kind string          `json:"kind"`
				Spec json.RawMessage `json:"spec"`
			} `json:"children"`
		}
		if json.Unmarshal(child.Spec, &gs) == nil {
			for _, gc := range gs.Children {
				extractDatasetFromSpec(gc.Spec, refs)
				if gc.Kind == "Tree" && gc.Spec != nil {
					var ts struct {
						Nodes []struct {
							Spec json.RawMessage `json:"spec"`
						} `json:"nodes"`
					}
					if json.Unmarshal(gc.Spec, &ts) == nil {
						for _, node := range ts.Nodes {
							extractDatasetFromSpec(node.Spec, refs)
						}
					}
				}
			}
		}
	case "LayoutCard":
		var cs struct {
			Children []json.RawMessage `json:"children"`
		}
		if json.Unmarshal(child.Spec, &cs) == nil {
			for _, grandchild := range cs.Children {
				collectDatasourceRefsFromLayoutChild(grandchild, refs)
			}
		}
	default:
		extractDatasetFromSpec(child.Spec, refs)
	}
}

// extractDatasetFromSpec parses a component spec JSON and adds any $-prefixed dataset refs.
func extractDatasetFromSpec(raw json.RawMessage, refs map[string]bool) {
	if raw == nil {
		return
	}
	var p struct {
		Dataset spec.DatasetList `json:"dataset"`
	}
	if json.Unmarshal(raw, &p) == nil {
		addDatasourceRefs(p.Dataset, refs)
	}
}

// addDatasourceRefs extracts $-prefixed dataset references from a DatasetList
// and adds them (without the $ prefix) to the refs map.
func addDatasourceRefs(dl spec.DatasetList, refs map[string]bool) {
	for _, s := range dl.Strings() {
		if strings.HasPrefix(s, "$") {
			refs[s[1:]] = true
		}
	}
}

// filterDatasourcesByRefs returns only datasource results whose names appear in the referenced set.
// If referenced is empty, returns nil (no datasources needed).
func filterDatasourcesByRefs(results []datasource.Result, referenced map[string]bool) []datasource.Result {
	if len(referenced) == 0 {
		return nil
	}
	var filtered []datasource.Result
	for _, res := range results {
		if referenced[res.Name] {
			filtered = append(filtered, res)
		}
	}
	return filtered
}

// renderDatasources generates bn-datasource elements for collected data.
//
// In DataModeInline, payload bodies are gzip+base64 inlined into the element
// (raw="false" indicates compressed content).
//
// In DataModeURL, the body is a URL pointing at previewhttp.Server (absolute
// when dataBaseURL is non-empty, otherwise a same-origin path starting with
// "/__bino/data/…"). The returned EmittedData entries must be registered on
// the server with PutDatasource(name, hash, body) before the HTML is served.
//
// dataMode "" is treated as DataModeInline.
//
// Duplicate names (which can happen when manifests define multiple
// constraint-gated variants and the caller didn't filter them) collapse to a
// single element — last definition wins, matching DuckDB cache write order.
// In url mode the duplicate would otherwise race in the browser:
// disconnectedCallback on one instance drops the AlaSQL table and aborts the
// in-flight fetch of the other.
func renderDatasources(results []datasource.Result, dataMode, dataBaseURL string) ([]string, []EmittedData) {
	if len(results) == 0 {
		return nil, nil
	}
	results = dedupeDatasourceResultsByName(results)
	useURL := dataMode == DataModeURL
	segments := make([]string, 0, len(results))
	var emitted []EmittedData
	if useURL {
		emitted = make([]EmittedData, 0, len(results))
	}
	for _, res := range results {
		var b strings.Builder
		b.WriteString("<bn-datasource")
		writeAttr(&b, "name", res.Name)
		if useURL {
			b.WriteString(">")
			hash := ContentHash(res.Data)
			b.WriteString(html.EscapeString(buildDataURL(dataBaseURL, EmittedKindDatasource, res.Name, hash)))
			b.WriteString("</bn-datasource>")
			emitted = append(emitted, EmittedData{
				Kind: EmittedKindDatasource,
				Name: res.Name,
				Hash: hash,
				Body: res.Data,
			})
		} else {
			writeAttr(&b, "raw", "false")
			b.WriteString(">")
			compressed, err := CompressContent(res.Data)
			if err != nil {
				// Fall back to plain JSON on compression failure.
				b.WriteString(html.EscapeString(string(res.Data)))
			} else {
				b.WriteString(compressed)
			}
			b.WriteString("</bn-datasource>")
		}
		segments = append(segments, b.String())
	}
	return segments, emitted
}

// renderDatasets generates bn-dataset elements for evaluated DataSet results.
// Each dataset is rendered with static="true" indicating pre-computed data.
// Mode semantics match renderDatasources, including duplicate-name dedup.
func renderDatasets(results []dataset.Result, dataMode, dataBaseURL string) ([]string, []EmittedData) {
	if len(results) == 0 {
		return nil, nil
	}
	results = dedupeDatasetResultsByName(results)
	useURL := dataMode == DataModeURL
	segments := make([]string, 0, len(results))
	var emitted []EmittedData
	if useURL {
		emitted = make([]EmittedData, 0, len(results))
	}
	for _, res := range results {
		var b strings.Builder
		b.WriteString("<bn-dataset")
		writeAttr(&b, "name", res.Name)
		writeAttr(&b, "static", "true")
		if useURL {
			b.WriteString(">")
			hash := ContentHash(res.Data)
			b.WriteString(html.EscapeString(buildDataURL(dataBaseURL, EmittedKindDataset, res.Name, hash)))
			b.WriteString("</bn-dataset>")
			emitted = append(emitted, EmittedData{
				Kind: EmittedKindDataset,
				Name: res.Name,
				Hash: hash,
				Body: res.Data,
			})
		} else {
			writeAttr(&b, "raw", "false")
			b.WriteString(">")
			compressed, err := CompressContent(res.Data)
			if err != nil {
				b.WriteString(html.EscapeString(string(res.Data)))
			} else {
				b.WriteString(compressed)
			}
			b.WriteString("</bn-dataset>")
		}
		segments = append(segments, b.String())
	}
	return segments, emitted
}

// buildDataURL composes the URL the template engine will fetch in url mode.
// When baseURL is empty, returns a same-origin path; otherwise an absolute URL.
// Path segments are escaped to allow names with spaces or special characters.
func buildDataURL(baseURL, kind, name, hash string) string {
	base := strings.TrimRight(baseURL, "/")
	return fmt.Sprintf("%s/__bino/data/%s/%s?hash=%s",
		base, kind, url.PathEscape(name), url.QueryEscape(hash))
}

// dedupeDatasetResultsByName collapses duplicate-name entries to the last
// occurrence, preserving the original ordering of unique names. Same-name
// duplicates can arise when a manifest defines multiple constraint-gated
// DataSet variants and the caller hasn't filtered them — every variant
// reaches dataset.Execute and every result reaches the renderer.
func dedupeDatasetResultsByName(results []dataset.Result) []dataset.Result {
	if len(results) <= 1 {
		return results
	}
	seen := make(map[string]int, len(results))
	out := make([]dataset.Result, 0, len(results))
	for _, r := range results {
		if idx, ok := seen[r.Name]; ok {
			out[idx] = r
			continue
		}
		seen[r.Name] = len(out)
		out = append(out, r)
	}
	return out
}

// dedupeDatasourceResultsByName mirrors dedupeDatasetResultsByName for
// datasource.Result.
func dedupeDatasourceResultsByName(results []datasource.Result) []datasource.Result {
	if len(results) <= 1 {
		return results
	}
	seen := make(map[string]int, len(results))
	out := make([]datasource.Result, 0, len(results))
	for _, r := range results {
		if idx, ok := seen[r.Name]; ok {
			out[idx] = r
			continue
		}
		seen[r.Name] = len(out)
		out = append(out, r)
	}
	return out
}

// renderInternationalizations generates bn-internationalization elements.
func renderInternationalizations(entries []internationalization) []string {
	if len(entries) == 0 {
		return nil
	}
	segments := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.value == "" {
			continue
		}
		var b strings.Builder
		b.WriteString("<bn-internationalization")
		writeAttr(&b, "code", entry.code)
		writeAttr(&b, "namespace", entry.namespace)
		b.WriteString(">")
		b.WriteString(html.EscapeString(entry.value))
		b.WriteString("</bn-internationalization>")
		segments = append(segments, b.String())
	}
	return segments
}

// renderComponentStyles generates bn-component-style elements.
func renderComponentStyles(styles []componentStyle) []string {
	if len(styles) == 0 {
		return nil
	}
	segments := make([]string, 0, len(styles))
	for _, style := range styles {
		if style.value == "" {
			continue
		}
		var b strings.Builder
		b.WriteString("<bn-component-style")
		writeAttr(&b, "name", style.name)
		b.WriteString(">")
		b.WriteString(html.EscapeString(style.value))
		b.WriteString("</bn-component-style>")
		segments = append(segments, b.String())
	}
	return segments
}

// renderScalingGroups generates bn-scaling-group elements.
func renderScalingGroups(groups []scalingGroup) []string {
	if len(groups) == 0 {
		return nil
	}
	segments := make([]string, 0, len(groups))
	for _, group := range groups {
		var b strings.Builder
		b.WriteString("<bn-scaling-group")
		writeAttr(&b, "name", group.name)
		writeAttr(&b, "value", fmt.Sprintf("%g", group.value))
		b.WriteString("></bn-scaling-group>")
		segments = append(segments, b.String())
	}
	return segments
}

// renderAssetComponents generates bn-asset elements for file assets.
func renderAssetComponents(assets []assetComponent) []string {
	if len(assets) == 0 {
		return nil
	}
	segments := make([]string, 0, len(assets))
	for _, asset := range assets {
		if asset.value == "" {
			continue
		}
		var b strings.Builder
		b.WriteString("<bn-asset")
		writeAttr(&b, "name", asset.name)
		b.WriteString(">")
		b.WriteString(html.EscapeString(asset.value))
		b.WriteString("</bn-asset>")
		segments = append(segments, b.String())
	}
	return segments
}

// renderFontLinks generates <link> elements for font assets.
func renderFontLinks(fonts []fontAsset) string {
	if len(fonts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, font := range fonts {
		b.WriteString("  <link rel='stylesheet'")
		writeAttr(&b, "href", font.href)
		if font.mediaType != "" {
			writeAttr(&b, "type", font.mediaType)
		}
		b.WriteString(">")
		b.WriteByte('\n')
	}
	return b.String()
}

// renderLayoutPage renders a LayoutPage document as HTML.
// docName is the metadata.name of the LayoutPage document, used to add a
// data-bino-page attribute for preview identification.
// targetFormat and targetOrientation are the artifact-level defaults used when
// the LayoutPage does not explicitly set pageFormat or pageOrientation.
func renderLayoutPage(raw json.RawMessage, docName string, targetFormat string, targetOrientation string, rc *renderCtx) (htmlOut string, matched bool, err error) {
	var payload struct {
		Spec layoutPageSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, err
	}

	if !layoutPageMatchesFormat(payload.Spec.PageFormat, targetFormat) {
		return "", false, nil
	}

	// Apply artifact-level defaults so the HTML attributes are always present.
	// The template engine CSS requires both page-format and page-orientation
	// to apply correct sizing and @page rules.
	if payload.Spec.PageFormat == "" {
		if targetFormat != "" && isPageLayoutFormat(targetFormat) {
			payload.Spec.PageFormat = targetFormat
		} else {
			payload.Spec.PageFormat = defaultLayoutPageFormat
		}
	}
	if payload.Spec.PageOrientation == "" {
		if targetOrientation != "" {
			payload.Spec.PageOrientation = targetOrientation
		} else {
			payload.Spec.PageOrientation = "landscape"
		}
	}

	htmlOut, err = renderLayoutContainer("bn-layout-page", payload.Spec, docName, rc)
	if err != nil {
		return "", false, err
	}
	return htmlOut, true, nil
}

// renderLayoutContainer renders a layout container (page or card) as HTML.
// docName is written as data-bino-page attribute when non-empty (for preview page identification).
func renderLayoutContainer(tag string, pageSpec layoutPageSpec, docName string, rc *renderCtx) (string, error) {
	var b strings.Builder
	b.WriteString("<")
	b.WriteString(tag)
	if docName != "" {
		writeAttr(&b, "data-bino-page", docName)
	}
	pageSpec.writeAttrs(&b)
	b.WriteString(">\n")

	// Filter children by constraints
	filteredChildren, err := filterChildrenByConstraints(pageSpec.Children, rc)
	if err != nil {
		return "", err
	}

	slotIdx := 0
	for _, child := range filteredChildren {
		childHTML, skip, err := renderLayoutChild(child, rc)
		if err != nil {
			return "", err
		}
		if skip {
			continue
		}
		// Build slot div with source location attributes for click-to-source in preview
		fmt.Fprintf(&b, "  <div slot='slot-%d' style='flex: 1 1 0%%; height: 100%%;'", slotIdx)
		writeSourceAttrs(&b, child)
		b.WriteString(">\n")
		b.WriteString(childHTML)
		b.WriteString("\n  </div>\n")
		slotIdx++
	}
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">")
	return b.String(), nil
}

// renderLayoutCardContainer renders a LayoutCard container as HTML.
func renderLayoutCardContainer(cardSpec layoutCardSpec, rc *renderCtx) (string, error) {
	var b strings.Builder
	b.WriteString("<bn-layout-card")
	cardSpec.writeAttrs(&b)
	b.WriteString(">\n")

	// Filter children by constraints
	filteredChildren, err := filterChildrenByConstraints(cardSpec.Children, rc)
	if err != nil {
		return "", err
	}

	slotIdx := 0
	for _, child := range filteredChildren {
		childHTML, skip, err := renderLayoutChild(child, rc)
		if err != nil {
			return "", err
		}
		if skip {
			continue
		}
		// Build slot div with source location attributes for click-to-source in preview
		fmt.Fprintf(&b, "  <div slot='card-slot-%d' style='flex: 1 1 0%%; height: 100%%;'", slotIdx)
		writeSourceAttrs(&b, child)
		b.WriteString(">\n")
		b.WriteString(childHTML)
		b.WriteString("\n  </div>\n")
		slotIdx++
	}
	b.WriteString("</bn-layout-card>")
	return b.String(), nil
}

// filterChildrenByConstraints filters layout children based on their metadata.constraints.
func filterChildrenByConstraints(children []layoutChild, rc *renderCtx) ([]layoutChild, error) {
	if rc == nil || rc.constraintCtx == nil {
		return children, nil
	}

	result := make([]layoutChild, 0, len(children))
	for _, child := range children {
		// No constraints means always included
		if len(child.Metadata.Constraints) == 0 {
			result = append(result, child)
			continue
		}

		// Parse constraints (supports both string and structured formats)
		constraints, parseErr := spec.ParseMixedConstraints(child.Metadata.Constraints)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid constraint in child %q: %w", child.Metadata.Name, parseErr)
		}

		// Evaluate constraints
		match, err := spec.EvaluateParsedConstraintsWithContext(constraints, rc.constraintCtx, child.Kind, child.Metadata.Name)
		if err != nil {
			return nil, err
		}

		if match {
			result = append(result, child)
		}
	}

	return result, nil
}

// renderLayoutChild renders a child component within a layout.
// Returns (html, skip, error) where skip=true means the child should be skipped (missing ref).
func renderLayoutChild(child layoutChild, rc *renderCtx) (htmlOut string, skip bool, err error) {
	// Resolve ref to get effective spec (base from referenced doc + overrides).
	effectiveSpec, err := resolveChildSpec(child, rc)
	if err != nil {
		return "", false, err
	}
	// If ref resolution returned nil (missing ref), skip this child.
	if effectiveSpec == nil {
		return "", true, nil
	}

	var component string
	switch child.Kind {
	case "Text":
		var s textSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render text child: %w", err)
		}
		component = renderTextComponent(s, rc.assetURLs)
	case "Table":
		var s tableSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render table child: %w", err)
		}
		component = renderTableComponent(s)
	case "ChartStructure":
		var s chartStructureSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render chart structure child: %w", err)
		}
		component = renderChartStructureComponent(s)
	case "ChartTime":
		var s chartTimeSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render chart time child: %w", err)
		}
		component = renderChartTimeComponent(s)
	case "Tree":
		var s treeSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render tree child: %w", err)
		}
		htmlOut, err := renderTreeComponent(s, rc)
		if err != nil {
			return "", false, fmt.Errorf("render tree child: %w", err)
		}
		component = htmlOut
	case "LayoutCard":
		var s layoutCardSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render layout card child: %w", err)
		}
		htmlOut, err := renderLayoutCardContainer(s, rc)
		if err != nil {
			return "", false, err
		}
		component = htmlOut
	case "Grid":
		var s gridSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render grid child: %w", err)
		}
		htmlOut, err := renderGridComponent(s, rc)
		if err != nil {
			return "", false, fmt.Errorf("render grid child: %w", err)
		}
		component = htmlOut
	case "Image":
		var s imageSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render image child: %w", err)
		}
		component = renderImageComponent(s)
	default:
		// Try plugin component renderer for unknown kinds.
		if rc.pluginRenderer != nil {
			htmlFragment, err := rc.pluginRenderer.RenderComponent(rc.ctx, child.Kind, child.Ref, effectiveSpec, rc.renderMode)
			if err != nil {
				return "", false, fmt.Errorf("plugin render %s: %w", child.Kind, err)
			}
			component = htmlFragment
		} else {
			return "", false, fmt.Errorf("unsupported child kind %q", child.Kind)
		}
	}

	return indentBlock(component, 4), false, nil
}

// resolveChildSpec resolves a layout child's effective spec.
// For inline children (no ref), it returns child.Spec directly.
// For ref children, it looks up the referenced document and merges any spec overrides.
// Returns nil without error if the ref is filtered by constraints (skip gracefully).
// Returns nil without error if optional=true and ref is missing (skip gracefully).
// Returns an error if a required ref is genuinely missing (not in unfiltered set).
// Returns an error if the ref points to LayoutPage (disallowed) or has a kind mismatch.
func resolveChildSpec(child layoutChild, rc *renderCtx) (json.RawMessage, error) {
	// Inline child: no ref, just return spec directly.
	if child.Ref == "" {
		return child.Spec, nil
	}

	log := logx.FromContext(rc.ctx).Channel("render")

	// Ref child: look up the referenced document in the filtered set.
	key := child.Kind + ":" + child.Ref
	refDoc, found := rc.docIndex[key]
	if !found {
		// Check if they're trying to reference a LayoutPage (explicitly disallowed).
		for _, doc := range rc.docs {
			if doc.Kind == "LayoutPage" && doc.Name == child.Ref {
				return nil, fmt.Errorf("ref %q points to LayoutPage which cannot be referenced; only Text, Table, ChartStructure, ChartTime, Tree, LayoutCard, and Image can be referenced", child.Ref)
			}
		}

		// Check if the ref exists in the global (unfiltered) set.
		_, existsGlobally := rc.globalIndex[key]
		if existsGlobally {
			// Ref exists but was filtered by constraints - skip gracefully.
			log.Infof("ref %q of kind %q filtered by constraints, skipping child", child.Ref, child.Kind)
			return nil, nil
		}

		// Ref doesn't exist at all.
		if child.Optional {
			// Optional ref: skip gracefully.
			log.Infof("optional ref %q of kind %q not found, skipping child", child.Ref, child.Kind)
			return nil, nil
		}

		// Required ref is genuinely missing - error.
		return nil, fmt.Errorf("required reference %q of kind %q not found (use optional: true to allow missing refs)", child.Ref, child.Kind)
	}

	// Extract the spec from the referenced document.
	var refPayload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(refDoc.Raw, &refPayload); err != nil {
		return nil, fmt.Errorf("failed to parse ref %q spec: %w", child.Ref, err)
	}

	// If no overrides, return the referenced spec directly.
	if len(child.Spec) == 0 || string(child.Spec) == "null" {
		return refPayload.Spec, nil
	}

	// Merge: referenced spec as base, child.Spec as overrides.
	mergedSpec, err := mergeJSONObjects(refPayload.Spec, child.Spec)
	if err != nil {
		return nil, fmt.Errorf("failed to merge ref %q with overrides: %w", child.Ref, err)
	}
	return mergedSpec, nil
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

// renderTextComponent renders a Text component as HTML.
func renderTextComponent(s textSpec, assetURLs map[string]string) string {
	var b strings.Builder
	b.WriteString("<bn-text")
	writeAttr(&b, "value", renderMarkdown(s.Value, assetURLs))
	if value := s.Dataset.Join(","); value != "" {
		writeAttr(&b, "datasets", value)
	}
	writeAttr(&b, "scale", s.Scale.String())
	b.WriteString("></bn-text>")
	return b.String()
}

// renderMarkdown converts a Markdown string to HTML.
// If the input contains no Markdown syntax, the output is the text
// wrapped in a <p> tag by goldmark.
// When assetURLs is non-nil, asset: image references are resolved.
func renderMarkdown(s string, assetURLs map[string]string) string {
	if s == "" {
		return ""
	}
	opts := []goldmark.Option{
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()),
	}
	if len(assetURLs) > 0 {
		opts = append(opts, goldmark.WithExtensions(NewAssetExtension(assetURLs)))
	}
	md := goldmark.New(opts...)
	var buf bytes.Buffer
	if err := md.Convert([]byte(s), &buf); err != nil {
		return s
	}
	return strings.TrimSpace(buf.String())
}

// renderChartStructureComponent renders a ChartStructure component as HTML.
func renderChartStructureComponent(s chartStructureSpec) string {
	var b strings.Builder
	b.WriteString("<bn-chart-structure")
	s.writeAttrs(&b)
	b.WriteString("></bn-chart-structure>")
	return b.String()
}

// renderChartTimeComponent renders a ChartTime component as HTML.
func renderChartTimeComponent(s chartTimeSpec) string {
	var b strings.Builder
	b.WriteString("<bn-chart-time")
	s.writeAttrs(&b)
	b.WriteString("></bn-chart-time>")
	return b.String()
}

// renderTreeComponent renders a Tree component as HTML.
// Tree charts use slotted content for nodes, so we render node slots inside the element.
// Each node can contain a Label, Table, ChartStructure, or ChartTime component.
func renderTreeComponent(s treeSpec, rc *renderCtx) (string, error) {
	var b strings.Builder
	b.WriteString("<bn-tree")
	s.writeAttrs(&b)
	b.WriteString(">")

	// Render node content as slotted elements
	for _, node := range s.Nodes {
		nodeContent, err := renderTreeNode(node, rc)
		if err != nil {
			return "", fmt.Errorf("render tree node %q: %w", node.ID, err)
		}
		if nodeContent == "" {
			continue // Skip nodes that couldn't be rendered (e.g., filtered refs)
		}
		b.WriteString("\n  <div slot='")
		b.WriteString(html.EscapeString(node.ID))
		b.WriteString("'>")
		b.WriteString(nodeContent)
		b.WriteString("</div>")
	}
	if len(s.Nodes) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("</bn-tree>")
	return b.String(), nil
}

// renderTreeNode renders a single node in a tree chart.
// It handles Label, Table, ChartStructure, ChartTime, and Image kinds with ref or inline spec.
func renderTreeNode(node treeNode, rc *renderCtx) (string, error) {
	// Resolve spec (handle ref if present)
	effectiveSpec, err := resolveTreeNodeSpec(node, rc)
	if err != nil {
		return "", err
	}
	if effectiveSpec == nil {
		return "", nil // Ref was filtered, skip this node
	}

	switch node.Kind {
	case "Label":
		var s treeLabelSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal label spec: %w", err)
		}
		return renderTreeLabelComponent(s), nil
	case "Table":
		var s tableSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal table spec: %w", err)
		}
		return renderTableComponent(s), nil
	case "ChartStructure":
		var s chartStructureSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal chart structure spec: %w", err)
		}
		return renderChartStructureComponent(s), nil
	case "ChartTime":
		var s chartTimeSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal chart time spec: %w", err)
		}
		return renderChartTimeComponent(s), nil
	case "Image":
		var s imageSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal image spec: %w", err)
		}
		return renderImageComponent(s), nil
	default:
		return "", fmt.Errorf("unsupported tree node kind %q", node.Kind)
	}
}

// resolveTreeNodeSpec resolves the effective spec for a tree node.
// For inline nodes (no ref), returns node.Spec directly.
// For ref nodes, looks up the referenced document and merges any spec overrides.
func resolveTreeNodeSpec(node treeNode, rc *renderCtx) (json.RawMessage, error) {
	// Label kind doesn't support refs (inline only)
	if node.Kind == "Label" {
		return node.Spec, nil
	}

	// Inline node: no ref, just return spec directly
	if node.Ref == "" {
		return node.Spec, nil
	}

	// Ref node: look up the referenced document
	if rc == nil {
		return nil, fmt.Errorf("ref %q cannot be resolved without render context", node.Ref)
	}

	key := node.Kind + ":" + node.Ref
	refDoc, found := rc.docIndex[key]
	if !found {
		// Check if ref exists in global set (filtered by constraints)
		_, existsGlobally := rc.globalIndex[key]
		if existsGlobally {
			return nil, nil // Filtered by constraints, skip
		}
		return nil, fmt.Errorf("reference %q of kind %q not found", node.Ref, node.Kind)
	}

	// Extract spec from referenced document
	var refPayload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(refDoc.Raw, &refPayload); err != nil {
		return nil, fmt.Errorf("parse ref %q spec: %w", node.Ref, err)
	}

	// If no overrides, return referenced spec directly
	if len(node.Spec) == 0 || string(node.Spec) == "null" {
		return refPayload.Spec, nil
	}

	// Merge: referenced spec as base, node.Spec as overrides
	return mergeJSONObjects(refPayload.Spec, node.Spec)
}

// renderTreeLabelComponent renders a Label component for tree nodes.
func renderTreeLabelComponent(s treeLabelSpec) string {
	var b strings.Builder
	b.WriteString("<bn-text")
	writeAttr(&b, "value", s.Value)
	if value := s.Dataset.Join(","); value != "" {
		writeAttr(&b, "datasets", value)
	}
	writeAttr(&b, "scale", s.Scale.String())
	b.WriteString("></bn-text>")
	return b.String()
}

// renderTableComponent renders a Table component as HTML.
func renderTableComponent(s tableSpec) string {
	var b strings.Builder
	b.WriteString("<bn-table")
	s.writeAttrs(&b)
	b.WriteString("></bn-table>")
	return b.String()
}

// renderImageComponent renders an Image component as HTML.
func renderImageComponent(s imageSpec) string {
	var b strings.Builder
	b.WriteString("<bn-image")
	s.writeAttrs(&b)
	b.WriteString("></bn-image>")
	return b.String()
}

// renderGridComponent renders a Grid component as HTML.
// Grid uses slotted content for children, with slot names following the pattern "{row-id}-{column-id}".
func renderGridComponent(s gridSpec, rc *renderCtx) (string, error) {
	var b strings.Builder
	b.WriteString("<bn-grid")
	s.writeAttrs(&b)
	b.WriteString(">")

	// Render child content as slotted elements
	for _, child := range s.Children {
		childContent, err := renderGridChild(child, rc)
		if err != nil {
			return "", fmt.Errorf("render grid child %s-%s: %w", child.Row, child.Column, err)
		}
		if childContent == "" {
			continue // Skip children that couldn't be rendered (e.g., filtered refs)
		}
		slotName := child.Row.String() + "-" + child.Column.String()
		b.WriteString("\n  <div slot='")
		b.WriteString(html.EscapeString(slotName))
		b.WriteString("'>")
		b.WriteString(childContent)
		b.WriteString("</div>")
	}
	if len(s.Children) > 0 {
		b.WriteString("\n")
	}
	b.WriteString("</bn-grid>")
	return b.String(), nil
}

// renderGridChild renders a single child (cell) in a grid.
// It handles Text, Table, ChartStructure, ChartTime, Tree, and Image kinds with ref or inline spec.
func renderGridChild(child gridChild, rc *renderCtx) (string, error) {
	// Resolve spec (handle ref if present)
	effectiveSpec, err := resolveGridChildSpec(child, rc)
	if err != nil {
		return "", err
	}
	if effectiveSpec == nil {
		return "", nil // Ref was filtered or optional ref missing, skip this child
	}

	switch child.Kind {
	case "Text":
		var s textSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal text spec: %w", err)
		}
		return renderTextComponent(s, rc.assetURLs), nil
	case "Table":
		var s tableSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal table spec: %w", err)
		}
		return renderTableComponent(s), nil
	case "ChartStructure":
		var s chartStructureSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal chart structure spec: %w", err)
		}
		return renderChartStructureComponent(s), nil
	case "ChartTime":
		var s chartTimeSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal chart time spec: %w", err)
		}
		return renderChartTimeComponent(s), nil
	case "Tree":
		var s treeSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal tree spec: %w", err)
		}
		return renderTreeComponent(s, rc)
	case "Image":
		var s imageSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", fmt.Errorf("unmarshal image spec: %w", err)
		}
		return renderImageComponent(s), nil
	default:
		return "", fmt.Errorf("unsupported grid child kind %q", child.Kind)
	}
}

// resolveGridChildSpec resolves the effective spec for a grid child.
// For inline children (no ref), returns child.Spec directly.
// For ref children, looks up the referenced document and merges any spec overrides.
func resolveGridChildSpec(child gridChild, rc *renderCtx) (json.RawMessage, error) {
	// Inline child: no ref, just return spec directly
	if child.Ref == "" {
		return child.Spec, nil
	}

	// Ref child: look up the referenced document
	if rc == nil {
		return nil, fmt.Errorf("ref %q cannot be resolved without render context", child.Ref)
	}

	log := logx.FromContext(rc.ctx).Channel("render")

	key := child.Kind + ":" + child.Ref
	refDoc, found := rc.docIndex[key]
	if !found {
		// Check if ref exists in global set (filtered by constraints)
		_, existsGlobally := rc.globalIndex[key]
		if existsGlobally {
			log.Infof("ref %q of kind %q filtered by constraints, skipping grid child", child.Ref, child.Kind)
			return nil, nil // Filtered by constraints, skip
		}

		// Ref doesn't exist at all
		if child.Optional {
			log.Infof("optional ref %q of kind %q not found, skipping grid child", child.Ref, child.Kind)
			return nil, nil // Optional ref: skip gracefully
		}

		return nil, fmt.Errorf("required reference %q of kind %q not found (use optional: true to allow missing refs)", child.Ref, child.Kind)
	}

	// Extract spec from referenced document
	var refPayload struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(refDoc.Raw, &refPayload); err != nil {
		return nil, fmt.Errorf("parse ref %q spec: %w", child.Ref, err)
	}

	// If no overrides, return referenced spec directly
	if len(child.Spec) == 0 || string(child.Spec) == "null" {
		return refPayload.Spec, nil
	}

	// Merge: referenced spec as base, child.Spec as overrides
	return mergeJSONObjects(refPayload.Spec, child.Spec)
}

// writeAttr writes an HTML attribute if the value is non-empty.
func writeAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString("='")
	b.WriteString(html.EscapeString(value))
	b.WriteString("'")
}

// writeSourceAttrs writes data attributes for click-to-source functionality in preview.
// These attributes allow the VS Code extension to navigate to the YAML source when
// the user clicks on a component in the preview.
// It also writes an id attribute for screenshot targeting via ScreenshotArtefact.
func writeSourceAttrs(b *strings.Builder, child layoutChild) {
	writeAttr(b, "data-bino-kind", child.Kind)
	// For ref children, use the ref as the name (points to standalone document).
	// For inline children, use the metadata name.
	if child.Ref != "" {
		writeAttr(b, "data-bino-ref", child.Ref)
		writeAttr(b, "id", "bino-"+strings.ToLower(child.Kind)+"-"+child.Ref)
	}
	if child.Metadata.Name != "" {
		writeAttr(b, "data-bino-name", child.Metadata.Name)
		if child.Ref == "" {
			writeAttr(b, "id", "bino-"+strings.ToLower(child.Kind)+"-"+child.Metadata.Name)
		}
	}
}

// indentBlock indents each line of a string by the specified number of spaces.
func indentBlock(s string, spaces int) string {
	if s == "" {
		return s
	}
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = prefix
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// knownPageFormats lists the page layout formats supported by the template engine.
// Values like "pdf" are output formats, not page layout formats, and must not
// be used to filter or default LayoutPage page-format attributes.
var knownPageFormats = map[string]bool{
	"xga": true, "hd": true, "full-hd": true, "4k": true, "4k2k": true,
	"a4": true, "a3": true, "a2": true, "a1": true, "a0": true,
	"letter": true, "legal": true,
}

// isPageLayoutFormat reports whether format is a recognized page layout format.
func isPageLayoutFormat(format string) bool {
	return knownPageFormats[strings.ToLower(strings.TrimSpace(format))]
}

// layoutPageMatchesFormat checks if a page format matches the target format.
// If targetFormat is not a recognized page layout format (e.g. "pdf"), the
// page is always included because non-layout formats cannot meaningfully
// filter pages.
func layoutPageMatchesFormat(pageFormat, targetFormat string) bool {
	target := strings.TrimSpace(targetFormat)
	if target == "" || !isPageLayoutFormat(target) {
		return true
	}
	format := strings.TrimSpace(pageFormat)
	if format == "" {
		format = defaultLayoutPageFormat
	}
	return strings.EqualFold(format, target)
}

// ComponentFromSpec renders a component HTML from its kind and spec JSON.
// This is an exported function that can be used by other packages (e.g., markdown)
// to render components consistently without duplicating spec types.
// Supported kinds: Text, Table, ChartStructure, ChartTime, Image.
// The assetURLs parameter is optional and used to resolve asset: image references in Text markdown.
func ComponentFromSpec(kind string, specRaw json.RawMessage, assetURLs map[string]string) (string, error) {
	switch kind {
	case "Text":
		var s textSpec
		if err := json.Unmarshal(specRaw, &s); err != nil {
			return "", fmt.Errorf("parse text spec: %w", err)
		}
		return renderTextComponent(s, assetURLs), nil
	case "Table":
		var s tableSpec
		if err := json.Unmarshal(specRaw, &s); err != nil {
			return "", fmt.Errorf("parse table spec: %w", err)
		}
		return renderTableComponent(s), nil
	case "ChartStructure":
		var s chartStructureSpec
		if err := json.Unmarshal(specRaw, &s); err != nil {
			return "", fmt.Errorf("parse chart structure spec: %w", err)
		}
		return renderChartStructureComponent(s), nil
	case "ChartTime":
		var s chartTimeSpec
		if err := json.Unmarshal(specRaw, &s); err != nil {
			return "", fmt.Errorf("parse chart time spec: %w", err)
		}
		return renderChartTimeComponent(s), nil
	case "Image":
		var s imageSpec
		if err := json.Unmarshal(specRaw, &s); err != nil {
			return "", fmt.Errorf("parse image spec: %w", err)
		}
		return renderImageComponent(s), nil
	default:
		return "", fmt.Errorf("unsupported component kind %q for inline rendering", kind)
	}
}
