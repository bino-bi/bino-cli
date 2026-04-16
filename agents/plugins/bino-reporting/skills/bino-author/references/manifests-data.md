# Data manifests — DataSource, DataSet, ConnectionSecret

Three manifest kinds handle all data loading and transformation in bino:

- **DataSource** — loads raw data (a CSV file, a DB query result, inline rows). Each becomes a DuckDB view named after `metadata.name`.
- **DataSet** — a SQL or PRQL transformation on top of one or more DataSources/DataSets.
- **ConnectionSecret** — stores credentials (reads from env vars; never embed secrets).

Write any of these in the project workdir. bino discovers every YAML with `apiVersion: bino.bi/v1alpha1` and a matching `kind`.

## Table of contents

- [Naming rules](#naming-rules)
- [DataSource](#datasource)
  - [CSV](#csv-datasource)
  - [Excel](#excel-datasource)
  - [Parquet / JSON](#parquet--json-datasource)
  - [Inline](#inline-datasource)
  - [Postgres / MySQL](#postgres--mysql-datasource)
  - [Sampling](#sampling)
- [DataSet](#dataset)
  - [Inline SQL](#inline-sql)
  - [External SQL file](#external-sql-file-file)
  - [PRQL](#prql)
  - [Pass-through `source:`](#pass-through-source)
  - [Dependencies & inline dependencies](#dependencies--inline-dependencies)
  - [Standard dataset schema for charts/tables](#standard-dataset-schema-for-chartstables)
- [ConnectionSecret](#connectionsecret)
- [Conditional inclusion (`constraints`)](#conditional-inclusion-constraints)
- [Lint rules that apply here](#lint-rules-that-apply-here)

---

## Naming rules

**DataSource `metadata.name`** — must be a valid SQL identifier because it becomes a DuckDB table name:

- Regex: `^[a-z_][a-z0-9_]*$`
- Lowercase letters, digits, underscores only
- First char must be a letter or `_`
- Max 64 chars
- Must **not** start with `_inline_` (reserved)

Examples: `sales_csv`, `orders_pg`, `fact_sales_2024`, `_staging`.
Invalid: `Sales_CSV` (uppercase), `sales-data` (hyphen), `2024_orders` (leading digit).

**DataSet / other kinds** — looser: `^[A-Za-z0-9_]([-A-Za-z0-9_]*[A-Za-z0-9_])?$`. Internal hyphens and mixed case are OK.

When a user hits `invalid datasource name: foo-bar`, the fix is rename to `foo_bar` everywhere (including the SQL `FROM` clauses and any `dependencies:` list).

---

## DataSource

Full envelope:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: sales_data               # SQL identifier pattern
  labels:                        # optional; used in constraint matching
    env: prod
    domain: finance
  description: "Raw sales data"  # optional
  constraints: []                # optional
spec:
  type: csv                      # csv | excel | parquet | json | inline | postgres_query | mysql_query
  path: ./data/sales.csv         # for file types (file, dir, or glob)
  connection: {}                 # for postgres_query / mysql_query
  query: ""                      # for postgres_query / mysql_query
  inline: {}                     # for type: inline
  ephemeral: false               # optional — true = never cached
  sample: 1000                   # optional row sample

  # CSV-only
  delimiter: ","
  header: true
  skipRows: 0
  thousands: ","
  decimalSeparator: "."
  dateFormat: "%Y-%m-%d"
  columnNames: []                # explicit list (mutually exclusive with columns)
  columns: {}                    # name→DuckDB type (mutually exclusive with columnNames)
```

### CSV DataSource

Standard:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: sales_daily }
spec:
  type: csv
  path: ./data/sales_daily/*.csv
```

European format (semicolon delimiter, dot thousands, comma decimal):

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: eu_sales }
spec:
  type: csv
  path: ./data/eu_sales.csv
  delimiter: ";"
  thousands: "."
  decimalSeparator: ","
  dateFormat: "%d/%m/%Y"
```

Headerless with explicit types:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: sensor_data }
spec:
  type: csv
  path: ./data/sensors.csv
  header: false
  columns:
    ts: "TIMESTAMP"
    device_id: "INTEGER"
    reading: "DECIMAL(8,3)"
```

Options map 1:1 to DuckDB's `read_csv`:

| Field | Default | Notes |
|---|---|---|
| `delimiter` | auto | e.g. `";"`, `"|"`, `"\t"` |
| `header` | `true` | First row is header |
| `skipRows` | `0` | Lines to skip before data |
| `thousands` | — | e.g. `"."`, `","` |
| `decimalSeparator` | — | e.g. `","` |
| `dateFormat` | — | DuckDB strftime string |
| `columns` | — | Map of column name → DuckDB type; mutex with `columnNames` |
| `columnNames` | — | Override column names; mutex with `columns` |

### Excel DataSource

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: budget_excel }
spec:
  type: excel
  path: ./data/budget.xlsx
```

### Parquet / JSON DataSource

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: fact_sales_parquet }
spec:
  type: parquet
  path: ./warehouse/fact_sales/*.parquet
```

JSON works the same way with `type: json`.

### Inline DataSource

Useful for KPIs, small lookup tables, or mock data:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: kpi_inline }
spec:
  type: inline
  inline:
    content:
      - { label: "Revenue", value: 123.45 }
      - { label: "EBIT",    value:  12.34 }
```

### Postgres / MySQL DataSource

Full pattern (Postgres — MySQL is identical, `type: mysql_query`):

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: postgresCredentials }
spec:
  type: postgres
  postgres:
    passwordFromEnv: POSTGRES_PASSWORD
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: sales_from_postgres }
spec:
  type: postgres_query
  connection:
    host: ${DB_HOST:db.example.com}
    port: 5432
    database: analytics
    schema: public
    user: reporting
    secret: postgresCredentials
  query: |
    SELECT id, region, booking_date, currency, amount
    FROM fact_sales
    WHERE booking_date >= DATE '2024-01-01'
```

DuckDB then joins this remote view with any other DataSource (CSV, Excel, inline) in downstream DataSets — so you don't need to push all logic into SQL on the remote DB.

### Sampling

For fast preview iteration on huge sources:

```yaml
spec:
  sample: 5000          # absolute row count
# or
spec:
  sample: "10%"         # percentage
# or
spec:
  sample:
    size: 10000
    method: reservoir   # bernoulli | system | reservoir
```

- `bernoulli` — independent row probability, good accuracy
- `system` — whole vector chunks, faster but higher variance
- `reservoir` — exact row count guarantee

Combine with `constraints: [mode!=build]` to sample only in preview (see below).

---

## DataSet

Full envelope:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata:
  name: sales_summary           # pattern ^[A-Za-z0-9_]([-A-Za-z0-9_]*[A-Za-z0-9_])?$
  labels: {}
  description: ""
  constraints: []
spec:
  # exactly ONE of query / prql / source
  query: ""
  prql: ""
  source: ""

  dependencies: []              # DataSource / DataSet names the query depends on
```

### Inline SQL

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: sales_by_region }
spec:
  query: |
    SELECT region, SUM(amount) AS total_amount
    FROM sales_csv
    GROUP BY region
  dependencies:
    - sales_csv
```

### External SQL file (`$file`)

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: sales_summary }
spec:
  query:
    $file: ./queries/sales_summary.sql
  dependencies:
    - sales_csv
```

Paths resolve relative to the manifest file. Changes to the `.sql` file auto-invalidate caches and trigger hot reload in `bino preview`. Use for non-trivial queries — keeps the YAML readable and lets your editor give you SQL syntax highlighting.

### PRQL

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: customer_orders }
spec:
  prql: |
    from orders
    join customers (==customer_id)
    derive full_name = f"{customers.first_name} {customers.last_name}"
    select { order_id, full_name, order_date, total }
    sort {-order_date}
    take 100
  dependencies:
    - orders
    - customers
```

External PRQL files use the same `$file` syntax: `prql: { $file: ./queries/customer_orders.prql }`.

### Pass-through `source:`

When a DataSet just renames or filters a single source:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: sales_passthrough }
spec:
  source: sales_csv              # equivalent to SELECT * FROM sales_csv, but explicit
```

`source`, `query`, and `prql` are mutually exclusive.

### Dependencies & inline dependencies

`spec.dependencies` lists every DataSource or DataSet the query reads from. Two forms:

```yaml
spec:
  dependencies:
    - sales_csv                  # named reference
    - product_dim
    - type: csv                  # inline DataSource — referenced as @inline(0)
      path: ./data/extra.csv
  query: |
    SELECT s.*, p.category
    FROM sales_csv s
    LEFT JOIN product_dim p ON s.product_id = p.id
    UNION ALL
    SELECT date, region, amount FROM @inline(0)
```

Inline sources get referenced by index: `@inline(0)`, `@inline(1)`, …

Always list every name used in the query here. If the query references `foo_csv` but `foo_csv` isn't in `dependencies`, bino can't wire it up.

### Standard dataset schema for charts/tables

When a DataSet feeds `ChartStructure`, `ChartTime`, `Table`, or `Tree`, bino expects a specific column vocabulary. Your query should produce a subset of these:

**Measure columns (IBCS scenarios)** — all nullable:

| Column family | Meaning |
|---|---|
| `ac1`, `ac2`, `ac3`, `ac4` | Actual |
| `pp1`, `pp2`, `pp3`, `pp4` | Prior period (YoY, MoM) |
| `fc1`, `fc2`, `fc3`, `fc4` | Forecast |
| `pl1`, `pl2`, `pl3`, `pl4` | Plan / budget |

**Grouping / dimension columns:**

| Column | Purpose |
|---|---|
| `rowGroup`, `rowGroupIndex` | Top-level grouping (e.g. "Revenue") |
| `category`, `categoryIndex` | Primary dimension (e.g. product name) |
| `subCategory`, `subCategoryIndex` | Detail for drilldown |
| `columnGroup`, `columnGroupIndex` | Column-level grouping |

**Metadata columns:**

| Column | Purpose |
|---|---|
| `date` | ISO 8601 date; required for `ChartTime` |
| `operation` | `"+"` or `"-"` for P&L-style structures |
| `setname` | Dataset identifier when multiple sets overlap |

Example compliant query:

```sql
SELECT
  'Revenue'                                              AS rowGroup,
  1                                                      AS rowGroupIndex,
  product_name                                           AS category,
  ROW_NUMBER() OVER (ORDER BY SUM(sales) DESC)           AS categoryIndex,
  TO_CHAR(booking_date, 'YYYY-MM-DD')                    AS date,
  SUM(sales)                                             AS ac1,
  SUM(budget)                                            AS pl1
FROM sales_data
GROUP BY product_name, booking_date
```

Charts then reference these columns via `scenarios: ["ac1", "pl1"]` and `variances: ["dpl1_ac1_pos"]`.

---

## ConnectionSecret

Full envelope:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: exampleSecret }
spec:
  type: postgres   # postgres | mysql | s3 | gcs | http | r2 | azure | huggingface | webdav
  scope: ""        # optional URL/path prefix to scope this secret
  # exactly one type-specific block:
  postgres: {}
  mysql: {}
  s3: {}
  gcs: {}
  http: {}
  r2: {}
  azure: {}
  huggingface: {}
  webdav: {}
```

Universal rule: **credentials come from env vars via `*FromEnv` fields**, never inline. bino reads them at run time, keeping secrets out of the YAML (and out of version control).

### Postgres

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: postgresCredentials }
spec:
  type: postgres
  postgres:
    passwordFromEnv: POSTGRES_PASSWORD
```

### MySQL

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: mysqlCredentials }
spec:
  type: mysql
  mysql:
    passwordFromEnv: MYSQL_PASSWORD
```

### S3

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: s3Access }
spec:
  type: s3
  scope: s3://my-report-bucket
  s3:
    keyIdFromEnv: AWS_ACCESS_KEY_ID
    secretFromEnv: AWS_SECRET_ACCESS_KEY
    region: eu-central-1
```

### GCS

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: gcsAccess }
spec:
  type: gcs
  gcs:
    keyFromEnv: GCS_SERVICE_ACCOUNT_KEY
```

### HTTP (Bearer)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: httpApi }
spec:
  type: http
  http:
    bearerTokenFromEnv: API_TOKEN
```

### HTTP (Basic)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: httpBasicAuth }
spec:
  type: http
  http:
    usernameFromEnv: HTTP_USER
    passwordFromEnv: HTTP_PASSWORD
```

### HTTP (Proxy)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: httpProxy }
spec:
  type: http
  scope: "https://internal.example.com"
  http:
    httpProxyFromEnv: http_proxy
    httpProxyUsernameFromEnv: http_proxy_username
    httpProxyPasswordFromEnv: http_proxy_password
```

### Hugging Face

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: huggingface }
spec:
  type: huggingface
  huggingface:
    tokenFromEnv: HUGGINGFACE_TOKEN
```

### WebDAV (incl. Hetzner Storage Box)

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: hetznerStorageBox }
spec:
  type: webdav
  scope: storagebox://u123456
  webdav:
    username: u123456
    passwordFromEnv: HETZNER_STORAGEBOX_PASSWORD
```

Then a DataSource can read directly from that scope:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: archived_sales }
spec:
  type: parquet
  path: storagebox://u123456/reports/sales.parquet
```

### Canonical `*FromEnv` field names

- `passwordFromEnv`, `usernameFromEnv`
- `keyIdFromEnv`, `secretFromEnv`, `tokenFromEnv`, `bearerTokenFromEnv`
- `keyFromEnv`
- `httpProxyFromEnv`, `httpProxyUsernameFromEnv`, `httpProxyPasswordFromEnv`

---

## Conditional inclusion (`constraints`)

`metadata.constraints` scopes a document to a particular build mode, artefact, or label state:

```yaml
metadata:
  constraints:
    - mode==preview                 # only loaded in bino preview
    - mode==build                   # only loaded in bino build
    - labels.env==prod
    - spec.format in [a4,letter]
    - artefactKind!=ScreenshotArtefact
```

Operators: `==`, `!=`, `in`, `not-in`. Available fields: `mode`, `labels.<key>`, `spec.<field>`, `artefactKind`.

Typical use: ship a fast sampled DataSource for preview and a full one for build (they share the same `metadata.name`):

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: fact_sales
  constraints: [mode!=build]
spec:
  type: parquet
  path: ./warehouse/fact_sales/*.parquet
  sample: "5%"
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata:
  name: fact_sales
  constraints: [mode==build]
spec:
  type: parquet
  path: ./warehouse/fact_sales/*.parquet
```

---

## Lint rules that apply here

| Rule | Severity | What it catches |
|---|---|---|
| `inline-ref-bounds` | warning | `@inline(N)` out of range |
| `dataset-source-exclusive` | warning | Using more than one of `query` / `prql` / `source` |
| `inline-naming-conflict` | warning | `metadata.name` starting with `_inline_` |
| `missing-required-reference` | **error** | Referenced document doesn't exist |

`bino lint --execute-queries` additionally runs every DataSet query and validates the result shape against the Standard dataset schema above — slow, but worth it before shipping.
