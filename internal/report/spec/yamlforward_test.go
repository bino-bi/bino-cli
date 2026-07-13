package spec

import "testing"

// table is the fixture body shared by the resolver cases. Cursor coordinates are
// 1-based (matching yaml.v3 / the resolver contract).
func TestResolvePositionPath(t *testing.T) {
	const table = `kind: Table
apiVersion: bino.bi/v1
metadata:
  name: rev_table
spec:
  dataset: sales
  scenarios:
    - ac1
    - pp1
  variances:
    - dac1_pp1_pos
  title: Revenue
`
	tests := []struct {
		name          string
		content       string
		line, col     int
		wantKind      PositionKind
		wantPath      string
		wantEnclosing string
		wantRefKind   string
		wantPrefix    string
		wantBound     []string
	}{
		{
			name: "kind value", content: table, line: 1, col: 7,
			wantKind: PosKindValue, wantPath: "kind", wantEnclosing: "Table", wantPrefix: "Table",
		},
		{
			name: "dataset reference value", content: table, line: 6, col: 12,
			wantKind: PosDatasetRef, wantPath: "spec.dataset", wantEnclosing: "Table",
			wantRefKind: "DataSet", wantPrefix: "sales",
		},
		{
			name: "scenario item existing", content: table, line: 8, col: 8,
			wantKind: PosScenarioItem, wantEnclosing: "Table", wantPrefix: "ac1",
			wantBound: []string{"sales"},
		},
		{
			name: "variance item existing", content: table, line: 11, col: 9,
			wantKind: PosVarianceItem, wantEnclosing: "Table", wantPrefix: "dac1_pp1_pos",
			wantBound: []string{"sales"},
		},
		{
			name: "new key under spec (blank trailing line)", content: table, line: 13, col: 3,
			wantKind: PosKey, wantEnclosing: "Table",
		},
		{
			name: "free value (title)", content: table, line: 12, col: 12,
			wantKind: PosFreeValue, wantPath: "spec.title", wantEnclosing: "Table", wantPrefix: "Revenue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, ok := ResolvePositionPath(tt.content, tt.line, tt.col)
			if !ok {
				t.Fatalf("ResolvePositionPath() ok=false, want a context")
			}
			if ctx.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", ctx.Kind, tt.wantKind)
			}
			if tt.wantPath != "" && ctx.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", ctx.Path, tt.wantPath)
			}
			if tt.wantEnclosing != "" && ctx.EnclosingKind != tt.wantEnclosing {
				t.Errorf("EnclosingKind = %q, want %q", ctx.EnclosingKind, tt.wantEnclosing)
			}
			if tt.wantRefKind != "" && ctx.RefKind != tt.wantRefKind {
				t.Errorf("RefKind = %q, want %q", ctx.RefKind, tt.wantRefKind)
			}
			if tt.wantPrefix != "" && ctx.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %q, want %q", ctx.Prefix, tt.wantPrefix)
			}
			if tt.wantBound != nil {
				if len(ctx.BoundDatasets) != len(tt.wantBound) || (len(tt.wantBound) > 0 && ctx.BoundDatasets[0] != tt.wantBound[0]) {
					t.Errorf("BoundDatasets = %v, want %v", ctx.BoundDatasets, tt.wantBound)
				}
			}
		})
	}
}

