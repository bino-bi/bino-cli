package schema

import "testing"

// TestValidate_PartialRefOverride verifies that a referenced component child may
// override individual spec fields without re-supplying the referenced spec's
// required fields (Issue 1: base/full split).
func TestValidate_PartialRefOverride(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: sales_table
      spec:
        filter: "amount > 0"
`
	if err := Validate([]byte(yaml)); err != nil {
		t.Errorf("partial ref override should pass, got: %v", err)
	}
}

// TestValidate_InlineChildRequiresDataset verifies that an inline child (no ref)
// still requires the spec's mandatory fields.
func TestValidate_InlineChildRequiresDataset(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      spec:
        filter: "amount > 0"
`
	err := Validate([]byte(yaml))
	if err == nil {
		t.Fatal("inline Table child without dataset should fail")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
}

// TestValidate_RefOverrideTypoFails verifies that a typo inside an override spec
// is rejected because the referenced base spec is closed (additionalProperties:false).
func TestValidate_RefOverrideTypoFails(t *testing.T) {
	yaml := `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Table
      ref: sales_table
      spec:
        filterr: "amount > 0"
`
	if err := Validate([]byte(yaml)); err == nil {
		t.Fatal("typo in ref override spec should fail")
	}
}

// TestValidate_TreeNodeRefPartialSpec covers the exact false positive from
// example-reports#86: a tree node (or layout child) referencing a chart
// component may override only individual fields (e.g. filter) without
// re-supplying the referenced spec's required dataset, while inline nodes
// still require it.
func TestValidate_TreeNodeRefPartialSpec(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "tree node ref with filter-only spec accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"category_1"}]'
  nodes:
    - id: category_1
      kind: ChartStructure
      ref: 202_country_chart
      optional: true
      spec:
        filter: rowGroup IN ('europe')
`,
			wantErr: false,
		},
		{
			name: "layout child ref with filter-only spec accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartStructure
      ref: 202_country_chart
      spec:
        filter: rowGroup IN ('europe')
`,
			wantErr: false,
		},
		{
			name: "inline tree node with filter-only spec rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"category_1"}]'
  nodes:
    - id: category_1
      kind: ChartStructure
      spec:
        filter: rowGroup IN ('europe')
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_Strictness covers the additionalProperties:false cases (Issue 2):
// misspelled spec fields, unknown top-level keys, and unknown keys inside a
// ConnectionSecret are all rejected, while valid documents still pass.
func TestValidate_Strictness(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "ChartStructure spec typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: chart
spec:
  dataset: revenue
  scenrios: [ac1]
`,
			wantErr: true,
		},
		{
			name: "unknown top-level key rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: ds
spec:
  query: SELECT 1
foo: bar
`,
			wantErr: true,
		},
		{
			name: "ConnectionSecret s3 valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata:
  name: my_s3
spec:
  type: s3
  s3:
    keyId: AKIA
    secret: shhh
`,
			wantErr: false,
		},
		{
			name: "ConnectionSecret s3 with unknown key rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata:
  name: my_s3
spec:
  type: s3
  s3:
    keyId: AKIA
  foo: bar
`,
			wantErr: true,
		},
		{
			name: "Table selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  selectedStyle: corporate-style
`,
			wantErr: false,
		},
		{
			name: "Table selectedStyle typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  selectedStyl: corporate-style
`,
			wantErr: true,
		},
		{
			name: "LayoutPage selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  selectedStyle: corporate-style
  children:
    - kind: Table
      ref: sales_table
`,
			wantErr: false,
		},
		{
			name: "ReportArtefact selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Report
  selectedStyle: corporate-style
`,
			wantErr: false,
		},
		{
			name: "ReportArtefact selectedStyle typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata:
  name: report
spec:
  filename: report.pdf
  title: Report
  selectedStyl: corporate-style
`,
			wantErr: true,
		},
		{
			name: "inline layout child selectedStyle accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: Text
      spec:
        value: hello
        selectedStyle: corporate-style
`,
			wantErr: false,
		},
		{
			name: "Asset with localPath source valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./logo.png
`,
			wantErr: false,
		},
		{
			name: "Asset source with two variants rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata:
  name: logo
spec:
  type: image
  mediaType: image/png
  source:
    localPath: ./logo.png
    remoteURL: https://example.com/logo.png
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_RuleSet covers the RuleSet kind and the ruleset attribute: valid
// object/string content, closed content and scenario-rule objects, and the
// attribute on the component/layout kinds that support it.
func TestValidate_RuleSet(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "RuleSet with object content valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: RuleSet
metadata:
  name: corporate_rules
spec:
  content:
    scenarios:
      pl:
        name: PLAN
        colorIndex: 50
        sortIndex: 900
    fallback:
      name: Series
      group: Series
      colorIndex: 10
      sortIndex: 120
`,
			wantErr: false,
		},
		{
			name: "RuleSet with string content valid",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: RuleSet
metadata:
  name: corporate_rules
spec:
  content: '{"scenarios": {"pl": {"sortIndex": 900}}}'
`,
			wantErr: false,
		},
		{
			name: "RuleSet without content rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: RuleSet
metadata:
  name: corporate_rules
spec: {}
`,
			wantErr: true,
		},
		{
			name: "RuleSet scenario entry with group rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: RuleSet
metadata:
  name: corporate_rules
spec:
  content:
    scenarios:
      pl:
        group: Series
`,
			wantErr: true,
		},
		{
			name: "RuleSet unknown content key rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: RuleSet
metadata:
  name: corporate_rules
spec:
  content:
    scenarioss:
      pl:
        name: PLAN
`,
			wantErr: true,
		},
		{
			name: "Table ruleset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  ruleset: corporate_rules
`,
			wantErr: false,
		},
		{
			name: "Table ruleset typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Table
metadata:
  name: sales
spec:
  dataset: revenue
  rulesett: corporate_rules
`,
			wantErr: true,
		},
		{
			name: "ChartTime ruleset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata:
  name: chart
spec:
  dataset: revenue
  ruleset: inherited-page
`,
			wantErr: false,
		},
		{
			name: "ChartStructure ruleset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata:
  name: chart
spec:
  dataset: revenue
  ruleset: inherited-closest
`,
			wantErr: false,
		},
		{
			name: "LayoutPage ruleset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  ruleset: corporate_rules
  children:
    - kind: Table
      ref: sales_table
`,
			wantErr: false,
		},
		{
			name: "inline layout child ruleset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: LayoutCard
      spec:
        children:
          - kind: Table
            ref: sales_table
        ruleset: inherited-page
`,
			wantErr: false,
		},
		{
			name: "Text ruleset rejected (unsupported kind)",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Text
metadata:
  name: note
spec:
  value: hello
  ruleset: corporate_rules
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_Scale verifies the tightened scale field (Issue 8): the keywords
// none/auto, positive numbers and positive numeric strings are accepted, while a
// misspelled keyword is rejected.
func TestValidate_Scale(t *testing.T) {
	cases := []struct {
		scale   string
		wantErr bool
	}{
		{"none", false},
		{"auto", false},
		{"0.8", false},   // numeric string (documented form)
		{`"0.8"`, false}, // explicitly quoted numeric string
		{"1.2", false},
		{"atuo", true}, // typo of "auto"
		{"abc", true},
	}
	for _, c := range cases {
		t.Run(c.scale, func(t *testing.T) {
			yaml := "apiVersion: bino.bi/v1alpha1\nkind: Text\nmetadata:\n  name: t\nspec:\n  value: hi\n  scale: " + c.scale + "\n"
			err := Validate([]byte(yaml))
			if c.wantErr && err == nil {
				t.Errorf("scale %q: expected error, got nil", c.scale)
			}
			if !c.wantErr && err != nil {
				t.Errorf("scale %q: expected valid, got: %v", c.scale, err)
			}
		})
	}
}

// TestValidate_XYCharts covers the ChartScatter/ChartBubble spec defs:
// dual-form measure mappings (bare token vs object), the measure-token
// pattern, required properties per kind, and the closed sub-objects.
func TestValidate_XYCharts(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "scatter minimal bare tokens",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
`,
			wantErr: false,
		},
		{
			name: "scatter object-form axes with iso and facet",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x:
    measure: ac1
    label: Margin
    unit: "%"
    min: 0
    max: 40
    refLine: 20
  y:
    measure: ac2
    label: Net sales
    unit: mEUR
    highlight:
      from: 30
  iso:
    values: [100, 200, 300]
    label: Gross profit
    unit: mUSD
    highlight:
      from: 300
  level: category
  seriesLevel: rowgroup
  facet:
    level: rowgroup
    columns: 3
  labels:
    points: auto
    max: 12
  legend:
    show: true
    position: bottom
  aspect: "21:9"
  limit: 50
  scale: none
  ruleset: inherited-page
`,
			wantErr: false,
		},
		{
			name: "scatter variance token with sentiment suffix",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: variance_scatter
spec:
  dataset: product_data
  x: drac1_pl1_pos
  y: dac1_pp1
`,
			wantErr: false,
		},
		{
			name: "scatter iso values auto",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
  iso:
    values: auto
`,
			wantErr: false,
		},
		{
			name: "scatter labels explicit point list",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
  labels:
    points: [RX-2000, X2-200]
    values: false
`,
			wantErr: false,
		},
		{
			name: "scatter invalid scenario slot rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac5
  y: ac2
`,
			wantErr: true,
		},
		{
			name: "scatter malformed variance token rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: dxac1_pp1
  y: ac2
`,
			wantErr: true,
		},
		{
			name: "scatter missing y rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
`,
			wantErr: true,
		},
		{
			name: "scatter spec typo rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
  serieslevel: rowgroup
`,
			wantErr: true,
		},
		{
			name: "scatter facet without level rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
  facet:
    columns: 2
`,
			wantErr: true,
		},
		{
			name: "scatter size rejected (bubble-only property)",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartScatter
metadata:
  name: products
spec:
  dataset: product_data
  x: ac1
  y: ac2
  size: ac3
`,
			wantErr: true,
		},
		{
			name: "bubble minimal bare tokens",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBubble
metadata:
  name: portfolio
spec:
  dataset: business_units
  x: ac1
  y: ac2
  size: ac3
`,
			wantErr: false,
		},
		{
			name: "bubble size object with scaling group and share",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBubble
metadata:
  name: portfolio
spec:
  dataset: business_units
  x: ac1
  y: ac2
  size:
    measure: ac3
    label: Net sales
    unit: mEUR
    group: netsales_area
  share: ac4
  compareWith: pp
`,
			wantErr: false,
		},
		{
			name: "bubble missing size rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBubble
metadata:
  name: portfolio
spec:
  dataset: business_units
  x: ac1
  y: ac2
`,
			wantErr: true,
		},
		{
			name: "bubble invalid compareWith rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBubble
metadata:
  name: portfolio
spec:
  dataset: business_units
  x: ac1
  y: ac2
  size: ac3
  compareWith: py
`,
			wantErr: true,
		},
		{
			name: "bubble iso rejected (scatter-only property)",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBubble
metadata:
  name: portfolio
spec:
  dataset: business_units
  x: ac1
  y: ac2
  size: ac3
  iso:
    values: auto
`,
			wantErr: true,
		},
		{
			name: "scatter ref child override without required axes accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartScatter
      ref: products
      spec:
        chartTitle: Override
`,
			wantErr: false,
		},
		{
			name: "scatter inline child without axes rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartScatter
      spec:
        dataset: product_data
`,
			wantErr: true,
		},
		{
			name: "bubble grid child inline accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata:
  name: overview
spec:
  rowHeaders: [Portfolio]
  columnHeaders: [Actual]
  children:
    - kind: ChartBubble
      row: 0
      column: 0
      spec:
        dataset: business_units
        x: ac1
        y: ac2
        size: ac3
`,
			wantErr: false,
		},
		{
			name: "scatter tree node ref accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"b"}]'
  nodes:
    - id: a
      kind: ChartScatter
      ref: products
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidate_TreeNodeLayoutCard(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "layout card tree node inline accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"b"}]'
  nodes:
    - id: a
      kind: LayoutCard
      spec:
        children:
          - kind: Text
            spec:
              value: Hello
`,
			wantErr: false,
		},
		{
			name: "layout card tree node ref accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"b"}]'
  nodes:
    - id: a
      kind: LayoutCard
      ref: summary_card
`,
			wantErr: false,
		},
		{
			name: "layout card tree node inline without children rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"b"}]'
  nodes:
    - id: a
      kind: LayoutCard
      spec:
        titleBusinessUnit: Sales
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_ChartBullet covers the ChartBullet spec defs: the plain
// scenario-slot pattern (variance tokens rejected), dual-form actual/target
// mappings, the ranges array, the mode enums, and the closed sub-objects.
func TestValidate_ChartBullet(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "bullet minimal dataset only",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
`,
			wantErr: false,
		},
		{
			name: "bullet bare token measures",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  actual: ac1
  target: pl1
`,
			wantErr: false,
		},
		{
			name: "bullet object-form measures with all options",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  chartTitle: KPI overview
  actual:
    measure: ac1
    label: AC
    unit: EUR k
  target:
    measure: pl1
    label: Plan
  ranges: [0.6, 0.9]
  normalize: none
  variances: none
  level: category
  order: ac1
  orderDirection: desc
  limit: 6
  labels:
    show: auto
    decimals: 0
  filter: "rowGroup = 'Revenue'"
  scale: none
  selectedStyle: kpi-style
  ruleset: inherited-page
`,
			wantErr: false,
		},
		{
			name: "bullet missing dataset rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  actual: ac1
`,
			wantErr: true,
		},
		{
			name: "bullet variance token rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  actual: drac1_pl1
`,
			wantErr: true,
		},
		{
			name: "bullet invalid scenario slot rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  target: banana
`,
			wantErr: true,
		},
		{
			name: "bullet invalid normalize rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  normalize: percent
`,
			wantErr: true,
		},
		{
			name: "bullet invalid variances rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  variances: "on"
`,
			wantErr: true,
		},
		{
			name: "bullet invalid level rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  level: series
`,
			wantErr: true,
		},
		{
			name: "bullet ranges with string entry rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  ranges: [0.6, high]
`,
			wantErr: true,
		},
		{
			name: "bullet empty ranges rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  ranges: []
`,
			wantErr: true,
		},
		{
			name: "bullet three ranges rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  ranges: [0.5, 0.7, 0.9]
`,
			wantErr: true,
		},
		{
			name: "bullet unknown property rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  aspect: "16:9"
`,
			wantErr: true,
		},
		{
			name: "bullet target object unknown field rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  target:
    measure: pl1
    refLine: 20
`,
			wantErr: true,
		},
		{
			name: "bullet labels unknown field rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: ChartBullet
metadata:
  name: kpis
spec:
  dataset: kpi_data
  labels:
    show: auto
    points: all
`,
			wantErr: true,
		},
		{
			name: "bullet ref child override without dataset accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartBullet
      ref: kpis
      spec:
        chartTitle: Override
`,
			wantErr: false,
		},
		{
			name: "bullet inline child without dataset rejected",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page
spec:
  children:
    - kind: ChartBullet
      spec:
        actual: ac1
`,
			wantErr: true,
		},
		{
			name: "bullet tree node ref accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata:
  name: drivers
spec:
  edges: '[{"from":"a","to":"b"}]'
  nodes:
    - id: a
      kind: ChartBullet
      ref: kpis
`,
			wantErr: false,
		},
		{
			name: "bullet grid child inline accepted",
			yaml: `
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata:
  name: overview
spec:
  rowHeaders: [KPIs]
  columnHeaders: [Actual]
  children:
    - kind: ChartBullet
      row: 0
      column: 0
      spec:
        dataset: kpi_data
        target: pl1
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

// TestValidate_ChartLevel covers the level enum on ChartStructure and
// ChartTime: it must accept every aggregation level the template engine
// supports (rowGroup/category/subCategory plus index variants, and auto)
// and reject unknown values such as the historic "rowcategory".
func TestValidate_ChartLevel(t *testing.T) {
	kinds := []string{"ChartStructure", "ChartTime"}
	cases := []struct {
		level   string
		wantErr bool
	}{
		{"auto", false},
		{"rowgroup", false},
		{"rowgroupindex", false},
		{"category", false},
		{"categoryindex", false},
		{"subcategory", false},
		{"subcategoryindex", false},
		{"rowcategory", true}, // not an engine level; was wrongly allowed
		{"foo", true},
	}
	for _, kind := range kinds {
		for _, c := range cases {
			t.Run(kind+"/"+c.level, func(t *testing.T) {
				yaml := "apiVersion: bino.bi/v1alpha1\nkind: " + kind + "\nmetadata:\n  name: c\nspec:\n  dataset: d\n  level: " + c.level + "\n"
				err := Validate([]byte(yaml))
				if c.wantErr && err == nil {
					t.Errorf("level %q: expected validation error, got nil", c.level)
				}
				if !c.wantErr && err != nil {
					t.Errorf("level %q: expected valid, got: %v", c.level, err)
				}
			})
		}
	}
}

// TestValidate_Internationalization pins the typed spec.content: built-in tokens
// and free-form keys are both accepted, but values must be strings.
//
// Note what is deliberately NOT rejected: a mistyped token like `global.acl`.
// Free-form keys must stay legal because Text components read `t('my.key')`,
// so the tail is `additionalProperties: {type: string}` and never `false`.
// Tightening that would break the documented Text workflow; catching typos
// belongs in a lint rule, which can name the file and line.
func TestValidate_Internationalization(t *testing.T) {
	doc := func(spec string) string {
		return "apiVersion: bino.bi/v1alpha1\nkind: Internationalization\nmetadata:\n  name: labels\nspec:\n" + spec
	}

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name:    "built-in tokens accepted",
			yaml:    doc("  code: en\n  content:\n    global.ac1: Actual\n    bn-table.there_of: of which\n"),
			wantErr: false,
		},
		{
			name:    "free-form key accepted",
			yaml:    doc("  code: de\n  content:\n    report.title: Umsatzübersicht\n"),
			wantErr: false,
		},
		{
			name:    "mistyped token still accepted (see doc comment)",
			yaml:    doc("  code: en\n  content:\n    global.acl: Actual\n"),
			wantErr: false,
		},
		{
			name:    "JSON string content accepted",
			yaml:    doc(`  code: en` + "\n" + `  content: '{"global.ac1":"Actual"}'` + "\n"),
			wantErr: false,
		},
		{
			name:    "named namespace accepted",
			yaml:    doc("  code: de\n  namespace: audited\n  content:\n    global.ac1: Ist\n"),
			wantErr: false,
		},
		{
			name:    "numeric value rejected",
			yaml:    doc("  code: en\n  content:\n    report.year: 2024\n"),
			wantErr: true,
		},
		{
			// yaml.v3 only treats true/True/TRUE as booleans, so this is the
			// form that actually reaches the validator as a bool.
			name:    "boolean value rejected",
			yaml:    doc("  code: en\n  content:\n    global.ac1: true\n"),
			wantErr: true,
		},
		{
			name:    "nested content rejected",
			yaml:    doc("  code: en\n  content:\n    global:\n      ac1: Actual\n"),
			wantErr: true,
		},
		{
			name:    "missing content rejected",
			yaml:    doc("  code: en\n"),
			wantErr: true,
		},
		{
			name:    "spec key typo rejected",
			yaml:    doc("  code: en\n  contnet:\n    global.ac1: Actual\n"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate([]byte(tt.yaml))
			if tt.wantErr && err == nil {
				t.Errorf("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}
