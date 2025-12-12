package render

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"

	"bino.bi/bino/internal/report/dataset"
	"bino.bi/bino/internal/report/datasource"
	"bino.bi/bino/internal/report/spec"
)

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
func renderLayoutPage(raw json.RawMessage, targetFormat string, constraintCtx *spec.ConstraintContext) (string, bool, error) {
	var payload struct {
		Spec layoutPageSpec `json:"spec"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, err
	}

	if !layoutPageMatchesFormat(payload.Spec.PageFormat, targetFormat) {
		return "", false, nil
	}

	html, err := renderLayoutContainer("bn-layout-page", payload.Spec, constraintCtx)
	if err != nil {
		return "", false, err
	}
	return html, true, nil
}

// renderLayoutContainer renders a layout container (page or card) as HTML.
func renderLayoutContainer(tag string, spec layoutPageSpec, constraintCtx *spec.ConstraintContext) (string, error) {
	var b strings.Builder
	b.WriteString("<")
	b.WriteString(tag)
	spec.writeAttrs(&b)
	b.WriteString(">\n")

	// Filter children by constraints
	filteredChildren, err := filterChildrenByConstraints(spec.Children, constraintCtx)
	if err != nil {
		return "", err
	}

	for idx, child := range filteredChildren {
		childHTML, err := renderLayoutChild(child, constraintCtx)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  <div slot=\"slot-%d\" style=\"flex: 1 1 0%%; height: 100%%;\">\n", idx))
		b.WriteString(childHTML)
		b.WriteString("\n  </div>\n")
	}
	b.WriteString("</")
	b.WriteString(tag)
	b.WriteString(">")
	return b.String(), nil
}

// renderLayoutCardContainer renders a LayoutCard container as HTML.
func renderLayoutCardContainer(cardSpec layoutCardSpec, constraintCtx *spec.ConstraintContext) (string, error) {
	var b strings.Builder
	b.WriteString("<bn-layout-card")
	cardSpec.writeAttrs(&b)
	b.WriteString(">\n")

	// Filter children by constraints
	filteredChildren, err := filterChildrenByConstraints(cardSpec.Children, constraintCtx)
	if err != nil {
		return "", err
	}

	for idx, child := range filteredChildren {
		childHTML, err := renderLayoutChild(child, constraintCtx)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  <div slot=\"card-slot-%d\" style=\"flex: 1 1 0%%; height: 100%%;\">\n", idx))
		b.WriteString(childHTML)
		b.WriteString("\n  </div>\n")
	}
	b.WriteString("</bn-layout-card>")
	return b.String(), nil
}

// filterChildrenByConstraints filters layout children based on their metadata.constraints.
func filterChildrenByConstraints(children []layoutChild, constraintCtx *spec.ConstraintContext) ([]layoutChild, error) {
	if constraintCtx == nil {
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
		match, err := spec.EvaluateConstraintsWithContext(child.Metadata.Constraints, constraintCtx, child.Kind, child.Metadata.Name)
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
func renderLayoutChild(child layoutChild, constraintCtx *spec.ConstraintContext) (string, error) {
	var component string
	switch child.Kind {
	case "Text":
		var spec textSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render text child: %w", err)
		}
		component = renderTextComponent(spec)
	case "Table":
		var spec tableSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render table child: %w", err)
		}
		component = renderTableComponent(spec)
	case "ChartStructure":
		var spec chartStructureSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render chart structure child: %w", err)
		}
		component = renderChartStructureComponent(spec)
	case "ChartTime":
		var spec chartTimeSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render chart time child: %w", err)
		}
		component = renderChartTimeComponent(spec)
	case "LayoutCard":
		var spec layoutCardSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render layout card child: %w", err)
		}
		html, err := renderLayoutCardContainer(spec, constraintCtx)
		if err != nil {
			return "", err
		}
		component = html
	case "Image":
		var spec imageSpec
		if err := json.Unmarshal(child.Spec, &spec); err != nil {
			return "", fmt.Errorf("render image child: %w", err)
		}
		component = renderImageComponent(spec)
	default:
		return "", fmt.Errorf("unsupported child kind %q", child.Kind)
	}

	return indentBlock(component, 4), nil
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