func TestResolvePositionPath_NewScenarioItem(t *testing.T) {
	// A fresh "- " slot below the last scenario should classify as a scenario item
	// with an empty prefix (the most valuable completion case).
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  scenarios:
    - ac1
    -
`
	ctx, ok := ResolvePositionPath(doc, 8, 7)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosScenarioItem {
		t.Fatalf("Kind = %v, want PosScenarioItem", ctx.Kind)
	}
	if len(ctx.BoundDatasets) != 1 || ctx.BoundDatasets[0] != "sales" {
		t.Errorf("BoundDatasets = %v, want [sales]", ctx.BoundDatasets)
	}
}

func TestResolvePositionPath_NestedLayoutChildBinding(t *testing.T) {
	// A scenario inside a layout child binds to that child's dataset, not the
	// document root (a LayoutPage has no top-level dataset).
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartStructure
      metadata:
        name: chart
      spec:
        dataset: revenue_by_region
        scenarios:
          - ac1
`
	ctx, ok := ResolvePositionPath(doc, 12, 13) // on "- ac1"
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosScenarioItem {
		t.Fatalf("Kind = %v, want PosScenarioItem", ctx.Kind)
	}
	if len(ctx.BoundDatasets) != 1 || ctx.BoundDatasets[0] != "revenue_by_region" {
		t.Errorf("BoundDatasets = %v, want [revenue_by_region] (the nearest enclosing child dataset)", ctx.BoundDatasets)
	}
	if ctx.EnclosingKind != "ChartStructure" {
		t.Errorf("EnclosingKind = %q, want ChartStructure (the child's kind, not the LayoutPage root)", ctx.EnclosingKind)
	}
}

func TestResolvePositionPath_FlowSequenceScenarios(t *testing.T) {
	// A flow-style scenarios value must still classify as scenario items.
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  scenarios: ["ac1", "pp1"]
`
	ctx, ok := ResolvePositionPath(doc, 6, 18) // inside the flow array
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosScenarioItem {
		t.Errorf("Kind = %v, want PosScenarioItem for a flow-style scenarios value", ctx.Kind)
	}
}

func TestResolvePositionPath_SelectedStyleRef(t *testing.T) {
	// selectedStyle resolves to a ComponentStyle reference, same as source/page/etc.
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  selectedStyle: highlighted
`
	ctx, ok := ResolvePositionPath(doc, 6, 20) // on "highlighted"
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosDatasetRef || ctx.RefKind != "ComponentStyle" {
		t.Errorf("Kind/RefKind = %v/%q, want PosDatasetRef/ComponentStyle", ctx.Kind, ctx.RefKind)
	}
	if ctx.Prefix != "highlighted" {
		t.Errorf("Prefix = %q, want highlighted", ctx.Prefix)
	}
}

func TestResolvePositionPath_RefParams(t *testing.T) {
	// A layout child's ref/params positions resolve against the sibling `kind`
	// and the referenced document's name.
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: "@acme/kpi-card"
      params:
        REGION: eu
        LIMIT: '10'
    - kind: Text
      ref: intro
      params:
`
	tests := []struct {
		name        string
		line, col   int
		wantKind    PositionKind
		wantRefKind string
		wantRefName string
		wantPresent []string
		wantPrefix  string
	}{
		{
			name: "ref value carries sibling kind and present params", line: 7, col: 14,
			wantKind: PosDatasetRef, wantRefKind: "Table", wantRefName: "@acme/kpi-card",
			wantPresent: []string{"REGION", "LIMIT"}, wantPrefix: "@acme/kpi-card",
		},
		{
			name: "param key position", line: 9, col: 10,
			wantKind: PosParamKey, wantRefKind: "Table", wantRefName: "@acme/kpi-card",
			wantPresent: []string{"REGION", "LIMIT"},
		},
		{
			name: "param value position", line: 9, col: 17,
			wantKind: PosParamValue, wantRefKind: "Table", wantRefName: "@acme/kpi-card",
			wantPrefix: "eu",
		},
		{
			name: "empty params mapping is a param key slot", line: 14, col: 9,
			wantKind: PosParamKey, wantRefKind: "Text", wantRefName: "intro",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, ok := ResolvePositionPath(doc, tt.line, tt.col)
			if !ok {
				t.Fatal("ok=false")
			}
			if ctx.Kind != tt.wantKind {
				t.Fatalf("Kind = %v, want %v", ctx.Kind, tt.wantKind)
			}
			if ctx.RefKind != tt.wantRefKind {
				t.Errorf("RefKind = %q, want %q", ctx.RefKind, tt.wantRefKind)
			}
			if ctx.RefName != tt.wantRefName {
				t.Errorf("RefName = %q, want %q", ctx.RefName, tt.wantRefName)
			}
			if tt.wantPrefix != "" && ctx.Prefix != tt.wantPrefix {
				t.Errorf("Prefix = %q, want %q", ctx.Prefix, tt.wantPrefix)
			}
			if tt.wantPresent != nil {
				if len(ctx.PresentKeys) != len(tt.wantPresent) {
					t.Fatalf("PresentKeys = %v, want %v", ctx.PresentKeys, tt.wantPresent)
				}
				for i := range tt.wantPresent {
					if ctx.PresentKeys[i] != tt.wantPresent[i] {
						t.Errorf("PresentKeys = %v, want %v", ctx.PresentKeys, tt.wantPresent)
						break
					}
				}
			}
		})
	}
}

func TestResolvePositionPath_QuotedRefReplaceRange(t *testing.T) {
	// yaml.v3 puts a quoted scalar's Column on the opening quote while Value
	// excludes the quotes; the replace range must cover the content only.
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: "@acme/kpi-card"
`
	ctx, ok := ResolvePositionPath(doc, 7, 14)
	if !ok {
		t.Fatal("ok=false")
	}
	want := Range{StartLine: 7, StartCol: 13, EndLine: 7, EndCol: 13 + len("@acme/kpi-card")}
	if ctx.ReplaceRange != want {
		t.Errorf("ReplaceRange = %+v, want %+v", ctx.ReplaceRange, want)
	}
}

