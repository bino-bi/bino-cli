---
name: bino-packaging
description:
  Author, check and publish a reusable bino registry package (a predef project) through the bino MCP
  server - scaffold it with init_bundle, fill the [package] table, name and parameterize its
  definitions, validate, run registry_publish_check, and hand the irreversible publish to the human.
  Use whenever the human wants to create a package, turn components into a package, release a new
  version of one, or asks why the registry rejected it. Consuming packages is in bino-authoring.
---

# Authoring and publishing bino packages

A **package** is an ordinary bino project whose `bino.toml` carries a `[package]` table. It ships a
set of predef documents (components, styles, assets, data) under `@scope/name`. Other projects add it
with `registry_add` and reference its documents by name. Read `bino-concepts` first.

## Create

- **New package:** `init_bundle(source: "predef", name: "<package-name>", set: {Scope,
  Description, Visibility, Tags, Category})`. `name` is only the part after the slash and `Scope` has no `@` (`@acme/finance-kit` →
  `name: "finance-kit"`, `Scope: "acme"`). `Tags` is comma-separated. Ask the human for the scope,
  visibility and license; never guess them. `init_bundle` does not take the license: set
  `license = "..."` in the `[package]` table of `bino.toml` afterwards. The scaffold is a working
  example: a table, a style, a logo asset and a `mocks/` folder.
- The MCP server only serves its own root. Scaffold in place when the root is empty. A package in
  another directory needs a new session there; the tools here would keep checking the old project.
- **Existing project:** add a `[package]` table to `bino.toml` by hand. A project may keep its
  `report-id` too.

```toml
[package]
name        = "@acme/finance-kit"   # @scope/name, lowercase a-z0-9_-
description = "IBCS finance report building blocks"
tags        = ["finance", "ibcs"]
category    = "components"
visibility  = "private"             # only used on the first publish
license     = "MIT"                 # SPDX, "UNLICENSED" or "SEE LICENSE IN <file>"
include     = ["components", "styles", "resources"]   # default: canonical folders minus mocks/, reports/
preview     = "components/waterfall.yaml#@acme/finance-kit/waterfall"
```

## Rules the registry enforces

- **Names.** Every shipped document is named `@scope/name` (the main definition, at most one) or
  `@scope/name/<part>`. Rename a document's `metadata.name` and every `ref` to it together.
- **Forbidden kinds.** No `ReportArtefact`, `LiveReportArtefact`, `ScreenshotArtefact`,
  `DocumentArtefact`, `ConnectionSecret` or `SigningProfile`. Nothing under `secrets/` or `signing/`.
  No symlinks.
- **Layout.** At most one directory level (`components/x.yaml`, not `components/a/x.yaml`). Max 50
  files, 1 MB per manifest, 50 MB per resource. Resources: png, jpg, jpeg, webp, gif, csv, xlsx,
  parquet.
- **Refs.** A ref must point into the package or into a package declared in `[dependencies]`.
- **Assets.** No absolute `localPath`.

## Parameters

Make a component reusable with `metadata.params` and `${NAME}` placeholders in its spec. Only
declared params survive to the consumer; any other `${VAR}` is expanded from the environment of
whoever loads the package, so never put a `${VAR}` in a package that the consumer did not ask for.

```yaml
metadata:
  name: "@acme/finance-kit/revenue"
  params:
    - name: REGION
      type: string
      default: EU
spec:
  value: "Revenue ${REGION}"
```

## Preview with mocks

`mocks/` holds sample data and a small report page that renders the package's definitions. It never
ships, so `build` and preview work without a consumer. Keep the artefact there, not in an included
folder.

## Check

1. `validate_project()` - with a `[package]` table it also runs the package rules
   (`package-config-invalid`, naming, forbidden kinds, external refs). Fix every finding.
2. `registry_auth_status()` - if not authenticated, stop: the human runs `bino registry login`. Never
   try to obtain a token.
3. `registry_publish_check(bump?)` - the registry's own gate, as a dry run. It mints nothing and is
   safe to repeat. With `bump`, the result names the version a publish would mint. When it rejects,
   fix the findings in `output`; they are the authority, not the local lint. A gap between the
   registry's bino/engine version and the local one explains "valid here, invalid there".

## Publish (the human decides)

Publishing is **irreversible**: a version is immutable and cannot be deleted. Before any publish,
show the human the package name, the file list from the check, the bump and, on a first publish, the
visibility. Then:

- If the server offers `registry_publish`, call it only after the human said yes to exactly that
  publish: `registry_publish(bump, visibility?)`.
- Otherwise give the human the command: `bino publish --bump <patch|minor|major>`. The tool only
  exists when the human started the server with `bino mcp --allow-publish`, or the daemon with
  `bino daemon --mcp-allow-publish` when `bino mcp` proxies to it.

**Bump:** `patch` for fixes that keep every name and param, `minor` for new definitions or new params
with defaults, `major` for a removed or renamed definition, a removed param, or a param without a
default. The first publish is always 1.0.0. Republishing unchanged content returns `unchanged=true`;
that is not an error.

**Visibility** only counts on the first publish (default private). Never choose `public` without the
human's explicit word. Later changes happen in the registry web UI.

## Release a new version

Edit the documents, `validate_project()`, `registry_publish_check(bump)`, then publish as above.
Consumers get it with `registry_update` (tag-following dependencies) or by adding a new pinned
version.
