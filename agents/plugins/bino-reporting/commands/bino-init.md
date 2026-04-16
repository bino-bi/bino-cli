---
description: Scaffold a new bino report bundle (bino init wrapper)
argument-hint: "[directory] [--name name] [--title title] [--language en|de] [-y] [--force]"
allowed-tools: [Bash, Read, Glob]
---

# /bino-init

Scaffold a new bino report bundle in the current working directory (or
the given subdirectory). Wraps `bino init`.

Arguments forwarded as-is: `$ARGUMENTS`.

## Behavior

Run `bino init` non-interactively when called from Claude Code (no TTY
for the wizard). If the user passed `-y` or any `--name` / `--title` /
`--language` flag, just forward. Otherwise, **always pass `-y`** and a
sensible default `--directory` so the wizard doesn't hang.

```bash
bino init -y $ARGUMENTS
```

## What gets created

A directory containing:

```
bino.toml             # report-id (uuid) + engine-version
report.yaml           # ReportArtefact (sample)
data.yaml             # DataSource (inline sample data)
pages.yaml            # LayoutPage with a sample Text component
.bnignore             # files to skip during scanning
.gitignore
```

## After scaffolding

1. `cd` into the new directory.
2. Run `/bino-lint` to confirm the scaffold is valid.
3. Run `/bino-build` to produce the first PDF in `dist/`.
4. Or run `/bino-preview` to author with hot reload.

If the user wants to extend the scaffold (add a real DataSource, a
chart, etc.), the **bino-author** skill is the right next step.
