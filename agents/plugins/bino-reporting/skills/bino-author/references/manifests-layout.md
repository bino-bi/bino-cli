# Layout manifests — LayoutPage, LayoutCard, ReportArtefact, LiveReportArtefact, ScreenshotArtefact, DocumentArtefact, Asset, Text

Layout manifests compose visualizations into pages and artefacts. A typical report flow:

```
DataSource → DataSet → ChartStructure / Table / Text
                            ↓
                        LayoutCard  (optional, for reuse)
                            ↓
                        LayoutPage
                            ↓
         ReportArtefact (PDF) or LiveReportArtefact (web app) or ScreenshotArtefact (PNG)
```

## Table of contents

- [LayoutPage](#layoutpage)
- [LayoutCard](#layoutcard)
- [ReportArtefact](#reportartefact)
- [LiveReportArtefact](#livereportartefact)
- [ScreenshotArtefact](#screenshotartefact)
- [DocumentArtefact](#documentartefact)
- [Asset](#asset)
- [Text](#text)

---

## LayoutPage

One printable / displayable page. Holds a title bar, a children grid, and an optional footer. Children are charts, tables, text, or reusable `LayoutCard`s.

```yaml
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata:
  name: page_name
  params:                              # optional parameters — referenced as ${NAME} inside spec
    - name: REGION
      type: select                     # string | number | boolean | select | date | date_time
      required: true
      options:
        items:
          - { value: "EU", label: "Europe" }
          - { value: "US", label: "North America" }
    - name: YEAR
      type: number
      default: "2024"
      options: { min: 2020, max: 2030 }
spec:
  # Title row — all optional; these flow down to child charts/tables as defaults via inherited-page
  titleBusinessUnit: ""
  titleNamespace: _system
  titleDateStart: 2024-01-01
  titleDateEnd: 2024-12-31
  titleDateFormat: none                # year | quarter | month | week | day | time | auto | none
  titleDateLink: none                  # avg | interval | cum | start | end | ytd | ytg | mat | none
  titleMeasures:
    - { name: "Revenue", unit: "mEUR" }
  titleScenarios: ["ac1", "fc1"]
  titleVariances: ["dfc1_ac1_pos"]
  titleOrder: category                 # category | categoryindex | rowgroup | rowgroupindex | ac1-4 | fc1-4 | pp1-4 | pl1-4
  titleOrderDirection: asc

  # Page layout
  pageLayout: 2x2                      # full | split-horizontal | split-vertical | 2x2 | 3x3 | 4x4 | 1-over-2 | 1-over-3 | 2-over-1 | 3-over-1 | custom-template
  pageCustomTemplate: ''               # CSS grid-template-areas, e.g. '"a a" "b c"'
  pageGridGap: "8"
  pageFormat: xga                      # xga | hd | full-hd | 4k | 4k2k | a4 | a3 | a2 | a1 | a0 | letter | legal
  pageOrientation: landscape           # landscape | portrait
  pageFitToContent: false
  pageNumber: "1"

  # Footer
  footerDisplayPageNumber: true
  footerText: ""
  messageImage: ""
  messageText: ""

  # Children — order matters (maps to grid cells in predefined layouts)
  children:
    - kind: ChartStructure             # inline component
      metadata: { name: chart1 }
      spec: { dataset: revenue_by_region, level: category, scenarios: ["ac1"] }
    - kind: LayoutCard                 # reference to standalone card
      ref: kpi_card
      spec:                            # optional spec override (deep-merged)
        titleBusinessUnit: "Sales"
```

### Minimal example

```yaml
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata: { name: kpi_overview }
spec:
  titleBusinessUnit: "Controlling"
  titleScenarios: ["ac1", "fc1"]
  pageLayout: 2x2
  pageFormat: a4
  pageOrientation: landscape
  footerDisplayPageNumber: true
  children:
    - kind: ChartStructure
      spec:
        dataset: revenue_data
        chartTitle: "Revenue"
        level: category
        scenarios: ["ac1"]
    - kind: Table
      spec:
        dataset: revenue_data
        tableTitle: "Details"
        type: list
        scenarios: ["ac1"]
```

### Layout grid options

- **Predefined layouts:** `full` (1 cell), `split-horizontal` / `split-vertical` (2 cells), `2x2` / `3x3` / `4x4` (n²), `1-over-2` / `1-over-3` / `2-over-1` / `3-over-1`.
- **Custom template:** set `pageLayout: custom-template` and pass CSS `grid-template-areas`:

  ```yaml
  pageLayout: custom-template
  pageCustomTemplate: '"header header" "chart table" "chart footer"'
  ```

  Children map by order. For named grid areas, use the `metadata.name` of each child to match an area.

### Parameters & `${VAR}` substitution

`metadata.params` defines values the page expects; `${VAR_NAME}` substitutes them in any string in `spec`. For `type: select`, both value and label are accessible:

```yaml
titleBusinessUnit: "Region: ${REGION_LABEL}"       # "Europe"
footerText: "Code: ${REGION}"                      # "EU"
```

### Inherited sentinels

Children can skip `scenarios`/`variances`/`order`/`orderDirection` by setting:

- `inherited-closest` — walk up to the nearest parent (`LayoutCard` or `LayoutPage`).
- `inherited-page` — jump directly to the owning `LayoutPage`.

This keeps card-level scenarios consistent without restating them in every chart.

### Gotchas

- If a child references a dataset that doesn't exist, the page renders with an error marker and lint raises `missing-required-reference`.
- Page format must match `ReportArtefact.format` — mismatched pages are silently filtered out of the artefact.
- `pageFitToContent: true` shrinks the page to its content height — use on cards / auto-sized components; leaves awkward gaps on paginated PDFs.

---

## LayoutCard

A reusable subpage — title + children grid — that drops into multiple `LayoutPage`s. Same shape as `LayoutPage`, minus page-level concerns.

```yaml
apiVersion: bino.bi/v1alpha1
kind: LayoutCard
metadata: { name: card_name }
spec:
  # Title (identical semantics to LayoutPage titleXxx)
  titleImage: "asset_name"
  titleBusinessUnit: ""
  titleScenarios: ["ac1", "fc1"]       # or inherited-page
  titleVariances: ["dfc1_ac1_pos"]
  titleOrder: category
  titleOrderDirection: asc
  titleMeasures: []
  titleDateStart: 2024-01-01
  titleDateEnd: 2024-12-31
  titleDateFormat: quarter
  titleDateLink: interval

  footerText: ""

  cardLayout: full                     # full | split-* | 2x2 | 3x3 | 4x4 | 1-over-2 | custom-template
  cardCustomTemplate: ''
  cardGridGap: "0.5rem"
  cardFitToContent: false
  cardShowBorder: true

  children: []                         # same rules as LayoutPage.children
```

### Pattern: define once, reuse many times

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: LayoutCard
metadata: { name: kpi_card }
spec:
  titleBusinessUnit: "Finance"
  titleScenarios: inherited-page
  titleVariances: inherited-page
  cardLayout: split-horizontal
  children:
    - kind: ChartStructure
      spec:
        dataset: segment_data
        chartTitle: "By Segment"
        level: category
        scenarios: inherited-closest
        variances: inherited-closest
    - kind: Table
      spec:
        dataset: segment_data
        tableTitle: "Details"
        grouped: true
        scenarios: inherited-closest
---
apiVersion: bino.bi/v1alpha1
kind: LayoutPage
metadata: { name: dashboard }
spec:
  titleScenarios: ["ac1", "pp1"]
  titleVariances: ["dpp1_ac1_pos"]
  pageLayout: 2x2
  children:
    - { kind: LayoutCard, ref: kpi_card }
    - { kind: LayoutCard, ref: kpi_card, spec: { titleBusinessUnit: "HR" } }
```

The second instance deep-merges the override: objects merge, arrays replace.

---

## ReportArtefact

Top-level PDF artefact. Selects pages and wires in metadata + optional signing.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: report_name }
spec:
  format: a4                           # xga | hd | full-hd | 4k | 4k2k | a4 | a3 | a2 | a1 | a0 | letter | legal
  orientation: landscape               # landscape | portrait
  language: en                         # en | de
  filename: report.pdf                 # REQUIRED
  title: "Report Title"                # REQUIRED
  description: ""
  subject: ""
  author: ""
  keywords: []

  layoutPages:                         # optional; default ["*"] = all matching pages
    - cover                            # exact name
    - page: regional_dashboard         # parameterized page
      params:
        REGION: EU
        YEAR: "2024"
    - detail-*                         # glob pattern

  signingProfile: corporateSigner      # optional
```

### Minimal example

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: monthly_report }
spec:
  filename: monthly.pdf
  title: "Monthly Sales Report"
  layoutPages:
    - cover-page
    - sales-dashboard
    - appendix-*
```

### Multi-region PDF via parameterized pages

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: global_regional_report }
spec:
  format: a4
  orientation: landscape
  language: en
  filename: global-regions-2024.pdf
  title: "Global Regional Sales 2024"
  author: "Group Controlling"

  layoutPages:
    - cover-page
    - executive-summary
    - page: regional_dashboard
      params: { REGION: EU,   YEAR: "2024" }
    - page: regional_dashboard
      params: { REGION: US,   YEAR: "2024" }
    - page: regional_dashboard
      params: { REGION: APAC, YEAR: "2024" }
    - appendix-*
```

### `layoutPages` pattern semantics

- Exact name (`cover`) — one page.
- Glob (`detail-*`, `appendix-?`) — pages rendered in alphabetical order within the match. Glob only works in **string** form, not the `{page, params}` object form.
- Parameterized (`{page: name, params: {...}}`) — instantiates a page with parameter values; use once per variant.
- Pages with a `format` different from the artefact's `format` are silently filtered out. Match your artefact and page formats.

### Other notes

- `signingProfile` — references a `SigningProfile` manifest; see `styling-i18n.md`.
- Required params without defaults raise an error if not supplied.

---

## LiveReportArtefact

Interactive web application with URL routes and query-param substitution. Served by `bino serve --live <name>`.

```yaml
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata: { name: live_app_name }
spec:
  title: "Application Title"
  description: ""
  routes:
    "/":                               # root route REQUIRED
      artefact: report_name            # XOR with layoutPages
      layoutPages:                     # XOR with artefact
        - page_name
        - page: param_page
          params: { KEY: ${QUERY_VAR} }
      title: ""
      queryParams:
        - name: QUERY_VAR
          type: string                 # string | number | number_range | select | date | date_time
          default: ""
          description: ""
          optional: false
          options:
            items: [{ value: "v1", label: "Label 1" }]
            dataset: data_source       # dynamic options
            valueColumn: col
            labelColumn: display_col
            min: 0
            max: 100
            step: 1
    "/path":
      artefact: another_report
```

### Example: regional explorer with filters

```yaml
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata: { name: regional_explorer }
spec:
  title: "Regional Sales Explorer"
  routes:
    "/":
      layoutPages:
        - page: region_overview
          params:
            REGION: ${REGION}
            YEAR: ${YEAR}
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

Access in URL: `/?REGION=EU&YEAR=2024`.

`${PARAM}` = value; `${PARAM_LABEL}` = label (for `select` types). Missing required params return HTTP 400 with a JSON list of missing fields.

---

## ScreenshotArtefact

PNG / JPEG export of specific components — for presentations, docs, dashboards.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata: { name: screenshot_name }
spec:
  filenamePrefix: prefix               # REQUIRED
  format: full-hd                      # same options as ReportArtefact.format
  orientation: landscape
  language: en
  scale: device                        # css (1x) | device (2x retina)
  imageFormat: png                     # png | jpeg
  quality: 85                          # 1-100, JPEG only
  omitBackground: false                # PNG only
  filenamePattern: ref                 # ref → {prefix}-{name}.{ext}; index → {prefix}-001.{ext}
  layoutPages: []                      # optional; usually omit to let bino auto-discover
  refs:
    - { kind: ChartStructure, name: revenue_chart }
    - { kind: Table,          name: kpi_summary }
    - { kind: LayoutCard,     name: executive_summary }
```

### Example: presentation assets

```yaml
apiVersion: bino.bi/v1alpha1
kind: ScreenshotArtefact
metadata: { name: q4_presentation }
spec:
  filenamePrefix: q4-slides
  format: full-hd
  scale: device
  imageFormat: png
  filenamePattern: index
  refs:
    - { kind: ChartStructure, name: revenue_by_segment }
    - { kind: ChartTime,      name: monthly_trend }
    - { kind: Table,          name: kpi_summary }
```

Output: `q4-slides-001.png`, `q4-slides-002.png`, …

### Gotchas

- Target components MUST have a `metadata.name` on their inline child (so `children: [{ kind: ChartStructure, metadata: {name: revenue_chart}, spec: {...} }]` — not just `children: [{ kind: ChartStructure, spec: {...} }]`).
- `scale: device` (retina / 2x) is recommended for crisp output; `css` (1x) is default but softer.
- If you explicitly set `layoutPages`, page `pageFormat` must match the artefact `format` — mismatch silently excludes.

---

## DocumentArtefact

Narrative PDF from Markdown sources. Good for manuals, policy docs, rendered notebooks.

```yaml
apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata: { name: doc_name }
spec:
  format: a4                           # a4 | a5 | letter | legal
  orientation: portrait
  locale: en
  filename: document.pdf               # REQUIRED
  title: "Document Title"              # REQUIRED
  author: ""
  subject: ""
  keywords: []

  sources:                             # glob-enabled list of .md files (alphabetical order)
    - ./docs/intro.md
    - ./docs/**/*.md
  stylesheet: ./styles/custom.css      # optional

  tableOfContents: false
  tocNumbering: true
  math: true                           # LaTeX $…$ / $$…$$ via KaTeX

  displayHeaderFooter: false
  headerTemplate: "<div>Header</div>"
  footerTemplate: "<div><span class='pageNumber'></span> / <span class='totalPages'></span></div>"
  marginTop: "20mm"
  marginBottom: "15mm"

  signingProfile: ""
  pageBreakBetweenSources: false
```

### Markdown extensions

Inside source Markdown:

- `:ref[ChartStructure:revenue_chart]{caption="Figure 1"}` — embed a component.
- `![Logo](asset:brandLogo)` — resolve an `Asset` by name.
- `$x = \frac{-b}{2a}$`, `$$ … $$` — KaTeX (server-side) when `math: true`.

### Example

```yaml
apiVersion: bino.bi/v1alpha1
kind: DocumentArtefact
metadata: { name: user_manual }
spec:
  format: a4
  filename: user-manual.pdf
  title: "User Manual"
  sources:
    - ./manual/introduction.md
    - ./manual/features/*.md
    - ./manual/troubleshooting.md
  tableOfContents: true
  tocNumbering: true
  displayHeaderFooter: true
  pageBreakBetweenSources: true
```

---

## Asset

Register images, fonts, and files by name so manifests and markdown can refer to them.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata: { name: asset_name }
spec:
  type: image                          # image | file | font
  mediaType: image/png                 # MIME type
  source:
    inlineBase64: ""                   # OR
    localPath: ./path/to/file          # OR
    remoteURL: https://…               # (exactly one)
```

### Examples

```yaml
---
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata: { name: brandLogo }
spec:
  type: image
  mediaType: image/png
  source: { localPath: ./assets/logo.png }
---
apiVersion: bino.bi/v1alpha1
kind: Asset
metadata: { name: corporateFont }
spec:
  type: font
  mediaType: font/woff2
  source: { localPath: ./assets/fonts/corporate.woff2 }
```

Reference inside Markdown: `![Logo](asset:brandLogo)`. Inside layout manifests (e.g. `LayoutPage.spec.messageImage`, `LayoutCard.spec.titleImage`), use the asset name as a string.

---

## Text

Rich text / Markdown block inside a layout.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Text
metadata: { name: text_name }
spec:
  value: "Text content"                # REQUIRED; supports Markdown and templates
  dataset: ""                          # optional; string or [string], exposes ${data.<name>[0].col}
  scale: auto                          # "" | none | auto | numeric factor
```

### Template expressions inside `value`

- `${data.<datasetName>[0].field}` — pull a field from the first row of a DataSet.
- `${t('translation.key')}` — look up an i18n key (`Internationalization` manifest).
- Standard Markdown (bold, italic, lists, links, tables, blockquotes, `asset:…` images) supported.

### Example

```yaml
apiVersion: bino.bi/v1alpha1
kind: Text
metadata: { name: kpi_summary }
spec:
  value: |
    ### Key Performance Indicators

    - **Revenue**: ${data.kpi_summary[0].ac1} EUR
    - **Growth**: ${data.kpi_summary[0].growth}%
    - **Status**: ${t('report.status')}
  dataset: kpi_summary
  scale: auto
```

### Notes

- Template expressions are sandboxed — only `data` and `t()` accessible.
- HTML output is sanitized (safe tags only); event handlers and `javascript:` URLs are stripped.
- `scale: auto` auto-shrinks to fit parent silently. `""` or unset auto-scales with a warning. `"none"` leaves sizing alone and warns on overflow.
