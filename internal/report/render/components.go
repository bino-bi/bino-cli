package render

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"bino.bi/bino/internal/logx"
	"bino.bi/bino/internal/report/config"
	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/spec"
)

// renderCtx holds context needed for rendering layout children with ref support.
type renderCtx struct {
	ctx           context.Context
	docs          []config.Document
	constraintCtx *spec.ConstraintContext
	// docIndex maps kind+name to the document for ref resolution.
	docIndex map[string]config.Document
}

// newRenderCtx creates a render context with a doc index for ref resolution.
func newRenderCtx(ctx context.Context, docs []config.Document, constraintCtx *spec.ConstraintContext) *renderCtx {
	rc := &renderCtx{
		ctx:           ctx,
		docs:          docs,
		constraintCtx: constraintCtx,
		docIndex:      make(map[string]config.Document, len(docs)),
	}
	for _, doc := range docs {
		switch doc.Kind {
		case "LayoutCard", "Text", "Table", "ChartStructure", "ChartTime", "Image":
			key := doc.Kind + ":" + doc.Name
			rc.docIndex[key] = doc
		}
	}
	return rc
}

// renderDatasources generates bn-datasource elements for collected data.
func renderDatasources(results []datasource.Result) []string {
	if len(results) == 0 {
		return nil
	}
	segments := make([]string, 0, len(results))
	for _, res := range results {
		var b strings.Builder
		b.WriteString("<bn-datasource")
		writeAttr(&b, "name", res.Name)
		b.WriteString(">")
		b.WriteString(html.EscapeString(string(res.Data)))
		b.WriteString("</bn-datasource>")
		segments = append(segments, b.String())
	}
	return segments
}

// renderDatasets generates bn-dataset elements for evaluated DataSet results.
// Each dataset is rendered with static="true" indicating pre-computed data.
func renderDatasets(results []dataset.Result) []string {
	if len(results) == 0 {
		return nil
	}
	segments := make([]string, 0, len(results))
	for _, res := range results {
		var b strings.Builder
		b.WriteString("<bn-dataset")
		writeAttr(&b, "name", res.Name)
		writeAttr(&b, "static", "true")
		b.WriteString(">")
		b.WriteString(html.EscapeString(string(res.Data)))
		b.WriteString("</bn-dataset>")
		segments = append(segments, b.String())
	}
	return segments
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
		b.WriteString("  <link rel=\"stylesheet\"")
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
func renderLayoutPage(raw json.RawMessage, targetFormat string, rc *renderCtx) (string, bool, error) {
	var payload struct {
		Spec layoutPageSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, err
	}

	if !layoutPageMatchesFormat(payload.Spec.PageFormat, targetFormat) {
		return "", false, nil
	}

	html, err := renderLayoutContainer("bn-layout-page", payload.Spec, rc)
	if err != nil {
		return "", false, err
	}
	return html, true, nil
}

// renderLayoutContainer renders a layout container (page or card) as HTML.
func renderLayoutContainer(tag string, pageSpec layoutPageSpec, rc *renderCtx) (string, error) {
	var b strings.Builder
	b.WriteString("<")
	b.WriteString(tag)
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
		b.WriteString(fmt.Sprintf("  <div slot=\"slot-%d\" style=\"flex: 1 1 0%%; height: 100%%;\"", slotIdx))
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
		b.WriteString(fmt.Sprintf("  <div slot=\"card-slot-%d\" style=\"flex: 1 1 0%%; height: 100%%;\"", slotIdx))
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

		// Evaluate constraints
		match, err := spec.EvaluateConstraintsWithContext(child.Metadata.Constraints, rc.constraintCtx, child.Kind, child.Metadata.Name)
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
func renderLayoutChild(child layoutChild, rc *renderCtx) (string, bool, error) {
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
		component = renderTextComponent(s)
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
	case "LayoutCard":
		var s layoutCardSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render layout card child: %w", err)
		}
		html, err := renderLayoutCardContainer(s, rc)
		if err != nil {
			return "", false, err
		}
		component = html
	case "Image":
		var s imageSpec
		if err := json.Unmarshal(effectiveSpec, &s); err != nil {
			return "", false, fmt.Errorf("render image child: %w", err)
		}
		component = renderImageComponent(s)
	default:
		return "", false, fmt.Errorf("unsupported child kind %q", child.Kind)
	}

	return indentBlock(component, 4), false, nil
}

