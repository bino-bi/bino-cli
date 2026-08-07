# {{ .ReportTitle }}

A bino report bundle scaffolded with `bino init standard`. Every manifest lives in the
canonical folder for its kind, which is where `bino add` puts new ones.

## Build it

```
bino build      # render the artefacts to dist/
bino preview    # dev server with hot reload
bino serve      # serve the LiveReportArtefact on demand
bino lint       # validate the manifests without building
bino graph      # visualise the dependency graph
```

## What is in here

| Folder | Contents |
| --- | --- |
| `datasources/` | Where data comes from: `new_cities` reads the CSV under `resources/data/`, `{{ .DataSourceName }}` is an inline example. |
| `datasets/` | SQL over the datasources. `revenue_by_city` aggregates the CSV; `{{ .DataSetName }}` is a passthrough. |
| `pages/` | `LayoutPage` documents. `{{ .LayoutName }}` holds the IBCS table the report renders; `welcome-page` is a narrative `Text` page you can open in preview. |
| `components/` | Reusable visuals — `example_chart` is a `ChartStructure` referenced from `docs/`. |
| `reports/` | The deliverables: a PDF (`{{ .ReportName }}`), a served app (`live`), and a Markdown-driven document (`documentation`). |
| `styles/` | `ComponentStyle` documents. `corporateTheme` is applied by the table and the chart. |
| `i18n/` | Label translations. `{{ .Language }}.yaml` matches this report's `language`; add one document per further locale. |
| `resources/` | Non-manifest payloads: the CSV, image and flag `Asset` declarations, signing key placeholders. |
| `docs/` | Markdown chapters compiled into `documentation.pdf` by `reports/document.yaml`. |
| `scripts/` | Example build hook. Run `chmod +x scripts/log_hook.sh` before wiring it into `bino.toml`. |

## Next steps

- Point `datasources/new_cities.yaml` at your own CSV, or run `bino add datasource`.
- Replace the placeholder certificate and key in `resources/signing/` before signing.
- `resources/sql/example.sql` shows the `revenue_by_city` query as standalone SQL.
