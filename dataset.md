# Dataset Schema Documentation

This document describes the dataset row schema used throughout the bn-template-engine for business intelligence visualizations in tables and charts.

## Overview

A dataset is a collection of rows, where each row represents a data point with dimensional attributes (groupings) and measure values (scenarios). The dataset structure supports:

- **Hierarchical row/column groupings** for drill-down and aggregation
- **Multiple scenario values** for comparative analysis (actual vs. plan, current vs. previous period)
- **Temporal dimensions** for time-series visualizations
- **Signed operations** for additive/subtractive value contributions

## Schema Definition

```json
{
  "$id": "https://bino.bi/schemas/dataset.schema.json",
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object"
}
```

---

## Field Reference

### Identification Fields

#### `setname`

| Property | Value          |
| -------- | -------------- |
| Type     | `string`       |
| Required | No             |
| Used By  | StructureChart |

Dataset identifier or name. Primarily used in StructureChart components for metadata identification. Can be used to distinguish between different data sets when multiple are loaded.

#### `date`

| Property | Value                           |
| -------- | ------------------------------- |
| Type     | `string` (ISO 8601 date format) |
| Required | No (required for TimeChart)     |
| Used By  | TimeChart                       |

Temporal dimension for time-series data. Parsed from ISO string format (e.g., `"2024-01-15"`) using Luxon's `DateTime.fromISO()`.

**Usage:**

- Forms the X-axis in time-series visualizations
- Normalized to configurable intervals (DAY, WEEK, MONTH, QUARTER, YEAR)
- Multiple data points on the same normalized date are aggregated via SUM
- Used to determine min/max date ranges for chart scaling

---

### Operation Field

#### `operation`

| Property     | Value          |
| ------------ | -------------- |
| Type         | `string`       |
| Required     | No             |
| Default      | `"+"`          |
| Valid Values | `"+"` or `"-"` |

Defines the mathematical sign applied to scenario values during aggregation.

**How it works:**

- A custom SQL function `transformOperator()` converts the operation to a multiplier:
  - `"+"` → `1`
  - `"-"` → `-1`
- During aggregation: `SUM(scenario * transformOperator(operation))`

**Use case:**
This enables hierarchical data structures where some rows contribute positively (revenue, income) and others contribute negatively (costs, expenses) to totals. For example, in a P&L statement, revenue rows have `operation: "+"` while cost rows have `operation: "-"`.

---

### Row Grouping Fields

These fields define the hierarchical structure for row-based organization.

#### `rowGroup`

| Property | Value    |
| -------- | -------- |
| Type     | `string` |
| Required | No       |

Primary row-level grouping dimension. Used to organize rows into major sections.

**Examples:** `"Revenue"`, `"Operating Costs"`, `"EBIT"`

**Usage:**

- Groups multiple categories under a common header
- Creates GROUP_SUM rows when aggregating
- Used in SQL `GROUP BY` clauses
- Filters data: `WHERE rowGroup = '${rowGroup}'`

#### `rowGroupIndex`

| Property | Value                              |
| -------- | ---------------------------------- |
| Type     | `number`                           |
| Required | No (required if `rowGroup` is set) |

Sort order for row groups. Lower values appear first.

**Important:** Both `rowGroup` and `rowGroupIndex` must be provided together for the grouping to be applied.

---

#### `category`

| Property | Value    |
| -------- | -------- |
| Type     | `string` |
| Required | No       |

Primary data dimension within a row group. This is the main axis for data organization.

**Examples:** `"Product A"`, `"Region North"`, `"Q1 2024"`

**Usage:**

- Primary grouping level in all SQL queries
- Forms the main data axis for charts and table rows
- Used in drill-down queries for "thereOf" functionality
- Labels for chart elements and table row headers

#### `categoryIndex`

| Property | Value                              |
| -------- | ---------------------------------- |
| Type     | `number`                           |
| Required | No (required if `category` is set) |

Sort order for categories within their row group.

---

#### `subCategory`

| Property | Value    |
| -------- | -------- |
| Type     | `string` |
| Required | No       |

Detail dimension for drill-down below the main category.

**Usage:**

- Creates "thereOf" drill rows (detail breakdown beneath main categories)
- Enables hierarchical detail display
- Row type becomes `DRILLELEMENT` when displaying subcategory data

#### `subCategoryIndex`

| Property | Value                                 |
| -------- | ------------------------------------- |
| Type     | `number`                              |
| Required | No (required if `subCategory` is set) |

Sort order for subcategories within their category.

---

### Column Grouping Fields

These fields enable column-level breakdown of measure values.

#### `columnGroup`

| Property | Value    |
| -------- | -------- |
| Type     | `string` |
| Required | No       |

Column-level grouping dimension for breaking down measures.

**Usage:**

- Enables column drilling via "categoryThereOf" configuration
- Groups measurement data by columns
- Used in drill column queries

#### `columnGroupIndex`

| Property | Value                                 |
| -------- | ------------------------------------- |
| Type     | `number`                              |
| Required | No (required if `columnGroup` is set) |

Sort order for column groups.

---

#### `columnSubGroup`

| Property | Value    |
| -------- | -------- |
| Type     | `string` |
| Required | No       |

Sub-grouping level for detailed column breakdown.

#### `columnSubGroupIndex`

