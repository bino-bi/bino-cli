---
description: Validate bino manifests without building (bino lint wrapper)
argument-hint: "[--work-dir dir] [--execute-queries] [--fail-on-warnings] [--log-format text|json]"
allowed-tools: [Bash, Read]
---

# /bino-lint

Validate manifests in the current bino project. Wraps `bino lint`.

```bash
bino lint $ARGUMENTS
```

## When to add `--execute-queries`

By default `bino lint` validates schema only. With `--execute-queries`
it also runs every `DataSource` and `DataSet` query to catch:

- column-not-found errors before they surface in `bino build`,
- missing `ConnectionSecret` env vars,
- malformed inline data.

Use it before committing or before a build.

## Exit code

Default: `0` unless a fatal load error occurred. Findings are warnings.

For CI gates that should fail on any finding, add `--fail-on-warnings`.

## Output

Human-readable colored output by default. Pass `--log-format json` for
machine consumption — the `bino-debug` skill prefers JSON.

## When this fails

Hand off to the **bino-debug** skill for diagnosis. Don't try to silence
errors with `--no-lint` or higher `BNR_*` limits without understanding
the root cause.
