package layoutstate

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Rule identifiers. They are stable strings: the preview inspector, the build
// warnings and the MCP tooling all key off them.
const (
	RuleEmptyComponent = "layout-empty-component"
	RuleScaleMismatch  = "layout-scale-mismatch"
	RuleOverflow       = "layout-overflow"
	RuleFontShrunk     = "layout-font-shrunk"
)

// Severity of a finding, matching the engine's diagnostic vocabulary.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

// Finding is one render-time problem derived from a snapshot.
type Finding struct {
	Rule        string   `json:"rule"`
	Severity    Severity `json:"severity"`
	ComponentID string   `json:"componentId"`
	Tag         string   `json:"tag,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Name        string   `json:"name,omitempty"`
	Message     string   `json:"message"`
	Hint        string   `json:"hint,omitempty"`
}

// String renders the finding as a single log/warning line.
func (f Finding) String() string {
	label := f.Name
	if f.Kind != "" && label != "" {
		label = f.Kind + " " + label
	} else if label == "" {
		label = f.ComponentID
	}
	s := fmt.Sprintf("[%s] %s: %s", f.Rule, label, f.Message)
	if f.Hint != "" {
		s += " (" + f.Hint + ")"
	}
	return s
}

// overflowTolerancePx matches the engine's integer-floored overflow tolerance:
// sub-pixel differences are rounding, not overflow.
const overflowTolerancePx = 1.0

// scaleToleranceRatio is the relative spread two resolved scales may differ by
// before they count as a mismatch. Auto-fit produces floats that differ in the
// last decimals for identical layouts; a half-percent difference is invisible,
// while a real IBCS violation is a factor.
const scaleToleranceRatio = 0.005

// fontScaleEpsilon guards the float comparison against 1.0.
const fontScaleEpsilon = 0.001

// Analyze derives findings from a capture. Component-scoped findings come
// first in document order, then the cross-component scale comparison, so the
// output is deterministic for golden tests and stable in the inspector.
//
// It never fails: a malformed or partial snapshot yields fewer findings, not
// an error. A snapshot whose version this package does not understand yields
// none — see SupportedVersion.
func Analyze(snap Snapshot) []Finding {
	if !SupportedVersion(snap.State.Version) {
		return nil
	}

	findings := make([]Finding, 0, 8)
	for _, c := range snap.State.Components {
		src := snap.Sources[c.ID]
		if f, ok := checkEmpty(c, src); ok {
			findings = append(findings, f)
		}
		findings = append(findings, checkOverflow(c, src)...)
		if f, ok := checkFontShrunk(c, src); ok {
			findings = append(findings, f)
		}
	}
	return append(findings, checkScaleMismatch(snap)...)
}

// SupportedVersion reports whether a snapshot's schema version is one this
// package can read. Version 0 means the field was absent, which is not a valid
// engine snapshot.
func SupportedVersion(v int) bool { return v == Version }

// checkEmpty flags a component that rendered but has nothing in it — the
// signature of a dataset or SQL wiring mistake, which otherwise just looks
// like an empty box in the preview.
//
// Charts and tables report hasNoData; the XY charts (scatter/bubble/bullet)
// only report pointCount, so the zero-count branch carries them. Components
// that declare no count at all (bn-text, bn-image, layout containers) are
// never flagged.
func checkEmpty(c Component, src Source) (Finding, bool) {
	reason := ""
	switch {
	case c.Metadata.HasNoData != nil && *c.Metadata.HasNoData:
		reason = "the component reports no data"
	case isZero(c.Metadata.BarCount):
		reason = "0 bars rendered"
	case isZero(c.Metadata.PointCount):
		reason = "0 points rendered"
	case isZero(c.Metadata.RowCount):
		reason = "0 rows rendered"
	case isZero(c.Metadata.NodeCount):
		reason = "0 nodes rendered"
	default:
		return Finding{}, false
	}

	return newFinding(c, src, RuleEmptyComponent, SeverityWarning,
		"rendered empty — "+reason,
		"check the dataset reference and that its query returns rows"), true
}

// checkOverflow reports content that does not fit its component box.
//
// The engine already detects this and publishes a WARN_overflow diagnostic,
// but without a magnitude. The magnitude comes from the regions: a chart's
// canvas region measures the rendered svg#drawCanvas, so it reports the
// content's true size even when the host clips it.
func checkOverflow(c Component, src Source) []Finding {
	var out []Finding
	for _, d := range c.Diagnostics {
		if !strings.Contains(strings.ToLower(d.ID), "overflow") {
			continue
		}
		msg := d.Message
		if msg == "" {
			msg = "content overflows the component box"
		}
		if by := overflowExtent(c); by != "" {
			msg = strings.TrimSuffix(msg, ".") + " — content exceeds the box by " + by
		}
		out = append(out, newFinding(c, src, RuleOverflow, severityOf(d), msg,
			"give the component more space, or pin a larger unitScaling so the content shrinks"))
	}
	return out
}

// overflowExtent measures how far the component's regions extend past its own
// box, in component-relative coordinates where the host's origin is (0,0).
// Returns "" when everything fits.
func overflowExtent(c Component) string {
	var right, bottom float64
	for _, r := range c.Regions {
		right = math.Max(right, r.Rect.Component.X+r.Rect.Component.Width-c.Rect.Component.Width)
		bottom = math.Max(bottom, r.Rect.Component.Y+r.Rect.Component.Height-c.Rect.Component.Height)
	}

	var parts []string
	if right > overflowTolerancePx {
		parts = append(parts, formatPx(right)+" horizontally")
	}
	if bottom > overflowTolerancePx {
		parts = append(parts, formatPx(bottom)+" vertically")
	}
	return strings.Join(parts, " and ")
}

// autoScaleDiagnosticID is the engine's font auto-fit notice. It is emitted
// only when the content did not fit, and removed once it does.
const autoScaleDiagnosticID = "auto_scale"

// checkFontShrunk reports a component whose font was auto-fitted down. It
// still renders, so nothing else surfaces it — but it is a silent typography
// regression, and it breaks font consistency across pages.
//
// Two signals, because only the rich components compute a factor: components
// served by the engine's generic layout state (bn-text among them) always
// report appliedScaleFactor 1 and announce the fit through an AUTO_scale
// diagnostic instead. That diagnostic's message carries the factor, and is
// passed through verbatim rather than parsed.
func checkFontShrunk(c Component, src Source) (Finding, bool) {
	hint := "give the component more space to keep type sizes consistent across pages"

	factor := c.Em.AppliedScaleFactor
	if factor > 0 && factor < 1-fontScaleEpsilon {
		pct := strconv.FormatFloat(factor*100, 'f', 0, 64)
		return newFinding(c, src, RuleFontShrunk, SeverityWarning,
			"font auto-fitted down to "+pct+"% to make the content fit", hint), true
	}

	for _, d := range c.Diagnostics {
		if !strings.EqualFold(d.ID, autoScaleDiagnosticID) {
			continue
		}
		msg := "font auto-fitted down to make the content fit"
		if d.Message != "" {
			msg += " — " + d.Message
		}
		return newFinding(c, src, RuleFontShrunk, SeverityWarning, msg, hint), true
	}
	return Finding{}, false
}

// scaleGroup collects the components whose resolved scale should agree.
type scaleGroup struct {
	key     string
	members []scaleMember
}

type scaleMember struct {
	component  Component
	source     Source
	unitsPerEm float64
}

// checkScaleMismatch compares the resolved units-per-em of components showing
// the same measure. IBCS requires a uniform scale so bars stay comparable, and
// the engine auto-fits each component independently — so two charts of the
// same measure silently end up with different scales, and nothing says so.
//
// Grouping needs a measure identity the engine does not report, so it uses the
// measure-unit attribute this CLI renders from the manifest. Components
// without a declared unit are skipped rather than lumped together: grouping
// every unitless component would flag unrelated measures.
func checkScaleMismatch(snap Snapshot) []Finding {
	groups := map[string]*scaleGroup{}

	for _, c := range snap.State.Components {
		src := snap.Sources[c.ID]
		if src.MeasureUnit == "" || c.Scaling == nil || c.Scaling.UnitsPerEm == nil {
			continue
		}
		value := *c.Scaling.UnitsPerEm
		if !(value > 0) || math.IsInf(value, 0) {
			continue
		}
		key := src.MeasureUnit + "\x00" + src.MeasureScale
		g, ok := groups[key]
		if !ok {
			g = &scaleGroup{key: key}
			groups[key] = g
		}
		g.members = append(g.members, scaleMember{component: c, source: src, unitsPerEm: value})
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var out []Finding
	for _, key := range keys {
		g := groups[key]
		if len(g.members) < 2 {
			continue
		}
		lo, hi := g.members[0].unitsPerEm, g.members[0].unitsPerEm
		for _, m := range g.members[1:] {
			lo = math.Min(lo, m.unitsPerEm)
			hi = math.Max(hi, m.unitsPerEm)
		}
		if hi/lo-1 <= scaleToleranceRatio {
			continue
		}

		// Pinning to the largest units-per-em is the safe choice: more units
		// per em means shorter bars, so no member starts overflowing.
		pin := formatScale(hi)
		unit := g.members[0].source.MeasureUnit
		for _, m := range g.members {
			if m.unitsPerEm >= hi*(1-scaleToleranceRatio) {
				continue
			}
			out = append(out, newFinding(m.component, m.source, RuleScaleMismatch, SeverityWarning,
				fmt.Sprintf("scale %s %s/em differs from %s %s/em used elsewhere for the same measure",
					formatScale(m.unitsPerEm), unit, pin, unit),
				"set unitScaling: "+pin+" on every component showing this measure"))
		}
	}
	return out
}

func newFinding(c Component, src Source, rule string, sev Severity, message, hint string) Finding {
	name := src.Name
	if name == "" {
		name = src.Ref
	}
	return Finding{
		Rule:        rule,
		Severity:    sev,
		ComponentID: c.ID,
		Tag:         c.Tag,
		Kind:        src.Kind,
		Name:        name,
		Message:     message,
		Hint:        hint,
	}
}

func severityOf(d Diagnostic) Severity {
	if strings.EqualFold(d.Type, string(SeverityError)) {
		return SeverityError
	}
	return SeverityWarning
}

func isZero(count *int) bool { return count != nil && *count == 0 }

func formatPx(v float64) string {
	return strconv.FormatFloat(math.Round(v), 'f', 0, 64) + "px"
}

// formatScale trims trailing zeros so 40.00 prints as 40 and 12.50 as 12.5.
func formatScale(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
