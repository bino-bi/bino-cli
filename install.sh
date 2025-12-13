#!/usr/bin/env bash
set -euo pipefail

REPO="bino-bi/bino-cli-releases"
TAG="latest"
INSTALL_DIR="$HOME/.local/bin"
DRY_RUN=0
ASSUME_YES=0

usage() {
  cat <<EOF
Usage: install.sh [--repo owner/repo] [--tag tag|latest] [--install-dir DIR] [--yes] [--dry-run]

Installs the latest bino release asset for this OS/ARCH.
Defaults:
  --repo       ${REPO}
  --tag        ${TAG}
  --install-dir ${INSTALL_DIR}
Options:
  --repo       Override release repo (owner/repo)
  --tag        Tag to install (default: latest)
  --install-dir  Destination directory for the binary
  --yes        Non-interactive (accept prompts)
  --dry-run    Show actions but do not perform installation
EOF
  exit 1
}

if [ $# -gt 0 ]; then
  while [ $# -gt 0 ]; do
    case "$1" in
      --repo)
        [ $# -ge 2 ] || usage
        REPO="$2"
        shift 2
        ;;
      --tag)
        [ $# -ge 2 ] || usage
        TAG="$2"
        shift 2
        ;;
      --install-dir)
        [ $# -ge 2 ] || usage
        INSTALL_DIR="$2"
        shift 2
        ;;
      --yes)
        ASSUME_YES=1
        shift
        ;;
      --dry-run)
        DRY_RUN=1
        shift
        ;;
      -h|--help)
        usage
        ;;
      *)
        echo "Unknown arg: $1" >&2
        usage
        ;;
    esac
  done
fi

confirm() {
  if [ "$ASSUME_YES" -eq 1 ]; then return 0; fi
  printf "%s [y/N]: " "$1"
  read ans || return 1
  case "$ans" in
    y|Y|yes|YES) return 0 ;;
    *) return 1 ;;
  esac
}

UNAME_S="$(uname -s)"
UNAME_M="$(uname -m)"

case "$UNAME_S" in
  Darwin) OS="Darwin" ;;
  Linux) OS="Linux" ;;
  MINGW*|MSYS*|CYGWIN*) OS="Windows" ;;
  *) OS="$UNAME_S" ;;
esac

case "$UNAME_M" in
  x86_64|amd64) ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) ARCH="$UNAME_M" ;;
esac

EXT="tar.gz"
if [ "$OS" = "Windows" ]; then
  EXT="zip"
fi

ASSET_NAME="bino-cli_${OS}_${ARCH}.${EXT}"
CHECKSUM_NAME="checksums.txt"

printf "Repo: %s\n" "$REPO"
printf "Tag: %s\n" "$TAG"
printf "OS: %s ARCH: %s\n" "$OS" "$ARCH"
printf "Expecting asset: %s\n" "$ASSET_NAME"

get_asset_url_via_gh() {
  if command -v gh >/dev/null 2>&1; then
    if [ "$TAG" = "latest" ]; then
      gh release view --repo "$REPO" --json assets -q ".assets[] | select(.name==\"$ASSET_NAME\") | .browser_download_url" 2>/dev/null || true
    else
      gh release view "$TAG" --repo "$REPO" --json assets -q ".assets[] | select(.name==\"$ASSET_NAME\") | .browser_download_url" 2>/dev/null || true
    fi
  fi
}

get_asset_url_via_api() {
  API_URL="https://api.github.com/repos/${REPO}/releases"
  if [ "$TAG" = "latest" ]; then
    API_URL="${API_URL}/latest"
  else
    API_URL="${API_URL}/tags/${TAG}"
  fi

  if command -v curl >/dev/null 2>&1 && command -v python3 >/dev/null 2>&1; then
    curl -sSL "$API_URL" | python3 -c "import sys,json
j=json.load(sys.stdin)
assets=j.get('assets',[])
for a in assets:
  if a.get('name')=='$ASSET_NAME':
    print(a.get('browser_download_url'))
    sys.exit(0)
sys.exit(1)"
  else
    return 1
  fi
}

get_checksums_url() {
  API_URL="https://api.github.com/repos/${REPO}/releases"
  if [ "$TAG" = "latest" ]; then
    API_URL="${API_URL}/latest"
  else
    API_URL="${API_URL}/tags/${TAG}"
  fi
  if command -v curl >/dev/null 2>&1 && command -v python3 >/dev/null 2>&1; then
    curl -sSL "$API_URL" | python3 -c "import sys,json
j=json.load(sys.stdin)
assets=j.get('assets',[])
for a in assets:
  if a.get('name')=='$CHECKSUM_NAME':
    print(a.get('browser_download_url'))
    sys.exit(0)
sys.exit(1)"
  else
    return 1
  fi
}

