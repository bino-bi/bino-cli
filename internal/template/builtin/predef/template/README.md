# {{ .ReportTitle }}

A predef project scaffolded with `bino init predef`. A predef project is an
ordinary bino project that also authors a reusable registry package: the
`[package]` table in `bino.toml` is the marker, nothing else.

## Naming

Every document this package publishes is named
`{{ .PackageName }}/<definition>`; exactly one document may carry the bare
package name `{{ .PackageName }}`. `bino init` already wrote that prefix into
`bino.toml` and into the `metadata.name` of every document under `components/`,
`styles/` and `resources/`.

## What is in here

| Path | Contents |
| --- | --- |
| `components/revenue_table.yaml` | The kit's `Table`. `spec.dataset` is `${DATASET}`, a required `metadata.params` entry, so a consumer binds it to their own DataSet. |
| `styles/corporate_theme.yaml` | The `ComponentStyle` the table wears. Referenced by its full package name. |
| `resources/logo.{yaml,png}` | An `Asset` with a manifest-relative `localPath`. Absolute paths cannot be published, and a package file may sit at most one directory deep. |
| `mocks/` | Sample data plus a `LayoutPage` and a `ReportArtefact` that render the kit. **Not published** — this is what makes `bino preview` work without a consumer project. |

## Working on it

```
bino lint       # includes the predef rules; expect 0 findings
bino preview    # renders mocks/preview.yaml
```

`bino lint` checks that every published document is namespaced, that no artefact
or credential is inside the package, that asset paths are relative, and that the
package references nothing it cannot reach. `mocks/`, `reports/` and `.bino/` are
never part of the package, so nothing in there is checked.

## Good to know

- A `DataSource` name becomes a DuckDB view name and is limited to two segments,
  so it can never carry a package segment. Keep DataSource manifests in `mocks/`
  or outside the include set.
- Markdown `:ref[Kind:name]` does not accept `@` or `/`, so a namespaced
  definition cannot be referenced from a Markdown document.