func TestResolvePositionPath_LayoutPagesObjectParams(t *testing.T) {
	// The layoutPages object form ({page, params}) resolves params against the
	// referenced LayoutPage.
	const doc = `kind: ReportArtefact
metadata:
  name: art
spec:
  layoutPages:
    - page: regional
      params:
        REGION: eu
`
	ctx, ok := ResolvePositionPath(doc, 8, 17) // on "eu"
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosParamValue || ctx.RefKind != "LayoutPage" || ctx.RefName != "regional" {
		t.Errorf("Kind/RefKind/RefName = %v/%q/%q, want PosParamValue/LayoutPage/regional",
			ctx.Kind, ctx.RefKind, ctx.RefName)
	}
	if ctx.FieldName != "REGION" {
		t.Errorf("FieldName = %q, want REGION", ctx.FieldName)
	}
}

func TestResolvePositionPath_RefWithoutSiblingKind(t *testing.T) {
	// A ref with no sibling kind keeps the empty RefKind (offer-everything).
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - ref: orphan
`
	ctx, ok := ResolvePositionPath(doc, 6, 13)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosDatasetRef || ctx.RefKind != "" {
		t.Errorf("Kind/RefKind = %v/%q, want PosDatasetRef with empty RefKind", ctx.Kind, ctx.RefKind)
	}
}

func TestResolvePositionPath_MetadataParamsDeclaration(t *testing.T) {
	// metadata.params declaration items (a sequence) keep their plain key/value
	// classification — no PosParamKey false positive.
	const doc = `kind: Table
metadata:
  name: kpi
  params:
    - name: REGION
      type: select
spec:
  dataset: sales
`
	ctx, ok := ResolvePositionPath(doc, 6, 8) // on the item's `type` key
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosKey {
		t.Errorf("Kind = %v, want PosKey inside a metadata.params item", ctx.Kind)
	}
}

func TestRepairUnquotedAt(t *testing.T) {
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: @acme/kpi-c
`
	repaired, token, raw, ok := RepairUnquotedAt(doc, 7)
	if !ok {
		t.Fatal("expected a repair for the unquoted @ value")
	}
	if token != "@acme/kpi-c" {
		t.Errorf("token = %q, want @acme/kpi-c", token)
	}
	want := Range{StartLine: 7, StartCol: 12, EndLine: 7, EndCol: 12 + len("@acme/kpi-c")}
	if raw != want {
		t.Errorf("raw = %+v, want %+v", raw, want)
	}
	// The repaired content must parse and resolve to a kind-aware ref position.
	ctx, rok := ResolvePositionPath(repaired, 7, 14)
	if !rok || ctx.Kind != PosDatasetRef || ctx.RefKind != "Table" {
		t.Errorf("repaired resolve = ok=%v kind=%v refKind=%q, want PosDatasetRef/Table", rok, ctx.Kind, ctx.RefKind)
	}

	for _, tt := range []struct {
		name, line string
		wantOK     bool
	}{
		{"sequence item form", "    - ref: @x/y", true},
		{"bare @ just typed", "      ref: @", true},
		{"already quoted", `      ref: "@x/y"`, false},
		{"plain local name", "      ref: intro", false},
		{"no space after colon", "      ref:@x", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, gotOK := RepairUnquotedAt(tt.line, 1)
			if gotOK != tt.wantOK {
				t.Errorf("RepairUnquotedAt(%q) ok = %v, want %v", tt.line, gotOK, tt.wantOK)
			}
		})
	}
}

