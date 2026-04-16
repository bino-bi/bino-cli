---
description: Start the bino preview server with hot reload (bino preview wrapper, runs in background)
argument-hint: "[--port 45678] [--work-dir dir] [--lint] [--log-sql]"
allowed-tools: [Bash, Read]
---

# /bino-preview

Start `bino preview` — a local SSE-driven dev server that hot-reloads
when YAML manifests change. Default port `45678`.

```bash
bino preview $ARGUMENTS
```

## Run in the background

The preview server is long-lived. Always launch it with
`run_in_background=true` so you (Claude) stay free to edit files and
respond to the user.

After starting, tail the captured stdout to confirm the listen URL
(usually `http://127.0.0.1:45678`) and surface it to the user.

## When to use it

- Interactive authoring sessions in Claude Code.
- Quick visual verification of manifest changes.
- Live debugging of layout or styling.

## When *not* to use it

- In Cowork (no browser to view the SSE stream — `bino build` is more
  appropriate).
- In CI (use `bino lint` + `bino build` instead).

## Stopping it

Kill the background shell when you're done — bino has no built-in
"stop" subcommand. The user can also `Ctrl-C` if they own the shell.

## Useful flags

- `--port <N>` — change the listen port.
- `--lint` — re-run lint rules on every refresh (slower, safer).
- `--log-sql` — log every DuckDB query.
- `--data-validation warn|error|ignore` — control how data anomalies
  are surfaced.
