package layoutstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func intPtr(v int) *int           { return &v }
func boolPtr(v bool) *bool        { return &v }
func floatPtr(v float64) *float64 { return &v }

// component builds a minimally valid component entry with a 100x50 host box.
func component(t *testing.T, id, tag string) Component {
	t.Helper()
	return Component{
		Version: Version,
		Tag:     tag,
		ID:      id,
		Rect:    DualRect{Component: Rect{Width: 100, Height: 50}},
		Em:      Em{FontSizePx: 13.33, AppliedScaleFactor: 1},
	}
}

// snapshot wraps components into a version-1 capture.
func snapshot(components []Component, sources map[string]Source) Snapshot {
	return Snapshot{
		State:   State{Version: Version, Detail: "summary", Components: components},
		Sources: sources,
	}
}

// rules returns the rule of each finding, in order.
func rules(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Rule)
	}
	return out
}

func TestAnalyzeVersionGate(t *testing.T) {
	tests := []struct {
		name    string
		version int
		want    bool
	}{
		{"supported", Version, true},
		{"absent", 0, false},
		{"future breaking change", 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportedVersion(tt.version); got != tt.want {
				t.Errorf("SupportedVersion(%d) = %v, want %v", tt.version, got, tt.want)
			}

			c := component(t, "bn-table[0]", "bn-table")
			c.Metadata.RowCount = intPtr(0)
			snap := snapshot([]Component{c}, nil)
			snap.State.Version = tt.version

			got := Analyze(snap)
			if tt.want && len(got) == 0 {
				t.Error("Analyze() = no findings on a supported version, want the empty-component finding")
			}
			if !tt.want && len(got) != 0 {
				t.Errorf("Analyze() = %v on an unsupported version, want none", rules(got))
			}
		})
	}
}

func TestCheckEmpty(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		metadata Metadata
		want     bool
		wantMsg  string
	}{
		{
			name:     "table reporting no data",
			tag:      "bn-table",
			metadata: Metadata{HasNoData: boolPtr(true), RowCount: intPtr(0)},
			want:     true,
			wantMsg:  "the component reports no data",
		},
		{
			name:     "chart with zero bars",
			tag:      "bn-chart-time",
			metadata: Metadata{BarCount: intPtr(0)},
			want:     true,
			wantMsg:  "0 bars rendered",
		},
		{
			// The XY charts report pointCount but never hasNoData, so only
			// the count branch catches them.
			name:     "scatter with zero points",
			tag:      "bn-chart-scatter",
			metadata: Metadata{PointCount: intPtr(0)},
			want:     true,
			wantMsg:  "0 points rendered",
		},
		{
			name:     "tree with zero nodes",
			tag:      "bn-tree",
			metadata: Metadata{NodeCount: intPtr(0)},
			want:     true,
			wantMsg:  "0 nodes rendered",
		},
		{
			name:     "table with rows",
			tag:      "bn-table",
			metadata: Metadata{HasNoData: boolPtr(false), RowCount: intPtr(12)},
			want:     false,
		},
		{
			// A component that declares no counts must never be flagged,
			// otherwise every bn-text and layout container reports empty.
			name:     "text declares no counts",
			tag:      "bn-text",
			metadata: Metadata{},
			want:     false,
		},
		{
			name:     "layout page declares no counts",
			tag:      "bn-layout-page",
			metadata: Metadata{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := component(t, tt.tag+"[0]", tt.tag)
			c.Metadata = tt.metadata

			got := Analyze(snapshot([]Component{c}, nil))

			if !tt.want {
				if len(got) != 0 {
					t.Fatalf("Analyze() = %v, want no findings", rules(got))
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Analyze() = %d findings %v, want 1", len(got), rules(got))
			}
			if got[0].Rule != RuleEmptyComponent {
				t.Errorf("rule = %q, want %q", got[0].Rule, RuleEmptyComponent)
			}
			if !strings.Contains(got[0].Message, tt.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", got[0].Message, tt.wantMsg)
			}
		})
	}
}

