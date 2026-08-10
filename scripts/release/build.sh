#!/usr/bin/env bash
# build.sh — U14 release build: single static binaries for the supported
# platforms, produced into dist/.
#
# Usage: scripts/release/build.sh [--version v0.2.0]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="v0.2.0"
if [[ $# -gt 0 ]]; then
  case "$1" in
    --version) VERSION="${2:?usage: build.sh [--version vX.Y.Z]}"; shift 2 ;;
    --version=*) VERSION="${1#--version=}" ;;
    *) VERSION="$1" ;;
  esac
fi
GO="${GO:-/tmp/go/go/bin/go}"
OUT="$ROOT/dist"
mkdir -p "$OUT"

# platform:goos:goarch list
PLATFORMS=(
  darwin:darwin:amd64
  darwin:darwin:arm64
  linux:linux:amd64
  linux:linux:arm64
  windows:windows:amd64
  windows:windows:arm64
)

echo "== semantix $VERSION release build =="
for p in "${PLATFORMS[@]}"; do
  name="${p%%:*}"
  rest="${p#*:}"
  goos="${rest%%:*}"
  goarch="${rest##*:}"
  ext=""
  if [[ "$goos" == "windows" ]]; then ext=".exe"; fi
  target="$OUT/semantix-${VERSION}-${goos}-${goarch}${ext}"
  echo "-- $name ($goos/$goarch)"
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" "$GO" build \
    -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "$target" ./cmd/semantix)
done

echo "== checksums =="
(cd "$OUT" && shasum -a 256 semantix-* > SHA256SUMS.txt && cat SHA256SUMS.txt)
echo "== done: $OUT =="
ls -lh "$OUT" | tail -8
