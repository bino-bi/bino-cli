# Environment substitution & secrets

bino expands `${VAR}` placeholders in manifests at load time, pulling
values from process environment variables.

## Syntax

Any string in any manifest supports substitution:

- `${VAR}` — replaced with `$VAR`; blank if unset.
- `${VAR:default}` — replaced with `$VAR`, or `default` if unset. Note: **single colon**, not `${VAR:-default}` (bash style) — bino uses `:`.
- `\${VAR}` — escaped; left as the literal `${VAR}` in output.

Examples:

```yaml
host: "${DB_HOST}"
port: 5432
database: "${DB_NAME:analytics}"
filename: "${REPORT_PREFIX:monthly}-${RUN_DATE}.pdf"
```

## Behavior asymmetry: preview vs. build

This difference is deliberate and worth remembering:

- **`bino preview`** — unresolved vars → log a warning, substitute empty
  string, keep rendering. You can iterate on layout even before wiring
  up secrets.
- **`bino build`** (and **`bino serve`**) — unresolved vars → fail-fast
  with a clear error. Prevents accidentally shipping a report with
  blank SQL or empty paths.

That's why it's fine to scroll through a report without exporting
`DB_PASSWORD`, but you must set it before running the actual build.

## Credentials: use `*FromEnv`, not `${VAR}`

Credentials **never** go in manifests as literal strings *or* `${VAR}`
interpolations. Use a `ConnectionSecret` with a `*FromEnv` field — bino
reads the env var at runtime and keeps the secret out of every log line
and cache entry:

```yaml
apiVersion: bino.bi/v1alpha1
kind: ConnectionSecret
metadata: { name: postgresCredentials }
spec:
  type: postgres
  postgres:
    passwordFromEnv: DB_PASSWORD     # ← correct
    # password: "${DB_PASSWORD}"     ← works but leaks into query logs; don't
```

Then reference the secret from a `DataSource`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: DataSource
metadata: { name: monthly_revenue }
spec:
  type: postgres_query
  connection:
    host: ${DB_HOST:db.local}
    port: 5432
    database: analytics
    user: reader
    secret: postgresCredentials
  query: |
    SELECT date_trunc('month', date) AS month, SUM(amount) AS revenue
    FROM sales
    WHERE date >= '${REPORT_START}'
    GROUP BY 1
    ORDER BY 1
```

Canonical `*FromEnv` field names (see `manifests-data.md` for type-specific
examples):

- `passwordFromEnv`, `usernameFromEnv`
- `keyIdFromEnv`, `secretFromEnv`, `tokenFromEnv`, `bearerTokenFromEnv`
- `keyFromEnv`
- `httpProxyFromEnv`, `httpProxyUsernameFromEnv`, `httpProxyPasswordFromEnv`

`${VAR}` interpolation is right for *values* (host, date ranges, port,
filenames) — anything a human would print without embarrassment.
`*FromEnv` is right for *secrets*.

## `.env` files

bino does **not** auto-load `.env`. Either:

1. Source it before invoking bino:
   ```bash
   set -a && source .env && set +a && bino build
   ```
2. Pass values inline:
   ```bash
   DB_PASSWORD=secret bino build
   ```
3. Use a wrapper (`direnv`, `dotenv-cli`, GitHub Actions
   `${{ secrets.* }}`, etc.).

## `bino.toml` can pre-set env vars per command

If your project always runs with certain env values, set them in
`bino.toml` instead of the shell:

```toml
[build.env]
BNR_MAX_QUERY_ROWS = "100000"
BNR_MAX_QUERY_DURATION_MS = "120000"

[preview.env]
BNR_MAX_QUERY_ROWS = "10000"      # smaller cap for faster preview
```

Override priority (highest wins): process env → `[<cmd>.env]` → defaults.
See `commands-reference.md`.

## LiveReportArtefact query parameters

For `bino serve`, query-string parameters get substituted via the same
`${VAR}` mechanism. Define them in the `LiveReportArtefact`:

```yaml
apiVersion: bino.bi/v1alpha1
kind: LiveReportArtefact
metadata: { name: regional }
spec:
  routes:
    "/":
      layoutPages:
        - { page: regional_page, params: { REGION: ${REGION}, YEAR: ${YEAR} } }
      queryParams:
        - { name: REGION, type: select, required: true, options: { items: [...] } }
        - { name: YEAR,   type: number, default: "2024" }
```

A request to `http://127.0.0.1:8080/?REGION=EU&YEAR=2024` sets
`${REGION}` and `${YEAR}` for that render only. Missing required params
return HTTP 400 with a JSON list of the missing fields.

For `select` types, both value and label are exposed: `${REGION}` = "EU",
`${REGION_LABEL}` = "Europe".

## Don't commit

- Manifest files containing literal credentials.
- `.env` files.
- The `certs/` directory of a `SigningProfile` (write cert/key from CI
  secrets at build time — see the "Signed PDF" recipe in `workflows.md`).

Add these to `.bnignore` (bino won't try to scan them as manifests) **and**
`.gitignore`.