// resolveChildSpec resolves a layout child's effective spec.
// For inline children (no ref), it returns child.Spec directly.
// For ref children, it looks up the referenced document and merges any spec overrides.
// Returns nil if the ref is missing (skip with warning).
// Returns an error if the ref points to LayoutPage (disallowed) or has a kind mismatch.
func resolveChildSpec(child layoutChild, rc *renderCtx) (json.RawMessage, error) {
	// Inline child: no ref, just return spec directly.
	if child.Ref == "" {
		return child.Spec, nil
	}

	// Ref child: look up the referenced document.
	key := child.Kind + ":" + child.Ref
	refDoc, found := rc.docIndex[key]
	if !found {
		// Check if they're trying to reference a LayoutPage (explicitly disallowed).
		for _, doc := range rc.docs {
			if doc.Kind == "LayoutPage" && doc.Name == child.Ref {
				return nil, fmt.Errorf("ref %q points to LayoutPage which cannot be referenced; only Text, Table, ChartStructure, ChartTime, LayoutCard, and Image can be referenced", child.Ref)
			}
		}
		// Missing ref: warn and skip.
		log := logx.FromContext(rc.ctx).Channel("render")
		log.Warnf("ref %q of kind %q not found, skipping child", child.Ref, child.Kind)
		return nil, nil
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
func renderTextComponent(spec textSpec) string {
	var b strings.Builder
	b.WriteString("<bn-text")
	writeAttr(&b, "value", spec.Value)
	if value := spec.Dataset.Join(","); value != "" {
		writeAttr(&b, "datasets", value)
	}
	b.WriteString("></bn-text>")
	return b.String()
}

// renderChartStructureComponent renders a ChartStructure component as HTML.
func renderChartStructureComponent(spec chartStructureSpec) string {
	var b strings.Builder
	b.WriteString("<bn-chart-structure")
	spec.writeAttrs(&b)
	b.WriteString("></bn-chart-structure>")
	return b.String()
}

// renderChartTimeComponent renders a ChartTime component as HTML.
func renderChartTimeComponent(spec chartTimeSpec) string {
	var b strings.Builder
	b.WriteString("<bn-chart-time")
	spec.writeAttrs(&b)
	b.WriteString("></bn-chart-time>")
	return b.String()
}

// renderTableComponent renders a Table component as HTML.
func renderTableComponent(spec tableSpec) string {
	var b strings.Builder
	b.WriteString("<bn-table")
	spec.writeAttrs(&b)
	b.WriteString("></bn-table>")
	return b.String()
}

// renderImageComponent renders an Image component as HTML.
func renderImageComponent(spec imageSpec) string {
	var b strings.Builder
	b.WriteString("<bn-image")
	spec.writeAttrs(&b)
	b.WriteString("></bn-image>")
	return b.String()
}

// writeAttr writes an HTML attribute if the value is non-empty.
func writeAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString("=\"")
	b.WriteString(html.EscapeString(value))
	b.WriteString("\"")
}

// writeSourceAttrs writes data attributes for click-to-source functionality in preview.
// These attributes allow the VS Code extension to navigate to the YAML source when
// the user clicks on a component in the preview.
func writeSourceAttrs(b *strings.Builder, child layoutChild) {
	writeAttr(b, "data-bino-kind", child.Kind)
	// For ref children, use the ref as the name (points to standalone document).
	// For inline children, use the metadata name.
	if child.Ref != "" {
		writeAttr(b, "data-bino-ref", child.Ref)
	}
	if child.Metadata.Name != "" {
		writeAttr(b, "data-bino-name", child.Metadata.Name)
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

// layoutPageMatchesFormat checks if a page format matches the target format.
func layoutPageMatchesFormat(pageFormat, targetFormat string) bool {
	if strings.TrimSpace(targetFormat) == "" {
		return true
	}
	format := strings.TrimSpace(pageFormat)
	if format == "" {
		format = defaultLayoutPageFormat
	}
	return strings.EqualFold(format, targetFormat)
}
