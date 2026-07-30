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

func TestResolvePositionPath_NestedKindValue(t *testing.T) {
	// The `kind:` of a layout child is a kind-value position at its own path —
	// the schema (layoutChild.kind enum) decides candidates, not the root enum.
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: rev_table
`
	ctx, ok := ResolvePositionPath(doc, 6, 13) // on "Table"
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosKindValue {
		t.Fatalf("Kind = %v, want PosKindValue", ctx.Kind)
	}
	if ctx.Path != "spec.children.0.kind" {
		t.Errorf("Path = %q, want spec.children.0.kind", ctx.Path)
	}
	if ctx.KindsByPath[""] != "LayoutPage" {
		t.Errorf("KindsByPath[root] = %q, want LayoutPage", ctx.KindsByPath[""])
	}
	if ctx.KindsByPath["spec.children.0"] != "Table" {
		t.Errorf("KindsByPath[spec.children.0] = %q, want Table", ctx.KindsByPath["spec.children.0"])
	}
}

func TestResolvePositionPath_EmptyNestedKindValue(t *testing.T) {
	// `- kind: ` with nothing typed yet must still resolve as the child's
	// kind-value position (this is the screenshot bug: "No suggestions.").
	const doc = `kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind:
`
	ctx, ok := ResolvePositionPath(doc, 6, 13)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosKindValue {
		t.Fatalf("Kind = %v, want PosKindValue", ctx.Kind)
	}
	if ctx.Path != "spec.children.0.kind" {
		t.Errorf("Path = %q, want spec.children.0.kind", ctx.Path)
	}
}

func TestResolvePositionPath_PresentKeysOnSpecKey(t *testing.T) {
	// A new-key position inside spec must report the keys already present so
	// completion stops re-offering them.
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  title: Revenue

`
	ctx, ok := ResolvePositionPath(doc, 7, 3)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosKey {
		t.Fatalf("Kind = %v, want PosKey", ctx.Kind)
	}
	if ctx.Path != "spec" {
		t.Errorf("Path = %q, want spec", ctx.Path)
	}
	want := map[string]bool{"dataset": true, "title": true}
	if len(ctx.PresentKeys) != 2 || !want[ctx.PresentKeys[0]] || !want[ctx.PresentKeys[1]] {
		t.Errorf("PresentKeys = %v, want dataset+title", ctx.PresentKeys)
	}
}

func TestResolvePositionPath_FirstKeyUnderEmptySpec(t *testing.T) {
	// A blank indented line under a still-empty `spec:` is the first spec key,
	// not a new root key; an unindented one IS a new root key.
	const doc = `kind: Table
metadata:
  name: t
spec:

`
	ctx, ok := ResolvePositionPath(doc, 5, 3)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosKey || ctx.Path != "spec" {
		t.Errorf("indented: Kind=%v Path=%q, want PosKey at spec", ctx.Kind, ctx.Path)
	}
	ctx, ok = ResolvePositionPath(doc, 5, 1)
	if !ok {
		t.Fatal("ok=false at col 1")
	}
	if ctx.Kind != PosKey || ctx.Path != "(root)" {
		t.Errorf("unindented: Kind=%v Path=%q, want PosKey at (root)", ctx.Kind, ctx.Path)
	}
}

