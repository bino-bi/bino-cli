# Styling, internationalization, signing

Three secondary manifest kinds round out a production-ready bino bundle:

- **ComponentStyle** — themes, typography, colors, chart styling.
- **Internationalization** — translations keyed by language + namespace.
- **SigningProfile** — digital signatures on PDF artefacts.

## ComponentStyle

Reusable visual rules that override bino's defaults for typography and chart styling. Applied globally when present.

```yaml
apiVersion: bino.bi/v1alpha1
kind: ComponentStyle
metadata:
  name: corporateTheme
spec:
  content: {}           # YAML object or JSON-encoded string
```

`spec.content` has these top-level sections:

| Section | Applies to |
|---|---|
| `global` | Font family, size, color, background for all components |
| `bn-text` | Text components (extensible; currently passes through) |
| `bn-chart-time` | Time-series charts (ChartTime) |
| `bn-chart-structure` | Structure/bar charts (ChartStructure) |
| `bn-tree` | Tree diagrams |
| `bn-grid` | Grid layouts |

### Global section

```yaml
spec:
  content:
    global:
      fontSizePx: 14                       # default 13.3333; all scale factors multiply this
      fontFamily: "'Inter', sans-serif"    # default 'Noto Sans', sans-serif
      fontColor: "#1a1a1a"                 # default #000000
      fontBackgroundColor: "#ffffff"       # default #ffffff
```

### Chart sections

Each chart section accepts:

- `scaleFactors` — multipliers applied to `global.fontSizePx` (e.g. `columnWidth: 2.0` → `2.0 × fontSizePx` pixels).
- `axisStyles`, `barStyles`, `varianceStyles` — keyed by scenario code (`ac1`, `pp1`, `fc1`, `pl1`); values are SVG style objects (`fill`, `stroke`, `stroke-width`, `stroke-dasharray`, …).
- `stackColors` — ordered CSS colors for stack segments; default `["#000000", "#c0c0c0", "#808080"]`.
- `padding` — `{ top, right, bottom, left }` in px.

Example — corporate theme with custom structure-chart bars:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ComponentStyle
metadata: { name: corporateTheme }
spec:
  content:
    global:
      fontSizePx: 14
      fontFamily: "'Inter', sans-serif"
      fontColor: "#1a1a1a"
    bn-chart-structure:
      scaleFactors:
        columnWidth: 2.5
        labelToColumn: 0.4
      barStyles:
        ac1: { fill: "#0a3d62", stroke: "#0a3d62" }
        pp1: { fill: "#c8d6e5", stroke: "#8395a7" }
      stackColors: ["#0a3d62", "#38ada9", "#60a3bc"]
      padding: { top: 8, right: 8, bottom: 8, left: 8 }
```

### JSON-string alternative

Useful when pasting from a front-end styling config:

```yaml
spec:
  content: >-
    {"global":{"fontSizePx":14,"fontFamily":"'Inter', sans-serif"}}
```

### Notes

- Only one active `ComponentStyle` applies per artefact context; later definitions override earlier ones. Don't define multiple themes with the same scope.
- Chart scale factors are fractions of `global.fontSizePx`; tweaking `fontSizePx` is the fastest way to up-scale or down-scale an entire report.

---

## Internationalization

Localized messages keyed by language code + optional namespace. Components look up keys via `${t('key')}` in `Text` or `translation: key` in charts/tables. The active language is driven by the consuming artefact's `language` / `locale` field.

```yaml
apiVersion: bino.bi/v1alpha1
kind: Internationalization
metadata: { name: systemTexts_en }
spec:
  code: en-US            # locale code (en-US, de-DE, …)
  namespace: _system     # optional; default namespace is _system
  content:               # object form (YAML map)
    report.title.sales_overview: "Sales Overview"
    report.subtitle.q1_2024: "Q1 2024"
    card.title.revenue: "Revenue"
    card.title.ebit: "EBIT"
```

### JSON-string form

```yaml
apiVersion: bino.bi/v1alpha1
kind: Internationalization
metadata: { name: productTexts_en }
spec:
  code: en-US
  namespace: products
  content: >-
    {"product.applications":"Applications","product.infrastructure":"Infrastructure"}
```

### Patterns

- **Language fallback chain**: when a key is missing in the requested language, the renderer falls back to the default namespace / default language.
- **One file per language is conventional** but not required: you can group by domain (`salesTexts_en`, `salesTexts_de`) or mix everything in one multi-document YAML.
- **Key naming** is free-form, but stick to a consistent hierarchy (`domain.subject.key`) so translators can work on slices.

Lookup in a `Text` manifest:

```yaml
apiVersion: bino.bi/v1alpha1
kind: Text
metadata: { name: header }
spec:
  value: "# ${t('report.title.sales_overview')}"
```

Lookup in a chart / table via `translation:`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ChartStructure
metadata: { name: revenue }
spec:
  dataset: revenue_by_region
  internationalisation: _system        # namespace
  translation: chart.revenue.title
```

---

## SigningProfile

Attaches a digital signature to a PDF artefact. Referenced from `ReportArtefact.spec.signingProfile` or `DocumentArtefact.spec.signingProfile`.

```yaml
apiVersion: bino.bi/v1alpha1
kind: SigningProfile
metadata: { name: corporateSigner }
spec:
  certificate:
    path: ./certs/corporate-cert.pem   # or inline: |  (-----BEGIN CERTIFICATE----- …)
  privateKey:
    path: ./certs/corporate-key.pem
  tsaURL: https://tsa.example.com/tsa  # optional timestamp authority
  digestAlgorithm: sha256              # sha256 | sha384 | sha512
  certType: approval                   # certification | approval | usage-rights | timestamp
  docMDPPerm: form-fill-sign           # no-changes | form-fill-sign | annotate
  signer:
    name: "Group Controlling"
    location: "Headquarters"
    reason: "Approved report"
    contact: "controlling@example.com"
```

### PEM source options

Both `certificate` and `privateKey` accept exactly one of:

```yaml
certificate:
  path: ./certs/cert.pem          # read from file at build time
# or
certificate:
  inline: |
    -----BEGIN CERTIFICATE-----
    MIID...
    -----END CERTIFICATE-----
```

Prefer `path:` for secrets — it keeps credentials out of the repo. Point the path at a file that's written at CI time from a secret store.

### Using a profile

```yaml
apiVersion: bino.bi/v1alpha1
kind: ReportArtefact
metadata: { name: annual_report }
spec:
  filename: annual-report.pdf
  title: "Annual Financial Report"
  signingProfile: corporateSigner
```

### Notes

- `certType` controls the kind of signature annotation embedded: `certification` locks the document to varying degrees; `approval` is a standard signature; `timestamp` is a timestamp-only signature.
- `docMDPPerm` governs what downstream editing is permitted: `no-changes` (most restrictive), `form-fill-sign`, `annotate`.
- Multiple `ReportArtefact`s can reference the same `SigningProfile`. Define each profile once.