func TestCheckOverflow(t *testing.T) {
	overflowDiag := Diagnostic{ID: "WARN_overflow", Type: "warning", Message: "Chart overflows its container."}

	tests := []struct {
		name        string
		diagnostics []Diagnostic
		regions     []Region
		want        bool
		wantSev     Severity
		wantExtent  string
	}{
		{
			name:        "canvas wider than the host",
			diagnostics: []Diagnostic{overflowDiag},
			// Host is 100x50; the canvas runs to x=118, so 18px stick out.
			regions:    []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{X: 0, Y: 0, Width: 118, Height: 40}}}},
			want:       true,
			wantSev:    SeverityWarning,
			wantExtent: "18px horizontally",
		},
		{
			name:        "canvas taller than the host",
			diagnostics: []Diagnostic{overflowDiag},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{X: 0, Y: 6, Width: 90, Height: 58}}}},
			want:        true,
			wantSev:     SeverityWarning,
			wantExtent:  "14px vertically",
		},
		{
			name:        "both axes",
			diagnostics: []Diagnostic{overflowDiag},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{X: 0, Y: 0, Width: 110, Height: 70}}}},
			want:        true,
			wantSev:     SeverityWarning,
			wantExtent:  "10px horizontally and 20px vertically",
		},
		{
			// A fixed scaling attribute makes the engine raise the severity;
			// the finding must inherit it rather than flatten to warning.
			name:        "engine reports an error",
			diagnostics: []Diagnostic{{ID: "WARN_overflow", Type: "error", Message: "Chart overflows its container."}},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{Width: 130, Height: 50}}}},
			want:        true,
			wantSev:     SeverityError,
			wantExtent:  "30px horizontally",
		},
		{
			// The host clips the content, so geometry shows nothing sticking
			// out. The engine's message must still come through.
			name:        "no measurable extent",
			diagnostics: []Diagnostic{overflowDiag},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{Width: 100, Height: 50}}}},
			want:        true,
			wantSev:     SeverityWarning,
			wantExtent:  "",
		},
		{
			// Sub-pixel rounding must not be reported as overflow.
			name:        "within tolerance",
			diagnostics: []Diagnostic{overflowDiag},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{Width: 100.4, Height: 50}}}},
			want:        true,
			wantSev:     SeverityWarning,
			wantExtent:  "",
		},
		{
			name:        "unrelated diagnostic",
			diagnostics: []Diagnostic{{ID: "ERR_invalid_value", Type: "warning", Message: "level: invalid value."}},
			regions:     []Region{{ID: "canvas:base", Rect: DualRect{Component: Rect{Width: 130, Height: 50}}}},
			want:        false,
		},
		{
			name: "no diagnostics",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := component(t, "bn-chart-time[0]", "bn-chart-time")
			c.Diagnostics = tt.diagnostics
			c.Regions = tt.regions

			got := Analyze(snapshot([]Component{c}, nil))

			if !tt.want {
				if len(got) != 0 {
					t.Fatalf("Analyze() = %v, want no findings", rules(got))
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Analyze() = %d findings %v, want 1", len(got), rules(got))
			}
			f := got[0]
			if f.Rule != RuleOverflow {
				t.Errorf("rule = %q, want %q", f.Rule, RuleOverflow)
			}
			if f.Severity != tt.wantSev {
				t.Errorf("severity = %q, want %q", f.Severity, tt.wantSev)
			}
			if tt.wantExtent == "" {
				if strings.Contains(f.Message, "exceeds the box") {
					t.Errorf("message = %q, want no extent clause", f.Message)
				}
				return
			}
			if !strings.Contains(f.Message, "exceeds the box by "+tt.wantExtent) {
				t.Errorf("message = %q, want extent %q", f.Message, tt.wantExtent)
			}
		})
	}
}

func TestCheckFontShrunk(t *testing.T) {
	autoScale := Diagnostic{ID: "AUTO_scale", Type: "warning", Message: "Auto-scaled font by factor 0.128"}

	tests := []struct {
		name        string
		factor      float64
		diagnostics []Diagnostic
		want        bool
		wantMsg     string
	}{
		{name: "shrunk to 82%", factor: 0.82, want: true, wantMsg: "82%"},
		{name: "shrunk to 50%", factor: 0.5, want: true, wantMsg: "50%"},
		{name: "not scaled", factor: 1, want: false},
		{name: "float noise around 1", factor: 1.0005, want: false},
		{name: "float noise below 1", factor: 0.9995, want: false},
		// The engine reports a sane factor when it cannot measure; a
		// non-positive value means "unknown", not "shrunk to nothing".
		{name: "unmeasurable", factor: 0, want: false},
		{name: "enlarged", factor: 1.4, want: false},
		{
			// Components on the engine's generic layout state — bn-text among
			// them — always report factor 1 and announce the fit through this
			// diagnostic instead. Without it the check could never fire for
			// the component type that shrinks most often.
			name:        "generic component reports the fit as a diagnostic",
			factor:      1,
			diagnostics: []Diagnostic{autoScale},
			want:        true,
			wantMsg:     "Auto-scaled font by factor 0.128",
		},
		{
			// The engine removes AUTO_scale once the content fits, so a
			// component carrying other auto notices must not be flagged.
			name:        "other auto notices are not font fits",
			factor:      1,
			diagnostics: []Diagnostic{{ID: "AUTO_percentageScaling", Type: "warning", Message: "auto-fitted"}},
			want:        false,
		},
		{
			// A rich component can report both; the measured factor is the
			// better message and must not produce a second finding.
			name:        "factor wins over the diagnostic",
			factor:      0.4,
			diagnostics: []Diagnostic{autoScale},
			want:        true,
			wantMsg:     "40%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := component(t, "bn-table[0]", "bn-table")
			c.Em.AppliedScaleFactor = tt.factor
			c.Diagnostics = tt.diagnostics

			got := Analyze(snapshot([]Component{c}, nil))

			if !tt.want {
				if len(got) != 0 {
					t.Fatalf("Analyze() = %v, want no findings", rules(got))
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("Analyze() = %d findings %v, want 1", len(got), rules(got))
			}
			if got[0].Rule != RuleFontShrunk {
				t.Errorf("rule = %q, want %q", got[0].Rule, RuleFontShrunk)
			}
			if !strings.Contains(got[0].Message, tt.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", got[0].Message, tt.wantMsg)
			}
		})
	}
}