| Property | Value                                    |
| -------- | ---------------------------------------- |
| Type     | `number`                                 |
| Required | No (required if `columnSubGroup` is set) |

Sort order for column sub-groups.

---

### Scenario Fields (Measure Values)

The schema supports four parallel sets of scenario values, enabling complex comparative analyses. Each set contains four scenario types.

#### Scenario Types

| Prefix | Name                | Description                                        |
| ------ | ------------------- | -------------------------------------------------- |
| `ac`   | **Actual**          | Current/actual measured values                     |
| `pp`   | **Previous Period** | Values from the prior time period (YoY, MoM, etc.) |
| `fc`   | **Forecast**        | Predicted/forecasted values                        |
| `pl`   | **Plan/Budget**     | Planned or budgeted target values                  |

#### Available Fields

| Set 1 | Set 2 | Set 3 | Set 4 |
| ----- | ----- | ----- | ----- |
| `ac1` | `ac2` | `ac3` | `ac4` |
| `pp1` | `pp2` | `pp3` | `pp4` |
| `fc1` | `fc2` | `fc3` | `fc4` |
| `pl1` | `pl2` | `pl3` | `pl4` |

All scenario fields:
| Property | Value |
|----------|-------|
| Type | `number` |
| Required | No |
| Default | `null` / `undefined` |

#### Aggregation Behavior

Scenario values are aggregated using the `operation` field:

```sql
SUM(scenario * transformOperator(operation)) as scenario,
COUNT(ISNULL(scenario)) as scenario_cnt
```

The `_cnt` suffix field tracks non-null value counts for variance validation. If count is 0, values are set to `NaN` to prevent false calculations.

---

## Variance Scenarios (Computed)

Beyond the base scenarios, the system supports computed variance scenarios following this naming pattern:

```
{type}_{base}_{delta}_{direction}
```

| Component   | Values                             | Description                                              |
| ----------- | ---------------------------------- | -------------------------------------------------------- |
| `type`      | `d`, `dr`                          | `d` = absolute delta, `dr` = relative delta (percentage) |
| `base`      | `ac1-4`, `pp1-4`, `fc1-4`, `pl1-4` | Base scenario for comparison                             |
| `delta`     | `ac1-4`, `pp1-4`, `fc1-4`, `pl1-4` | Comparison scenario                                      |
| `direction` | `pos`, `neg`, `neu`                | Semantic direction (positive/negative/neutral)           |

**Examples:**

- `d_ac1_pl1_pos` → Absolute difference: Actual vs Plan (positive is good)
- `dr_ac1_pp1_neg` → Relative change: Actual vs Previous Period (negative is good, e.g., costs)

**Calculation:**

```sql
-- Absolute delta (d)
SUM((base * transformOperator(operation)) - (delta * transformOperator(operation)))

-- Relative delta (dr)
(base - delta) / delta
```

---

## Data Hierarchy

### Table Component Hierarchy

```
Dataset
├── rowGroup (optional grouping level)
│   ├── GROUPHEAD row
│   ├── category (primary dimension)
│   │   ├── ENTRY row with scenarios [ac1, pp1, fc1, pl1, ...]
│   │   └── subCategory (drill detail)
│   │       └── DRILLELEMENT row
│   └── GROUP_SUM row
└── SUM row (grand total)
```

### Chart Component Hierarchy

```
Dataset
├── AggregationLevel (configured per chart)
│   ├── ROWGROUP: Groups by rowGroup field
│   ├── CATEGORY: Groups by category field
│   └── SUBCATEGORY: Groups by subCategory field
├── Index fields maintain sort order
└── Scenarios provide measure values
```

---

## Row Types (Generated)

During data processing, the system generates different row types:

| Type           | Description                                         |
| -------------- | --------------------------------------------------- |
| `ENTRY`        | Normal data row                                     |
| `GROUPHEAD`    | Group header row (when grouped mode is enabled)     |
| `GROUP_SUM`    | Sum row for a row group                             |
| `SUM`          | Grand total sum row                                 |
| `DRILLELEMENT` | Detail row from thereOf drill-down                  |
| `PARTELEMENT`  | Relative contribution row from partOf configuration |
| `REST`         | Aggregated remaining rows (when limit is applied)   |

---

## Component Usage

### bn-table

Uses `TableDataRow` class. Supports full hierarchical grouping with row/column drilling, thereOf and partOf configurations.

### bn-chart-time

Uses `TimeChartDataRow` class. Requires `date` field. Normalizes dates to intervals and aggregates values per time period.

### bn-chart-structure

Uses `StructureChartDataRow` class. Supports `setname` for identification. Aggregates by configured level (rowGroup, category, or subCategory).

---

## Example Data

```json
[
  {
    "operation": "+",
    "rowGroup": "Revenue",
    "rowGroupIndex": 1,
    "category": "Product Sales",
    "categoryIndex": 1,
    "date": "2024-01-15",
    "ac1": 150000,
    "pp1": 140000,
    "fc1": 145000,
    "pl1": 148000
  },
  {
    "operation": "-",
    "rowGroup": "Costs",
    "rowGroupIndex": 2,
    "category": "Operating Expenses",
    "categoryIndex": 1,
    "subCategory": "Personnel",
    "subCategoryIndex": 1,
    "date": "2024-01-15",
    "ac1": 50000,
    "pp1": 48000,
    "fc1": 51000,
    "pl1": 49000
  }
]
```

---
