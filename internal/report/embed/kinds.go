// Package embed is the single source of truth for which manifest kinds render
// standalone as a component — i.e. can be shown on their own (the preview's
// /__embedding endpoint, the designer's live canvas, the bino://kinds
// `embeddable` flag). Keeping the set here, free of render/pipeline imports,
// lets both the preview rebuilder and the MCP server read one authority instead
// of maintaining divergent copies.
package embed

// componentKinds are the standalone component manifest kinds: kinds that can be
// rendered on their own (the preview wraps them in a synthetic single-child
// LayoutPage). These are the kinds for which bino://kinds reports
// `embeddable: true`.
//
// Asset/Image: "Image" is a layout-child kind, not a manifest kind, so it never
// names a top-level document and is intentionally absent. "Asset" is a resource
// (image/font/file bytes referenced by an Image child), not a component that
// renders standalone, so it is not embeddable here.
var componentKinds = map[string]struct{}{
	"Table":          {},
	"ChartStructure": {},
	"ChartTime":      {},
	"Text":           {},
	"Tree":           {},
	"Grid":           {},
}

// IsEmbeddable reports whether a manifest kind renders standalone as a
// component.
func IsEmbeddable(kind string) bool {
	_, ok := componentKinds[kind]
	return ok
}

// ComponentKinds returns the standalone component manifest kinds in no
// particular order. Callers that need a stable order must sort the result.
func ComponentKinds() []string {
	out := make([]string, 0, len(componentKinds))
	for k := range componentKinds {
		out = append(out, k)
	}
	return out
}