func TestCheckScaleMismatch(t *testing.T) {
	// scaled builds a chart carrying a resolved units-per-em.
	scaled := func(t *testing.T, id string, unitsPerEm float64) Component {
		t.Helper()
		c := component(t, id, "bn-chart-time")
		c.Metadata.BarCount = intPtr(4)
		c.Scaling = &Scaling{UnitMode: "auto", UnitsPerEm: floatPtr(unitsPerEm)}
		return c
	}

	tests := []struct {
		name    string
		values  map[string]float64
		sources map[string]Source
		wantIDs []string
		wantPin string
	}{
		{
			name:    "same unit, diverging scales",
			values:  map[string]float64{"a": 12.5, "b": 40},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}, "b": {MeasureUnit: "EUR"}},
			// Only the odd one out is flagged; the largest scale is the safe
			// pin because more units per em means shorter bars.
			wantIDs: []string{"a"},
			wantPin: "40",
		},
		{
			name:    "three members, one outlier",
			values:  map[string]float64{"a": 40, "b": 40, "c": 10},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}, "b": {MeasureUnit: "EUR"}, "c": {MeasureUnit: "EUR"}},
			wantIDs: []string{"c"},
			wantPin: "40",
		},
		{
			name:    "identical scales",
			values:  map[string]float64{"a": 40, "b": 40},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}, "b": {MeasureUnit: "EUR"}},
			wantIDs: nil,
		},
		{
			name:    "within tolerance",
			values:  map[string]float64{"a": 40, "b": 40.1},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}, "b": {MeasureUnit: "EUR"}},
			wantIDs: nil,
		},
		{
			name:    "different units are different measures",
			values:  map[string]float64{"a": 12.5, "b": 40},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}, "b": {MeasureUnit: "pcs"}},
			wantIDs: nil,
		},
		{
			name:    "different scale words are different measures",
			values:  map[string]float64{"a": 12.5, "b": 40},
			sources: map[string]Source{"a": {MeasureUnit: "EUR", MeasureScale: "greatest"}, "b": {MeasureUnit: "EUR", MeasureScale: "least"}},
			wantIDs: nil,
		},
		{
			// Without a declared unit there is no evidence the components
			// show the same measure, so grouping them would be a guess.
			name:    "no declared unit",
			values:  map[string]float64{"a": 12.5, "b": 40},
			sources: map[string]Source{"a": {}, "b": {}},
			wantIDs: nil,
		},
		{
			name:    "single member",
			values:  map[string]float64{"a": 12.5},
			sources: map[string]Source{"a": {MeasureUnit: "EUR"}},
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sort ids so component order is deterministic regardless of map
			// iteration order.
			ids := make([]string, 0, len(tt.values))
			for id := range tt.values {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			components := make([]Component, 0, len(ids))
			for _, id := range ids {
				components = append(components, scaled(t, id, tt.values[id]))
			}

			var got []Finding
			for _, f := range Analyze(snapshot(components, tt.sources)) {
				if f.Rule == RuleScaleMismatch {
					got = append(got, f)
				}
			}

			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d scale findings, want %d: %+v", len(got), len(tt.wantIDs), got)
			}
			for i, want := range tt.wantIDs {
				if got[i].ComponentID != want {
					t.Errorf("finding %d componentId = %q, want %q", i, got[i].ComponentID, want)
				}
				if !strings.Contains(got[i].Hint, "unitScaling: "+tt.wantPin) {
					t.Errorf("finding %d hint = %q, want it to pin %q", i, got[i].Hint, tt.wantPin)
				}
			}
		})
	}
}

