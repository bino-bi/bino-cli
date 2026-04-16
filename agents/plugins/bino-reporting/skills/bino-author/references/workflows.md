# End-to-end bino workflows

Self-contained recipes wiring together commands and manifest kinds. Each recipe ends with a runnable `bino build` (or equivalent) and describes where outputs land. Refer to the kind-specific pages for deeper detail:

- `manifests-data.md` — DataSource, DataSet, ConnectionSecret
- `manifests-layout.md` — LayoutPage, LayoutCard, Report/Live/Screenshot/DocumentArtefact
- `manifests-viz.md` — ChartStructure, ChartTime, Table, Tree, Grid
- `commands-reference.md` — CLI flags and `bino.toml`

## Recipe: CSV-backed one-page report

**Goal:** a CSV in → a single-page PDF out, with one chart and one summary table.

```
my-report/
├── bino.toml
├── data.yaml            # DataSource + DataSet
├── pages.yaml           # LayoutPage
├── report.yaml          # ReportArtefact
└── data/
    └── sales.csv        # region,amount,date
```

`bino.toml`:

```toml
report-id = "…uuid…"

[build.args]
out-dir = "dist"
```

`data.yaml`:

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: sales_csv }
spec:
  type: csv
  path: ./data/sales.csv
---
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: monthly_by_region }
spec:
  query: |
    SELECT
      region                                                AS category,
      ROW_NUMBER() OVER (ORDER BY SUM(amount) DESC)         AS categoryIndex,
      strftime(date_trunc('month', date), '%Y-%m-01')       AS date,
      SUM(amount)                                           AS ac1
    FROM sales_csv
    GROUP BY region, date_trunc('month', date)
  dependencies:
    - sales_csv
```

`pages.yaml`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata: { name: sales_dashboard }
spec:
  titleBusinessUnit: "Sales"
  titleScenarios: ["ac1"]
  pageLayout: split-vertical
  pageFormat: a4
  pageOrientation: landscape
  footerDisplayPageNumber: true
  children:
    - kind: ChartStructure
      metadata: { name: region_bars }
      spec:
        dataset: monthly_by_region
        chartTitle: "Monthly revenue by region"
        level: category
        order: ac1
        orderDirection: desc
        scenarios: ["ac1"]
        measureScale: k
        measureUnit: "EUR"
    - kind: Table
      metadata: { name: region_table }
      spec:
        dataset: monthly_by_region
        tableTitle: "Regional totals"
        type: sum
        grouped: false
        scenarios: ["ac1"]
        limit: 20
```

`report.yaml`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: monthly_sales }
spec:
  filename: monthly-sales.pdf
  title: "Monthly Sales"
  format: a4
  orientation: landscape
  language: en
  layoutPages:
    - sales_dashboard
```

Build:

```bash
cd my-report
bino preview          # iterate in browser
bino build            # writes dist/monthly-sales.pdf
```

---

## Recipe: Database-backed report (Postgres)

**Goal:** pull fact data from Postgres, join with a reference CSV, produce a PDF. Credentials come from `DB_PASSWORD` env var.

```yaml
---
# Secrets — never inline the password
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: postgresCredentials }
spec:
  type: postgres
  postgres:
    passwordFromEnv: DB_PASSWORD
---
# Small reference CSV (FX rates)
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: fx_rates }
spec:
  type: csv
  path: ./data/fx_rates.csv
---
# Sales fact pulled from Postgres
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: sales_pg }
spec:
  type: postgres_query
  connection:
    host: ${DB_HOST:reporting-db.internal}
    port: 5432
    database: analytics
    schema: public
    user: reader
    secret: postgresCredentials
  query: |
    SELECT id, region, booking_date, currency, amount
    FROM fact_sales
    WHERE booking_date >= DATE '2024-01-01'
---
# DuckDB joins remote + local sources
apiVersion: bino.bi/v1alpha1
kind: DataSet
metadata: { name: revenue_eur }
spec:
  query: |
    SELECT
      s.region                                             AS category,
      ROW_NUMBER() OVER (ORDER BY SUM(s.amount * f.eur_rate) DESC)
                                                           AS categoryIndex,
      SUM(s.amount * f.eur_rate)                           AS ac1
    FROM sales_pg s
    LEFT JOIN fx_rates f ON s.currency = f.currency
    GROUP BY s.region
  dependencies: [sales_pg, fx_rates]
```

Build with the env var set:

```bash
export DB_PASSWORD='…'
export DB_HOST='reporting-db.internal'
bino build
```

`bino preview` would have warned but still run with an empty password. `bino build` fails fast if `DB_PASSWORD` is unset, which is what you want for production.

---

## Recipe: Multi-region PDF from one page + `ReportArtefact` params

**Goal:** one `LayoutPage` reused for EU, US, APAC — the `ReportArtefact` expands it into three physical pages of the PDF.

`LayoutPage` with params:

```yaml
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: regional_dashboard
  params:
    - name: REGION
      type: string
      required: true
    - name: YEAR
      type: number
      default: "2024"
spec:
  titleBusinessUnit: "Region ${REGION}"
  titleScenarios: ["ac1", "pp1"]
  pageLayout: 2x2
  pageFormat: a4
  pageOrientation: landscape
  children:
    - kind: ChartStructure
      metadata: { name: regional_chart }
      spec:
        dataset: regional_sales
        chartTitle: "Revenue ${REGION} ${YEAR}"
        level: category
        scenarios: ["ac1", "pp1"]
        variances: ["dpp1_ac1_pos"]
        filter: "region = '${REGION}'"       # DuckDB sees the substituted string
