# syntax=docker/dockerfile:1

# Two targets:
#   --target slim   bino without Chromium — serve, lint, graph, lsp, mcp (~250 MB)
#   --target full   adds Chromium + fonts for build and preview (default, ~900 MB)
#
# Both are fully offline after `docker pull`: the template engine and every
# DuckDB extension the CLI can use are baked into the image.

# --- build -------------------------------------------------------------------
# The builder and the runtime must share a Debian release. duckdb-go-bindings
# ships prebuilt static C++ archives that link -lstdc++ -lm -ldl dynamically, so
# a newer-glibc builder produces a binary bookworm-slim cannot start.
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Dependency layer — only invalidated when go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown

# CGO is mandatory (duckdb-go). The embedded web bundle under internal/web/static
# is committed, so there is no Node or `go generate` step.
# ldflags mirror .github/workflows/release.yml.
RUN CGO_ENABLED=1 go build \
  -ldflags "-s -w \
  -X 'bino.bi/bino/internal/version.Version=${VERSION}' \
  -X 'bino.bi/bino/internal/version.Commit=${COMMIT}' \
  -X 'bino.bi/bino/internal/version.Date=${DATE}'" \
  -o /out/bino ./cmd/bino

# --- slim --------------------------------------------------------------------
FROM debian:bookworm-slim AS slim

LABEL org.opencontainers.image.title="bino" \
  org.opencontainers.image.description="Pixel-perfect PDF reports from YAML manifests and SQL" \
  org.opencontainers.image.source="https://github.com/bino-bi/bino-cli" \
  org.opencontainers.image.licenses="AGPL-3.0-or-later" \
  org.opencontainers.image.vendor="bino.bi"

# tini reaps the Chromium process tree under a long-running `bino serve`.
# curl is only used by HEALTHCHECK.
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  curl \
  tini \
  && rm -rf /var/lib/apt/lists/*

# Every bino cache lives under $HOME/.bino via os.UserHomeDir(), which on Unix
# reads $HOME and never consults /etc/passwd — so an explicit HOME makes the
# baked caches resolve for any runtime UID, including the arbitrary UIDs that
# OpenShift and hardened Kubernetes assign.
ENV HOME=/opt/bino \
  BINO_DISABLE_UPDATE_CHECK=1

RUN useradd --uid 1000 --gid 0 --home-dir "$HOME" --create-home \
  --shell /usr/sbin/nologin bino

COPY --from=builder /out/bino /usr/local/bin/bino

# Bake the offline caches:
#   $HOME/.bino/cdn/bn-template-engine/<version>/
#   $HOME/.bino/cache/duckdb/extensions/v1.4.4/linux_{amd64,arm64}/
# Both runs also set state.SetupCompleted, which suppresses the "Setup required"
# banner bino prints on every command.
#
# ENGINE_VERSION is pinned rather than resolved: the engine's GitHub "latest"
# release skips pre-releases and still points at a 0.x version, which no current
# CLI accepts. Keep this inside engine.SupportedEngineRanges (internal/engine/compat.go),
# and keep it in step with ENGINE_VERSION in .github/workflows/release.yml.
# TestDockerEngineVersionPin (internal/engine/pin_test.go) enforces both rules.
# Note the engine line was renamed alpha -> next after alpha.18; per semver
# pre-release ordering next.N sorts above alpha.19, so the next.* line is what
# satisfies the current floor.
#
# Loading webdavfs prints one "[WebDAV Extension] ..." line from C++ that --quiet
# cannot suppress; it is expected in the build log and is not an error.
#
# The chgrp/chmod share this layer on purpose: group 0 gets the owner's rights so
# an arbitrary UID can still read the caches, and a separate RUN would duplicate
# every cached file into a second layer (~200 MB).
ARG ENGINE_VERSION=v1.0.0-next.23
LABEL bi.bino.engine-version="${ENGINE_VERSION}"
RUN bino setup --template-engine --quiet --engine-version "$ENGINE_VERSION" \
  && bino setup --duckdb-extensions --quiet \
  && mkdir -p /work \
  && chgrp -R 0 "$HOME" /work \
  && chmod -R g=u "$HOME" /work

USER 1000
WORKDIR /work

EXPOSE 8080

# Only meaningful for `bino serve`; a one-shot `bino build` container exits
# before the start period elapses.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/bin/tini", "--", "bino"]
CMD ["--help"]

# --- full (default target) ---------------------------------------------------
FROM slim AS full

USER root

# chrome-headless-shell has no official linux/arm64 build, so the distro package
# is the only option that works on both architectures. Noto and Liberation cover
# the font families the built-in styles ask for.
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  chromium \
  fonts-dejavu-core \
  fonts-liberation \
  fonts-noto-core \
  && rm -rf /var/lib/apt/lists/*

ENV CHROME_PATH=/usr/bin/chromium

USER 1000
