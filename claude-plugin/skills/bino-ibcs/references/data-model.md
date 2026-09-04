# Engine data model — reference for the bino-ibcs skill

> Excerpt **copied from the bn-template-engine repository**, file `doc/data-model.md`, at commit
> `1ff628c`. The engine is the renderer bino drives; this is the row shape its components read.
> **Refresh this file when the engine's data model changes.** Loaded on demand; the skill body
> (`../SKILL.md`) is the always-in-context rubric. The public summary lives at
> https://cli.bino.bi/concepts/data-model/ and https://cli.bino.bi/reference/dataset/.

## Overview

Each row in a dataset represents a data point with:

- **Dimensions** — categorical fields for grouping and labeling (`category`, `rowGroup`, …)
- **Scenarios** — numeric values for different business scenarios (actual, plan, forecast, previous period)
- **Operation** — whether the row adds (`"+"`) or subtracts (`"-"`) in aggregations

## Scenarios

Four scenario families, each with up to four slots:

| Family | Slots | Description | IBCS visual |
| --- | --- | --- | --- |
| **Actual (AC)** | `ac1`, `ac2`, `ac3`, `ac4` | Realized values | Solid dark bars |
| **Previous Period (PP)** | `pp1`, `pp2`, `pp3`, `pp4` | Prior period comparison | Light gray bars |
| **Forecast (FC)** | `fc1`, `fc2`, `fc3`, `fc4` | Projected values | Hatched bars |
| **Plan (PL)** | `pl1`, `pl2`, `pl3`, `pl4` | Target / budget values | Outlined bars |

All scenario fields are `number | null`. In the engine's queries they are aggregated with the
operation sign — `SUM(ac1 * sign(operation))` where `"+"` is `1` and `"-"` is `-1`:

```json
[
  { "category": "Product Sales", "operation": "+", "ac1": 150000, "pp1": 140000, "pl1": 145000 },
  { "category": "Discounts",     "operation": "-", "ac1": 12000,  "pp1": 10000,  "pl1": 11000 },
  { "category": "Services",      "operation": "+", "ac1": 90000,  "pp1": 85000,  "pl1": 88000 }
]
```

## Variances

Variance columns are computed from scenario pairs and follow one naming pattern:

```
{type}{base}_{delta}_{direction}
```

| Part | Values | Meaning |
| --- | --- | --- |
| `type` | `d` or `dr` | `d` = absolute difference, `dr` = relative (percentage) |
| `base` | `ac1`..`pl4` | The primary scenario (minuend) |
| `delta` | `ac1`..`pl4` | The comparison scenario (subtrahend) |
| `direction` | `pos`, `neg`, `neu` | Favorable, unfavorable, or neutral when the difference is positive |

Examples:

| Variance | Meaning |
| --- | --- |
| `dac1_pl1_pos` | Absolute: AC1 minus PL1, favorable when positive |
| `drac1_pp1_neg` | Relative: (AC1 − PP1) / PP1, unfavorable when positive |
| `dac1_fc1_neu` | Absolute: AC1 minus FC1, neutral (no color coding) |

The `direction` determines the IBCS color coding in charts and tables:

- `pos` — green/teal when the value is favorable (a positive difference is good)
- `neg` — red when the value is unfavorable (a positive difference is bad)
- `neu` — gray, no direction judgment

Components take the variances to display as a list of these tokens (in bino: the component's
`variances` spec field, e.g. `variances: [dac1_pl1_pos, drac1_pl1_pos]`).

## Dimensions

### Row grouping (hierarchical)

Rows can be organized into a three-level hierarchy:

| Field | Type | Description |
| --- | --- | --- |
| `rowGroup` | `string` | Primary grouping (e.g. "Revenue", "Costs") |
| `rowGroupIndex` | `number` | Sort order for row groups |
| `category` | `string` | Dimension within a row group (e.g. "Product Sales") |
| `categoryIndex` | `number` | Sort order for categories |
| `subCategory` | `string` | Detail dimension for drill-down ("thereof" rows) |
| `subCategoryIndex` | `number` | Sort order for subcategories |

```
rowGroup: "Revenue"
├── category: "Product Sales"
│   ├── subCategory: "Electronics"
│   └── subCategory: "Furniture"
└── category: "Services"
```

### Column grouping

For multi-dimensional tables:

| Field | Type | Description |
| --- | --- | --- |
| `columnGroup` | `string` | Column-level grouping |
| `columnGroupIndex` | `number` | Sort order for column groups |
| `columnSubGroup` | `string` | Column sub-grouping |
| `columnSubGroupIndex` | `number` | Sort order for column sub-groups |

### Temporal dimension

| Field | Type | Description |
| --- | --- | --- |
| `date` | `string` | ISO 8601 date string (e.g. `"2024-01-15"`) |

Used by `ChartTime` for time series; its `dateInterval` field controls the aggregation level (year,
quarter, month, …).

### Other fields

| Field | Type | Description |
| --- | --- | --- |
| `setname` | `string` | Dataset identifier (used by structure charts) |
| `operation` | `string` | `"+"` (default) or `"-"` — determines the sign in aggregations |

### Custom fields

Rows may carry additional user-defined fields prefixed with an underscore (e.g. `_region`,
`_product`). They pass through untouched and can be shown by `Table` via its `attributes` field.
Standard visualization queries ignore them.