// TestAnalyzeOrdering pins the contract the inspector and the golden build
// warnings rely on: component-scoped findings in document order, then the
// cross-component scale comparison.
func TestAnalyzeOrdering(t *testing.T) {
	first := component(t, "a", "bn-chart-time")
	first.Metadata.BarCount = intPtr(4)
	first.Em.AppliedScaleFactor = 0.8
	first.Scaling = &Scaling{UnitsPerEm: floatPtr(10)}

	second := component(t, "b", "bn-chart-time")
	second.Metadata.BarCount = intPtr(0)
	second.Scaling = &Scaling{UnitsPerEm: floatPtr(40)}

	snap := snapshot([]Component{first, second}, map[string]Source{
		"a": {Kind: "ChartTime", Name: "revenue", MeasureUnit: "EUR"},
		"b": {Kind: "ChartTime", Name: "margin", MeasureUnit: "EUR"},
	})

	want := []string{RuleFontShrunk, RuleEmptyComponent, RuleScaleMismatch}
	if got := rules(Analyze(snap)); !slices.Equal(got, want) {
		t.Errorf("Analyze() rules = %v, want %v", got, want)
	}
}

func TestFindingString(t *testing.T) {
	tests := []struct {
		name    string
		finding Finding
		want    string
	}{
		{
			name:    "named component",
			finding: Finding{Rule: RuleEmptyComponent, Kind: "Table", Name: "sales", Message: "rendered empty"},
			want:    "[layout-empty-component] Table sales: rendered empty",
		},
		{
			name:    "falls back to the engine id",
			finding: Finding{Rule: RuleOverflow, ComponentID: "bn-table[0]", Message: "overflows"},
			want:    "[layout-overflow] bn-table[0]: overflows",
		},
		{
			name:    "with a hint",
			finding: Finding{Rule: RuleScaleMismatch, Name: "revenue", Message: "scale differs", Hint: "set unitScaling: 40"},
			want:    "[layout-scale-mismatch] revenue: scale differs (set unitScaling: 40)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.finding.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceLabel(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		want   string
	}{
		{"kind and name", Source{Kind: "Table", Name: "sales"}, "Table sales"},
		{"ref stands in for name", Source{Kind: "Table", Ref: "sharedTable"}, "Table sharedTable"},
		{"name only", Source{Name: "sales"}, "sales"},
		{"kind only", Source{Kind: "Table"}, "Table"},
		{"nothing known", Source{}, "bn-table[0]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.source.Label("bn-table[0]"); got != tt.want {
				t.Errorf("Label() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDecodeSnapshot checks the Go types against a capture shaped like the
// engine's real output, including fields this package deliberately ignores
// (elements, table) and ones it must not confuse with zero (counts).
func TestDecodeSnapshot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "snapshot.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	if snap.State.Version != Version {
		t.Errorf("version = %d, want %d", snap.State.Version, Version)
	}
	if len(snap.State.Components) != 4 {
		t.Fatalf("components = %d, want 4", len(snap.State.Components))
	}

	table := snap.State.Components[1]
	if table.Tag != "bn-table" {
		t.Fatalf("components[1].tag = %q, want bn-table", table.Tag)
	}
	if table.Metadata.RowCount == nil || *table.Metadata.RowCount != 0 {
		t.Errorf("table rowCount = %v, want a pointer to 0", table.Metadata.RowCount)
	}
	if table.Rect.Context.Width != 640 {
		t.Errorf("table context width = %v, want 640", table.Rect.Context.Width)
	}

	chart := snap.State.Components[2]
	if chart.Scaling == nil || chart.Scaling.UnitsPerEm == nil || *chart.Scaling.UnitsPerEm != 12.5 {
		t.Errorf("chart unitsPerEm = %v, want 12.5", chart.Scaling)
	}

	// A component that declares no counts must decode with nil pointers, or
	// the empty check would flag every text block.
	text := snap.State.Components[3]
	if text.Metadata.RowCount != nil || text.Metadata.BarCount != nil || text.Metadata.HasNoData != nil {
		t.Errorf("text metadata = %+v, want all counts nil", text.Metadata)
	}

	got := rules(Analyze(snap))
	want := []string{RuleEmptyComponent, RuleOverflow, RuleScaleMismatch}
	if !slices.Equal(got, want) {
		t.Errorf("Analyze(fixture) rules = %v, want %v", got, want)
	}
}
