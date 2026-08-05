// Package layoutstate decodes the template engine's getLayoutState() snapshot
// and derives render-time findings from it.
//
// The engine exposes getLayoutState() on bn-context (aggregating every visual
// descendant) since v1.0.0-next.24. It reports what actually reached the
// screen: box geometry, resolved auto-scaling, render metadata and the
// components' has-error diagnostics. Three classes of defect are invisible
// without it — a component that rendered empty, a silently auto-fitted scale,
// and overflow without a magnitude — so the checks here run over a snapshot
// rather than over the manifests.
//
// The same analysis backs the preview inspector (via POST /__bino/layout-state),
// the build warnings and the MCP tooling, so all three agree by construction.
package layoutstate

// Version is the snapshot schema this package understands. The engine bumps
// LAYOUT_STATE_VERSION only for breaking shape changes; additive changes keep
// it at 1.
const Version = 1

// Snapshot is the capture envelope posted by the browser: the engine's raw
// state plus the source identity only the CLI can supply.
//
// The engine's ComponentMetadata declares measureUnit/measureScale but no
// component populates them, and the engine's generated ids ("bn-table[0]")
// carry no link back to the YAML. Both gaps are filled from the DOM by the
// capture snippet, which reads the data-bino-* attributes and measure-*
// attributes this CLI's renderer writes.
type Snapshot struct {
	State   State             `json:"state"`
	Sources map[string]Source `json:"sources,omitempty"`
}

// Source is the CLI-side identity of one component, keyed by the engine's
// component id in Snapshot.Sources.
type Source struct {
	// Kind, Name and Ref come from the data-bino-* attributes written by
	// writeSourceAttrs, so a finding can name the manifest document.
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
	Ref  string `json:"ref,omitempty"`

	// MeasureUnit and MeasureScale come from the rendered measure-unit /
	// measure-scale attributes. They are the grouping key for comparing
	// resolved scales between components.
	MeasureUnit  string `json:"measureUnit,omitempty"`
	MeasureScale string `json:"measureScale,omitempty"`
}

// Label returns a human-readable identifier for the component, preferring the
// manifest identity over the engine's generated id.
func (s Source) Label(componentID string) string {
	name := s.Name
	if name == "" {
		name = s.Ref
	}
	switch {
	case s.Kind != "" && name != "":
		return s.Kind + " " + name
	case name != "":
		return name
	case s.Kind != "":
		return s.Kind
	default:
		return componentID
	}
}

// State mirrors the engine's LayoutState.
//
// Element-level detail (the elements and table fields of a component) is
// deliberately not decoded: no check needs it, and a full-detail snapshot of a
// large table runs to megabytes. Callers that persist a snapshot write the raw
// bytes instead.
type State struct {
	Version     int         `json:"version"`
	GeneratedAt string      `json:"generatedAt,omitempty"`
	Detail      string      `json:"detail,omitempty"`
	Context     Context     `json:"context"`
	Components  []Component `json:"components"`
}

// Context anchors the snapshot to the viewport.
type Context struct {
	Viewport Rect   `json:"viewport"`
	Locale   string `json:"locale,omitempty"`
}

// Rect is an axis-aligned box in CSS px.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// DualRect is a box expressed relative to the bn-context host and relative to
// the owning component host.
type DualRect struct {
	Context   Rect `json:"context"`
	Component Rect `json:"component"`
}

// Component mirrors the engine's ComponentLayoutState.
type Component struct {
	Version     int          `json:"version"`
	Tag         string       `json:"tag"`
	ID          string       `json:"id"`
	Slot        string       `json:"slot,omitempty"`
	Rect        DualRect     `json:"rect"`
	Em          Em           `json:"em"`
	Regions     []Region     `json:"regions,omitempty"`
	Metadata    Metadata     `json:"metadata"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Scaling     *Scaling     `json:"scaling,omitempty"`
	Error       string       `json:"error,omitempty"`
}

// Em is the component's effective font context.
type Em struct {
	FontSizePx float64 `json:"fontSizePx"`
	// AppliedScaleFactor is the font auto-fit factor: 1 when the component
	// was not shrunk to fit.
	AppliedScaleFactor float64 `json:"appliedScaleFactor"`
}

// Region is a named sub-area: canvas:<segment> for charts, header/body for
// tables, grid for tree and grid. Chart canvas regions measure the rendered
// svg#drawCanvas, so they report content size even when the host clips it.
type Region struct {
	ID   string   `json:"id"`
	Rect DualRect `json:"rect"`
}

// Metadata mirrors the engine's ComponentMetadata. Counts are pointers so an
// absent count (a bn-text has no rows) is distinguishable from a zero one (a
// table that rendered no rows) — the difference the empty-component check
// turns on.
type Metadata struct {
	Title          string   `json:"title,omitempty"`
	Scenarios      []string `json:"scenarios,omitempty"`
	Variances      []string `json:"variances,omitempty"`
	BarCount       *int     `json:"barCount,omitempty"`
	PointCount     *int     `json:"pointCount,omitempty"`
	RowCount       *int     `json:"rowCount,omitempty"`
	ColumnCount    *int     `json:"columnCount,omitempty"`
	HeaderRowCount *int     `json:"headerRowCount,omitempty"`
	CellCount      *int     `json:"cellCount,omitempty"`
	NodeCount      *int     `json:"nodeCount,omitempty"`
	EdgeCount      *int     `json:"edgeCount,omitempty"`
	ChartMode      string   `json:"chartMode,omitempty"`
	HasNoData      *bool    `json:"hasNoData,omitempty"`
}

// Diagnostic mirrors the engine's LayoutDiagnosticItem — the same items the
// component publishes in its has-error attribute.
type Diagnostic struct {
	ID          string `json:"id,omitempty"`
	Type        string `json:"type,omitempty"`
	Message     string `json:"message,omitempty"`
	Description string `json:"description,omitempty"`
}

// Scaling is the component's resolved value scaling, including scales the
// engine auto-fitted silently.
type Scaling struct {
	UnitPxPerUnit         *float64 `json:"unitPxPerUnit,omitempty"`
	PercentagePxPerPoint  *float64 `json:"percentagePxPerPoint,omitempty"`
	UnitsPerEm            *float64 `json:"unitsPerEm,omitempty"`
	PercentagePointsPerEm *float64 `json:"percentagePointsPerEm,omitempty"`
	UnitMode              string   `json:"unitMode,omitempty"`
	PercentageMode        string   `json:"percentageMode,omitempty"`
}