ASSET_URL="$(get_asset_url_via_gh || true)"
if [ -z "$ASSET_URL" ]; then
  ASSET_URL="$(get_asset_url_via_api || true)"
fi

if [ -z "$ASSET_URL" ]; then
  echo "Could not find release asset $ASSET_NAME in $REPO (tag: $TAG)." >&2
  exit 2
fi

CHECKSUMS_URL="$(get_checksums_url || true)"

TMPDIR="$(mktemp -d)"
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

ARCHIVE_PATH="$TMPDIR/$ASSET_NAME"

if [ "$DRY_RUN" -eq 1 ]; then
  printf "[dry-run] curl -sL -o '%s' '%s'\n" "$ARCHIVE_PATH" "$ASSET_URL"
else
  echo "Downloading $ASSET_NAME ..."
  curl -sL -o "$ARCHIVE_PATH" "$ASSET_URL"
fi

if [ -n "$CHECKSUMS_URL" ]; then
  CHECKSUMS_PATH="$TMPDIR/$CHECKSUM_NAME"
  if [ "$DRY_RUN" -eq 1 ]; then
    printf "[dry-run] curl -sL -o '%s' '%s'\n" "$CHECKSUMS_PATH" "$CHECKSUMS_URL"
  else
    echo "Downloading $CHECKSUM_NAME ..."
    curl -sL -o "$CHECKSUMS_PATH" "$CHECKSUMS_URL"
  fi

  EXPECTED_SHA="$(grep "${ASSET_NAME}" "$CHECKSUMS_PATH" 2>/dev/null | awk '{print $1}' || true)"
  if [ -z "$EXPECTED_SHA" ]; then
    EXPECTED_SHA="$(grep "${ASSET_NAME}" "$CHECKSUMS_PATH" 2>/dev/null | cut -d' ' -f1 || true)"
  fi

  if [ -n "$EXPECTED_SHA" ]; then
    if [ "$DRY_RUN" -eq 1 ]; then
      printf "[dry-run] verify checksum for '%s'\n" "$ASSET_NAME"
    else
      echo "Verifying checksum..."
      if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL_SHA="$(sha256sum "$ARCHIVE_PATH" | awk '{print $1}')"
      else
        ACTUAL_SHA="$(shasum -a 256 "$ARCHIVE_PATH" | awk '{print $1}')"
      fi
      if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
        echo "ERROR: checksum mismatch for $ASSET_NAME" >&2
        echo "expected: $EXPECTED_SHA" >&2
        echo "actual:   $ACTUAL_SHA" >&2
        exit 3
      else
        echo "Checksum OK."
      fi
    fi
  else
    echo "Warning: checksum for $ASSET_NAME not found in $CHECKSUM_NAME; skipping verification."
  fi
else
  echo "Warning: $CHECKSUM_NAME not found in release; skipping checksum verification."
fi

WORKDIR="$TMPDIR/work"
mkdir -p "$WORKDIR"
if [ "$EXT" = "tar.gz" ]; then
  tar -xzf "$ARCHIVE_PATH" -C "$WORKDIR"
else
  unzip -q "$ARCHIVE_PATH" -d "$WORKDIR"
fi

BIN_PATH="$(find "$WORKDIR" -type f \( -name bino -o -name bino.exe \) | head -n 1 || true)"
if [ -z "$BIN_PATH" ]; then
  echo "ERROR: could not find bino binary inside the archive." >&2
  exit 4
fi

echo "Found binary: $BIN_PATH"
echo "Installing to $INSTALL_DIR"
if [ "$DRY_RUN" -eq 1 ]; then
  printf "[dry-run] mkdir -p '%s' && cp '%s' '%s/' && chmod +x '%s/%s'\n" "$INSTALL_DIR" "$BIN_PATH" "$INSTALL_DIR" "$INSTALL_DIR" "$(basename "$BIN_PATH")"
  exit 0
fi

mkdir -p "$INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ]; then
  if [ "$ASSUME_YES" -eq 0 ]; then
    if confirm "Install requires sudo to write to $INSTALL_DIR. Continue?"; then
      sudo cp "$BIN_PATH" "$INSTALL_DIR/"
      sudo chmod +x "$INSTALL_DIR/$(basename "$BIN_PATH")"
    else
      echo "Aborted by user."
      exit 5
    fi
  else
    sudo cp "$BIN_PATH" "$INSTALL_DIR/"
    sudo chmod +x "$INSTALL_DIR/$(basename "$BIN_PATH")"
  fi
else
  cp "$BIN_PATH" "$INSTALL_DIR/"
  chmod +x "$INSTALL_DIR/$(basename "$BIN_PATH")"
fi

echo "Installation complete."
echo "Installed: $INSTALL_DIR/$(basename "$BIN_PATH")"
echo "Run: $(basename "$BIN_PATH") --version"
