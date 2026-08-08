// Package embed is the single source of truth for which manifest kinds render
// standalone as a component — i.e. can be shown on their own (the preview's
// /__embedding endpoint, the designer's live canvas, the bino://kinds
// `embeddable` flag) — and for each built-in kind's capability category. Keeping
// these here, free of render/pipeline/plugin imports, lets the preview
// rebuilder, the MCP server, the daemon `/kinds` endpoint, and `lsp-helper
// kinds` all read one authority instead of maintaining divergent copies. The
// plugin-kind fallback is layered on by each caller (which already holds the
// registry) so this package stays a pure leaf.
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
	"ChartScatter":   {},
	"ChartBubble":    {},
	"ChartBullet":    {},
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

// builtinCategory maps each built-in manifest kind to its capability category
// (data / layout / embeddable / artefact / config). This is the authority that
// the MCP server, the daemon `/kinds` endpoint, and `lsp-helper kinds` all read,
// so the categories never drift between the agent and GUI surfaces.
var builtinCategory = map[string]string{
	"DataSource":           "data",
	"DataSet":              "data",
	"ConnectionSecret":     "data",
	"LayoutPage":           "layout",
	"LayoutCard":           "layout",
	"Text":                 "embeddable",
	"Table":                "embeddable",
	"ChartStructure":       "embeddable",
	"ChartTime":            "embeddable",
	"ChartScatter":         "embeddable",
	"ChartBubble":          "embeddable",
	"ChartBullet":          "embeddable",
	"Tree":                 "embeddable",
	"Grid":                 "embeddable",
	"Asset":                "embeddable",
	"ReportArtefact":       "artefact",
	"LiveReportArtefact":   "artefact",
	"ScreenshotArtefact":   "artefact",
	"DocumentArtefact":     "artefact",
	"ComponentStyle":       "config",
	"RuleSet":              "config",
	"Internationalization": "config",
	"ScalingGroup":         "config",
	"SigningProfile":       "config",
}

// BuiltinCategory returns the capability category for a built-in manifest kind
// and whether the kind is known. Callers layer their own plugin-kind fallback on
// a false result (using the plugin registry they already hold), which keeps this
// package free of a plugin import.
func BuiltinCategory(kind string) (string, bool) {
	c, ok := builtinCategory[kind]
	return c, ok
}

// AllBuiltinKinds returns every built-in manifest kind (the keys of the
// category registry) in no particular order. Callers that need a stable order
// must sort the result.
func AllBuiltinKinds() []string {
	out := make([]string, 0, len(builtinCategory))
	for k := range builtinCategory {
		out = append(out, k)
	}
	return out
}
