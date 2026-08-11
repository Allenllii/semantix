#!/usr/bin/env bash
# install.sh — install the semantix memory-kernel middleware into this agent's
# environment. Downloads the release binary, verifies the sha256, installs it
# under ~/.local/bin, initializes ~/.semantix/ and runs a smoke test.
#
# Usage: bash <(curl -sL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh)
#        or: bash install.sh [version] [arch]
set -euo pipefail

VERSION="${1:-v0.2.0}"
# arch: auto-detect; override with the second argument (amd64|arm64)
ARCH="${2:-}"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin|linux) ;;
  *) echo "unsupported OS: $OS (darwin|linux)" >&2; exit 2 ;;
esac
if [[ -z "$ARCH" ]]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    arm64|aarch64) ARCH=arm64 ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 2 ;;
  esac
fi
case "$ARCH" in
  amd64|arm64) ;;
  *) echo "invalid arch: $ARCH (amd64|arm64)" >&2; exit 2 ;;
esac
case "$VERSION" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "invalid version: $VERSION (want vX.Y.Z)" >&2; exit 2 ;;
esac

BIN_DIR="${SEMANTIX_BIN_DIR:-$HOME/.local/bin}"
DATA_DIR="${SEMANTIX_DATA_DIR:-$HOME/.semantix}"
BASE="https://github.com/Gnosil/semantix/releases/download/$VERSION"
EXE="semantix-$VERSION-$OS-$ARCH"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "== semantix $VERSION ($OS/$ARCH) install =="
mkdir -p "$BIN_DIR" "$DATA_DIR"

# 1. download binary + checksums (HTTP/1.1 + retry-all: some networks
#    break on HTTP2 framing; resume-safe via --retry)
CURL="curl -fSL --http1.1 --retry 5 --retry-all-errors --retry-delay 2"
$CURL -o "$TMP/$EXE" "$BASE/$EXE"
$CURL -o "$TMP/SHA256SUMS.txt" "$BASE/SHA256SUMS.txt"
# 2. verify checksum
(cd "$TMP" && grep "$EXE" SHA256SUMS.txt | shasum -a 256 -c -)
# 3. install
chmod +x "$TMP/$EXE"
mv "$TMP/$EXE" "$BIN_DIR/semantix"

# 4. smoke test
"$BIN_DIR/semantix" search --help >/dev/null 2>&1 \
  || { echo "install: smoke test failed" >&2; exit 1; }
echo "== installed: $BIN_DIR/semantix =="
echo "== data dir:  $DATA_DIR (user memory library) =="
echo "== next: add $BIN_DIR to PATH; see agent-skill/SKILL.md for usage =="