func TestResolvePositionPath_NewSequenceItemPath(t *testing.T) {
	// A fresh sequence slot's path is the ELEMENT path (parent + index), not a
	// duplicated field segment — it is the schema resolver's lookup key.
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
  scenarios:
    - ac1

`
	ctx, ok := ResolvePositionPath(doc, 8, 5)
	if !ok {
		t.Fatal("ok=false")
	}
	if ctx.Kind != PosScenarioItem {
		t.Fatalf("Kind = %v, want PosScenarioItem", ctx.Kind)
	}
	if ctx.Path != "spec.scenarios.1" {
		t.Errorf("Path = %q, want spec.scenarios.1", ctx.Path)
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
	// An empty buffer is a fresh document: a root key position, so completion
	// can offer the manifest skeleton instead of going dark.
	if ctx, ok := ResolvePositionPath("", 1, 1); !ok || ctx.Kind != PosKey || ctx.Path != "(root)" {
		t.Errorf("empty content should resolve as a root key position, got ok=%v ctx=%+v", ok, ctx)
	}
	if _, ok := ResolvePositionPath("\t bad: : :\n", 1, 1); ok {
		// malformed YAML in the cursor's own document → ok=false
		t.Error("unparseable content should yield ok=false")
	}
	// A cursor far past EOF still resolves into the single document (clamped),
	// which is acceptable; assert it does not panic.
	_, _ = ResolvePositionPath(doc, 99, 1)
}

// TestResolvePositionPath_BrokenSiblingDocIsolated: a syntax error in one
// document must not kill resolution in the others — previously ANY broken doc
// darkened completion for the whole file.
func TestResolvePositionPath_BrokenSiblingDocIsolated(t *testing.T) {
	const doc = `kind: Table
metadata:
  name: broken
spec: [
---
kind: Table
metadata:
  name: fine
spec:
  dataset: sales
`
	// Cursor on `sales` in the healthy second document.
	ctx, ok := ResolvePositionPath(doc, 10, 12)
	if !ok {
		t.Fatal("a broken sibling document must not kill resolution in a healthy one")
	}
	if ctx.Kind != PosDatasetRef || ctx.Prefix != "sales" {
		t.Fatalf("Kind=%v Prefix=%q, want PosDatasetRef on sales", ctx.Kind, ctx.Prefix)
	}
	// The cursor inside the broken document itself still bails.
	if _, ok := ResolvePositionPath(doc, 4, 8); ok {
		t.Error("the broken document's own positions should still yield ok=false")
	}
}

// TestResolvePositionPath_HealthyPrefixDocStaysResolvable: when a LATER doc is
// broken, the parsed prefix documents keep working with absolute positions.
func TestResolvePositionPath_HealthyPrefixDocStaysResolvable(t *testing.T) {
	const doc = `kind: Table
metadata:
  name: fine
spec:
  dataset: sales
---
kind: Table
spec: [
`
	ctx, ok := ResolvePositionPath(doc, 5, 12)
	if !ok {
		t.Fatal("a broken later document must not affect the healthy first one")
	}
	if ctx.Kind != PosDatasetRef || ctx.Prefix != "sales" || ctx.DocIndex != 0 {
		t.Fatalf("Kind=%v Prefix=%q DocIndex=%d, want PosDatasetRef sales in doc 0", ctx.Kind, ctx.Prefix, ctx.DocIndex)
	}
}

// TestResolvePositionPath_BareScalarRoot: typing the first word of a fresh
// manifest ("kin") resolves as a root key position carrying the prefix.
func TestResolvePositionPath_BareScalarRoot(t *testing.T) {
	ctx, ok := ResolvePositionPath("kin", 1, 4)
	if !ok {
		t.Fatal("a bare scalar root should resolve")
	}
	if ctx.Kind != PosKey || ctx.Path != "(root)" || ctx.Prefix != "kin" {
		t.Fatalf("got %+v, want a (root) PosKey with prefix kin", ctx)
	}
	if ctx.ReplaceRange.StartCol != 1 || ctx.ReplaceRange.EndCol != 4 {
		t.Errorf("ReplaceRange = %+v, want the typed span 1..4", ctx.ReplaceRange)
	}
}

// TestResolvePositionPath_FreshDocAfterSeparator: a cursor below a trailing
// `---` is a NEW document's root key position with the next DocIndex.
func TestResolvePositionPath_FreshDocAfterSeparator(t *testing.T) {
	const doc = `kind: Table
metadata:
  name: t
spec:
  dataset: sales
---
`
	ctx, ok := ResolvePositionPath(doc, 7, 1)
	if !ok {
		t.Fatal("a fresh slice after --- should resolve")
	}
	if ctx.Kind != PosKey || ctx.Path != "(root)" {
		t.Fatalf("got Kind=%v Path=%q, want a (root) PosKey", ctx.Kind, ctx.Path)
	}
	if ctx.DocIndex != 1 {
		t.Errorf("DocIndex = %d, want 1 (the new document)", ctx.DocIndex)
	}
}