```

`ReportArtefact`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: global_regional }
spec:
  filename: global-regions-2024.pdf
  title: "Global Regional Sales 2024"
  format: a4
  orientation: landscape
  language: en
  layoutPages:
    - cover
    - { page: regional_dashboard, params: { REGION: EU,   YEAR: "2024" } }
    - { page: regional_dashboard, params: { REGION: US,   YEAR: "2024" } }
    - { page: regional_dashboard, params: { REGION: APAC, YEAR: "2024" } }
    - appendix-*
```

Notes:

- Parameters substitute into chart/text/filter strings before SQL runs.
- Glob (`appendix-*`) picks up any number of cover / appendix pages without listing them explicitly.
- Produce more regions? Add more `{page, params}` entries — no duplication of the actual page.

---

## Recipe: Interactive dashboard with `LiveReportArtefact`

**Goal:** serve a small web app where users pick a region and year and see the report update.

```yaml
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata: { name: regional_explorer }
spec:
  title: "Regional Sales Explorer"
  routes:
    "/":
      layoutPages:
        - { page: regional_dashboard, params: { REGION: ${REGION}, YEAR: ${YEAR} } }
      title: "Regional Overview"
      queryParams:
        - name: REGION
          type: select
          required: true
          options:
            items:
              - { value: "EU",   label: "Europe" }
              - { value: "US",   label: "North America" }
              - { value: "APAC", label: "Asia Pacific" }
        - name: YEAR
          type: number
          default: "2024"
          options: { min: 2020, max: 2030 }
```

Run:

```bash
bino serve --live regional_explorer --port 8080
# visit http://localhost:8080/?REGION=EU&YEAR=2024
```

`bino serve` renders on demand per request — no file watching. Missing required query params return HTTP 400 with a JSON list of missing fields.

---

## Recipe: Export individual charts as PNG (`ScreenshotArtefact`)

**Goal:** same report bundle also emits retina-resolution PNGs of specific components, for pasting into slides.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata: { name: slide_assets }
spec:
  filenamePrefix: slides
  format: full-hd
  scale: device                      # 2x retina
  imageFormat: png
  filenamePattern: ref               # → slides-<component name>.png
  refs:
    - { kind: ChartStructure, name: region_bars }
    - { kind: Table,          name: region_table }
```

The component must have `metadata.name` on the **inline child** in its `LayoutPage` (see the CSV recipe above — `metadata: { name: region_bars }` on the ChartStructure inside `children`). Run `bino build` and PNGs land next to the PDF in `dist/`.

---

## Recipe: CI/CD pipeline (GitHub Actions)

Minimal workflow to build on every push and upload the PDFs as artifacts.

```yaml
# .github/workflows/report.yml
name: Build report
on:
  push:
    branches: [main]
  workflow_dispatch:

jobs:
  build:
    runs-on: ubuntu-latest
    env:
      CI: "1"                         # disables bino's auto-update check
      DB_PASSWORD: ${{ secrets.DB_PASSWORD }}
    steps:
      - uses: actions/checkout@v4

      - name: Install bino + Chrome shell
        run: |
          /bin/bash -c "$(curl -fsSL https://github.com/bino-bi/bino-cli/releases/latest/download/install.sh)"
          bino setup

      - name: Lint
        run: bino lint --fail-on-warnings

      - name: Build
        run: bino build --out-dir dist --log-format json

      - uses: actions/upload-artifact@v4
        with:
          name: reports
          path: dist/*.pdf
```

Key details:

- Set `CI=1` (or `BINO_DISABLE_UPDATE_CHECK=1`) so bino doesn't try to self-update mid-build.
- Pass secrets through env vars; bino consumes them via `passwordFromEnv` / `${VAR}`.
- `bino lint --fail-on-warnings` turns any warning into a non-zero exit so the pipeline fails early.
- Cache `~/.bino/` between runs to avoid re-downloading Chrome (saves ~30s per run).

---

## Recipe: Multi-artefact build (PDF + PNG + Live)

One bundle, three artefacts, all built together:

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: monthly_pdf }
spec:
  filename: monthly.pdf
  title: "Monthly Report"
  layoutPages: [sales_dashboard]
---
apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata: { name: chart_png }
spec:
  filenamePrefix: monthly
  refs: [{ kind: ChartStructure, name: region_bars }]
---
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata: { name: monthly_live }
spec:
  title: "Monthly Live Dashboard"
  routes:
    "/":
      artefact: monthly_pdf
```

```bash
bino build                               # → dist/monthly.pdf + dist/monthly-region_bars.png
bino build --artefact monthly_pdf        # only the PDF
bino serve --live monthly_live           # live dashboard on port 8080
```

`--artefact` is repeatable; `--exclude-artefact` is the inverse.

---

## Recipe: Signed PDF for compliance

Add digital signature to the PDF for audit/compliance:

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: SigningProfile
metadata: { name: corporateSigner }
spec:
  certificate: { path: ./certs/cert.pem }
  privateKey:  { path: ./certs/key.pem }
  digestAlgorithm: sha256
  certType: approval
  docMDPPerm: form-fill-sign
  signer:
    name: "Group Controlling"
    reason: "Approved monthly report"
    location: "Headquarters"
---
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: monthly_signed }
spec:
  filename: monthly-signed.pdf
  title: "Monthly Report (Signed)"
  layoutPages: [sales_dashboard]
  signingProfile: corporateSigner
```

In CI, write the cert/key files from secret storage just before build:

```bash
echo "$CERT_PEM" > ./certs/cert.pem
echo "$KEY_PEM"  > ./certs/key.pem
chmod 600 ./certs/*.pem
bino build --artefact monthly_signed
```
