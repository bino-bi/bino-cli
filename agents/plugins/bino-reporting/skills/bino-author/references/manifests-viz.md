# Visualization manifests — ChartStructure, ChartTime, Table, Tree, Grid, ScalingGroup

All visualization manifests consume a DataSet (via `spec.dataset: <name>`) and render it using IBCS conventions. The DataSet must produce the columns the chart expects — see [Standard dataset schema](./manifests-data.md#standard-dataset-schema-for-chartstables).

## IBCS primer

| Scenario code | Meaning | Notes |
|---|---|---|
| `ac1` … `ac4` | Actual | `ac1` is the most recent |
| `pp1` … `pp4` | Prior period | YoY, MoM etc. |
| `fc1` … `fc4` | Forecast | |
| `pl1` … `pl4` | Plan / budget | |

Variance codes follow the pattern `d<B>_<A>_<pos|neg|neu>`:

- `d` = delta
- `<B>_<A>` = scenario B minus scenario A
- `pos` → positive delta shown green (good). `neg` → positive delta shown red (bad). `neu` → neutral.
- `dr<B>_<A>_…` → **relative** variance (percentage), rendered as a thin "pin" needle instead of a bar.

Examples:

- `dpp1_ac1_pos` — prior period minus actual; positive = good (actual up is good).
- `dpl1_ac1_neg` — plan minus actual; positive = bad (missed target).
- `drfc1_ac1_pos` — relative forecast vs actual variance as a percentage.

"Inherited" sentinel values — `inherited-closest` walks up to the nearest parent card/page; `inherited-page` jumps to the owning page — let children reuse the page-level scenarios/variances/order without restating them.

---

## ChartStructure

Categorical horizontal bar chart (ranking, segment breakdown, structure decomposition). Cells always run left-to-right; rows stack top-to-bottom.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata: { name: chart_name }
spec:
  dataset: dataset_name           # required
  chartTitle: ""                  # optional title above chart
  filter: "column = 'value'"      # optional SQL-like filter
  level: category                 # rowcategory | category | subcategory | auto
  order: ac1                      # category | categoryindex | rowgroup | rowgroupindex | ac1-4 | fc1-4 | pp1-4 | pl1-4 | auto | inherited-closest | inherited-page
  orderDirection: asc             # asc | desc | inherited-*
  measureScale: M                 # SI prefix: _ | k | M | G | T | P | E | Z | Y | m | μ | n | p | f | a | z | y | GREATEST | LEAST
  measureUnit: "EUR"
  unitScaling: 0.001              # pixels per unit for bars, or ScalingGroup name
  percentageScaling: 0.5          # pixels per %, or ScalingGroup name
  showCategories: true
  showMeasureScale: true
  limit: 10                       # max bars per group; 0 = unlimited; extras rolled into REST
  scenarios: ["ac1", "pp1"]       # which scenarios to draw
  variances: ["dpp1_ac1_pos"]     # which variance bars/pins
  scale: auto                     # "" | none | auto | numeric factor — handles height overflow
  stack:                          # optional — turns bars into stacks
    by: scenarios                 # scenarios | dimensions
    mode: absolute                # absolute | relative | absolute-relative
    order: dataset                # asc | desc | dataset
```

### Variant: ranking by a scenario (top N)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata: { name: top_products }
spec:
  dataset: revenue_by_product
  chartTitle: "Top 10 products by revenue"
  level: category
  order: ac1
  orderDirection: desc
  scenarios: ["ac1"]
  measureScale: M
  measureUnit: "EUR"
  limit: 10
```

### Variant: actual vs prior with variance

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata: { name: regional_revenue }
spec:
  dataset: revenue_by_region
  chartTitle: "Revenue by region"
  level: category
  order: ac1
  orderDirection: desc
  measureScale: M
  measureUnit: "EUR"
  scenarios: ["ac1", "pp1"]
  variances: ["dpp1_ac1_pos"]
  unitScaling: 0.001
  percentageScaling: 0.5
  scale: auto
```

### Variant: 100% stacked (relative)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata: { name: revenue_share }
spec:
  dataset: revenue_by_segment
  chartTitle: "Revenue share"
  level: category
  scenarios: ["ac1", "pp1", "fc1"]
  stack:
    by: scenarios
    mode: relative
```

### Field notes

- **level**: `rowcategory` (top-level group), `category` (primary dimension), `subcategory` (grandchild). `auto` detects silently.
- **order**: sort key; if you order by a scenario (`ac1` etc.), that scenario must be in `scenarios`.
- **unitScaling**: pixels per data unit. Numeric enables overdrive rendering (zigzag break + extended bar) when a value exceeds container. Alternatively pass a `ScalingGroup` name to sync across components.
- **limit**: rows beyond `limit` are rolled into a synthetic `REST` bar. `0` = unlimited.
- **stack.by: dimensions**: automatically derives the stack column from `level` (category → subcategory, etc.) — use only one scenario.
- **scale**: handles **height** overflow; `"none"` raises overflow warnings, `"auto"` or numeric silences them.

---

## ChartTime

Time-series chart — column or line mode, date axis driven by the dataset's `date` column.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata: { name: chart_name }
spec:
  dataset: dataset_name           # required; must expose a `date` column
  chartTitle: ""
  chartMode: bar                  # bar | line | auto
  maxBars: 28                     # for chartMode: auto — switch to line above this
  lineFullWidth: true             # stretch line mode across container
  dateInterval: auto              # year | quarter | month | week | day | hour | minute | second | millisecond | auto
  axisLabelsMode: smart           # smart | long | short
  filter: ""
  level: category
  order: ac1
  orderDirection: asc
  measureScale: M
  measureUnit: "EUR"
  unitScaling: 0.05
  percentageScaling: 50
  syncSpaceLeft: 80               # px — sync left axis space across multiple time charts on a page
  showCategories: true
  showMeasureScale: true
  showOverlayAvg: true
  showOverlayMedian: false
  limit: 0
  intervalSpanLimit: 50
  scenarios: ["ac1", "fc1"]
  variances: ["dfc1_ac1_pos"]
  scale: auto
  stack: {}                       # same shape as ChartStructure
```

### Variant: monthly line with YoY comparison

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata: { name: revenue_monthly }
spec:
  dataset: revenue_daily
  chartTitle: "Monthly revenue trend"
  chartMode: line
  dateInterval: month
  axisLabelsMode: smart
  measureScale: M
  measureUnit: "EUR"
  scenarios: ["ac1", "pp1"]
  variances: ["dpp1_ac1_pos"]
  showOverlayAvg: true
```

### Variant: auto-switching bar→line

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata: { name: sales_auto }
spec:
  dataset: sales_detail
  chartTitle: "Sales"
  chartMode: auto
  maxBars: 15                     # above 15 data points → line
  dateInterval: day
  scenarios: ["ac1"]
```

### Variant: stacked area

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartTime
metadata: { name: revenue_stacked_area }
spec:
  dataset: revenue_monthly
  chartMode: line
  dateInterval: month
  scenarios: ["ac1", "pp1", "fc1"]
  stack:
    by: scenarios
    mode: absolute
```

IBCS line styling (applied automatically):

- `ac` — black fill, solid black line
- `pp` — gray fill, solid gray line
- `fc` — white fill, dashed black line
- `pl` — white fill, solid black line

---

## Table

Tabular output with optional grouping, scenario columns, and inline variance bars/pins.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Table
metadata: { name: table_name }
spec:
  dataset: dataset_name           # required
  tableTitle: ""
  filter: ""
  order: category
  orderDirection: asc
  measureScale: M
  measureType: currency           # volume | currency
  measureUnit: "EUR"
  categoryWidth: w15              # w5 | w10 | w15 | w20 | w25 | w50 | inf
  dataFormat: decimal             # decimal | percent
  dataFormatDigitsDecimal: 1
  dataFormatDigitsPercent: 2
  grouped: false                  # group rows by `rowGroup`
  showGroupTitle: true
  showMeasureScale: true
  limit: 10
  type: list                      # list | sum | opt | sumnototal | optnototal
  scenarios: ["ac1", "pp1"]
  variances: ["dpp1_ac1_pos"]
  barColumns: []                  # which columns to render as inline bars/pins
  barColumnWidth: w10             # w5 … w100
  unitScaling: 0.01
  percentageScaling: 0.5
  scale: auto
  thereof: []                     # advanced: row drilldown
  partof: []
  columnthereof: null
```

### Variant: flat list

```yaml
apiVersion: bino.bi/v1alpha1
kind: Table
metadata: { name: top_customers }
spec:
  dataset: customer_revenue
  tableTitle: "Top customers"
  type: list
  limit: 10
  scenarios: ["ac1"]
```

### Variant: grouped with inline variance bars

```yaml
apiVersion: bino.bi/v1alpha1
kind: Table
metadata: { name: revenue_grouped }
spec:
  dataset: revenue_by_segment_customer
  tableTitle: "Revenue by segment and customer"
  grouped: true
  showGroupTitle: true
  type: sum
  limit: 5
  scenarios: ["ac1", "pp1"]
  variances: ["dpp1_ac1_pos", "drpp1_ac1_pos"]
  barColumns: ["dpp1_ac1_pos", "drpp1_ac1_pos"]
  barColumnWidth: w20
  unitScaling: 0.02
  percentageScaling: 1
  measureScale: M
  measureUnit: "EUR"
  categoryWidth: w25
```

### `barColumns` — how columns render

- Absolute variance (`d<…>`) → horizontal bar, green (pos) or red (neg).
- Relative variance (`dr<…>`) → thin pin/needle with italic % label.
- Scenario code (`ac1`, `pp1`, `fc1`, `pl1`) → IBCS grayscale bar (AC solid black, PP solid gray, FC hatched, PL outlined).

### Table type semantics

| `type` | Subtotals | Grand total |
|---|---|---|
| `list` | no | no |
| `sum` | yes | yes |
| `opt` | optimized subtotals | yes |
| `sumnototal` | yes | no |
| `optnototal` | optimized subtotals | no |

`grouped: true` requires the dataset to expose `rowGroup`.

---

## Tree

Driver tree / decomposition diagram. Nodes can be plain labels or reuse any chart/table manifest.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata: { name: tree_name }
spec:
  edges:                          # DAG edges
    - from: parent_id
      to: child_id
      operator: "x"               # * | / | + | - | x | ÷ | none
      label: ""                   # optional override
      style: { color: "#000", width: 1 }
  direction: ltr                  # ltr | rtl | ttb | btt
  levelSpacing: 60                # px
  nodeSpacing: 20                 # px
  edgeStyle: orthogonal           # straight | orthogonal | curved
  showOperators: true
  nodes:
    - id: node_id
      kind: Label                 # Label | Table | ChartStructure | ChartTime | Image
      spec: {}                    # inline spec (Label is inline-only)
      ref: ""                     # reference to a standalone manifest (for non-Label kinds)
```

### Example: ROI decomposition

```yaml
apiVersion: bino.bi/v1alpha1
kind: Tree
metadata: { name: roi_driver_tree }
spec:
  direction: ltr
  edgeStyle: orthogonal
  levelSpacing: 80
  showOperators: true
  edges:
    - { from: roi,            to: ros,             operator: "x" }
    - { from: roi,            to: asset_turnover,  operator: "x" }
    - { from: ros,            to: profit,          operator: "/" }
    - { from: ros,            to: revenue }
    - { from: asset_turnover, to: revenue,         operator: "/" }
    - { from: asset_turnover, to: assets }
  nodes:
    - { id: roi,            kind: Label, spec: { value: "<strong>ROI</strong><br/>12.5%" } }
    - { id: ros,            kind: Label, spec: { value: "<strong>ROS</strong><br/>8.3%" } }
    - { id: asset_turnover, kind: Label, spec: { value: "<strong>Asset Turnover</strong><br/>1.5x" } }
    - { id: profit,         kind: Label, spec: { value: "<strong>Profit</strong><br/>50M EUR" } }
    - { id: revenue,        kind: Label, spec: { value: "<strong>Revenue</strong><br/>600M EUR" } }
    - { id: assets,         kind: Label, spec: { value: "<strong>Assets</strong><br/>400M EUR" } }
```

Notes:

- Exactly one root (a node that appears in `from` but never in `to`).
- A node may appear as `to` for multiple parents → DAG, not strict tree.
- `kind: Label` is inline-only (spec.value = sanitized HTML). Other kinds can inline a `spec` or point at a standalone manifest via `ref`.

---

## Grid

Matrix layout — position components at (row, column) intersections with row/column headers.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Grid
metadata: { name: grid_name }
spec:
  chartTitle: "Matrix Title"
  rowHeaders:                     # strings OR {label, id} objects
    - { label: "North", id: north }
    - { label: "South", id: south }
  columnHeaders:
    - { label: "Q1", id: q1 }
    - { label: "Q4", id: q4 }
  showRowHeaders: true
  showColumnHeaders: true
  showBorders: true
  rowHeaderWidth: "120px"
  cellGap: "8px"
  children:
    - { row: north, column: q1, kind: ChartStructure, ref: q1_north_chart }
    - { row: north, column: q4, kind: Table,          spec: { dataset: q4_north, tableTitle: "Q4 North" } }
```

Header format choices:

- Simple strings → IDs auto-assigned 0, 1, 2 … (match children by integer index).
- `{label, id}` → explicit IDs (recommended; stable across refactors).

Cell children specify `row`, `column`, `kind` (Text | Table | ChartStructure | ChartTime | Tree | Image), and either an inline `spec` or a `ref:` name. Cells support `metadata.constraints` for conditional inclusion.

---

## ScalingGroup

Named scaling factor shared across multiple charts/tables — ensures equal pixels-per-unit so visual comparisons are honest.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ScalingGroup
metadata: { name: revenue_unit_scale }
spec:
  value: 0.001                    # pixels per data unit
```

Referenced by any chart/table that accepts numeric `unitScaling` or `percentageScaling`:

```yaml
spec:
  unitScaling: revenue_unit_scale     # lookup by name
  percentageScaling: revenue_pct_scale
```

Use this whenever two or more components need to be visually comparable. For a one-off chart, just use a numeric value directly. The name `auto` is reserved.

---

## Choosing between chart kinds

| Situation | Kind |
|---|---|
| "Compare values across a categorical dimension" | `ChartStructure` |
| "Show a trend over time" (dataset has `date`) | `ChartTime` |
| "Show numbers with optional subtotals, side-by-side scenarios, inline bars" | `Table` |
| "Show decomposition / formula derivation" | `Tree` |
| "Arrange several small charts in a row×col matrix" | `Grid` |
| "Sync scale across charts for fair comparison" | `ScalingGroup` + two chart/tables referencing it |

Charts and tables can co-exist on the same `LayoutPage` (4-up with `pageLayout: 2x2` is a common recipe). Reuse via `LayoutCard` when the same cluster of components appears on many pages — see `manifests-layout.md`.
