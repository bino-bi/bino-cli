---
description: Create a reusable bino registry package, or turn the current project into one.
argument-hint: "[@scope/name]"
---

Create a bino package (a predef project). Arguments (all optional): **$ARGUMENTS**

Follow `bino-packaging`. This MCP server only serves its own project root.

1. `describe_project()`. If the root already holds a bino project, ask the human: turn this project
   into a package, or create a new package in a separate directory **outside** this project?
2. Ask for whatever the arguments do not give: scope, package name, description, visibility
   (default private) and license. Never pick `public` or a scope on your own.
3. **Empty root:** `init_bundle(source: "predef", name, set: {Scope, Description, Visibility, Tags,
   Category})` scaffolds in place. `name` is only the part after the slash and `Scope` has no `@`:
   for `@acme/finance-kit` pass `name: "finance-kit", set: {Scope: "acme", ...}`. Then set `license = "<value>"` in the `[package]` table of
   `bino.toml` (the template only has it commented out; `init_bundle` does not take it).
   **Existing project:** add the `[package]` table to `bino.toml`, rename the documents to ship to
   `@scope/name/<part>` (and every `ref` to them), move artefacts and sample data to `mocks/`, and set
   `include`.
   **Separate directory:** call `init_bundle` with an absolute directory outside this project, set the
   license there, then stop: tell the human to open a new session in that directory. Do not run
   `validate_project` or `/bino:publish` here; they would check the current project.
4. `validate_project()` and fix the package findings.
5. Suggest next steps: author the components (`bino-authoring`), then `/bino:publish`.