func TestResolvePositionPath_MultiDoc(t *testing.T) {
	const content = `kind: DataSource
metadata:
  name: src
spec:
  type: csv
---
kind: DataSet
metadata:
  name: ds
spec:
  source: src
`
	// Cursor on the `source:` value in the SECOND document.
	ctx, ok := ResolvePositionPath(content, 11, 11)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.DocIndex != 1 {
		t.Errorf("DocIndex = %d, want 1", ctx.DocIndex)
	}
	if ctx.EnclosingKind != "DataSet" {
		t.Errorf("EnclosingKind = %q, want DataSet", ctx.EnclosingKind)
	}
	if ctx.Kind != PosDatasetRef || ctx.RefKind != "DataSource" {
		t.Errorf("Kind/RefKind = %v/%q, want PosDatasetRef/DataSource", ctx.Kind, ctx.RefKind)
	}
}

func TestResolvePositionPath_EnvVarLine(t *testing.T) {
	// The resolver parses the RAW buffer; a ${VAR} line must not crash and offsets
	// stay raw-relative.
	const doc = `kind: DataSource
metadata:
  name: src
spec:
  type: csv
  path: ${DATA_DIR}/sales.csv
`
	ctx, ok := ResolvePositionPath(doc, 6, 9)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Path != "spec.path" {
		t.Errorf("Path = %q, want spec.path", ctx.Path)
	}
	if ctx.Prefix != "${DATA_DIR}/sales.csv" {
		t.Errorf("Prefix = %q, want the raw ${VAR} value", ctx.Prefix)
	}
}

func TestResolvePositionPath_BlockScalar(t *testing.T) {
	const doc = `kind: DataSet
metadata:
  name: ds
spec:
  query: |
    SELECT region, amount
    FROM sales
`
	ctx, ok := ResolvePositionPath(doc, 6, 12)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosQueryScalar {
		t.Errorf("Kind = %v, want PosQueryScalar", ctx.Kind)
	}
	if ctx.FieldName != "query" {
		t.Errorf("FieldName = %q, want query", ctx.FieldName)
	}
}

func TestResolvePositionPath_OutOfBounds(t *testing.T) {
	const doc = "kind: Table\nmetadata:\n  name: t\n"
	if _, ok := ResolvePositionPath("", 1, 1); ok {
		t.Error("empty content should yield ok=false")
	}
	if _, ok := ResolvePositionPath("\t bad: : :\n", 1, 1); ok {
		// malformed YAML → no nodes → ok=false
		t.Error("unparseable content should yield ok=false")
	}
	// A cursor far past EOF still resolves into the single document (clamped),
	// which is acceptable; assert it does not panic.
	_, _ = ResolvePositionPath(doc, 99, 1)
}
