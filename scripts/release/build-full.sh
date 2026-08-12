#!/usr/bin/env bash
# build-full.sh — package the complete downloadable agent (v0.3.0+):
#   reasonix (coding-agent harness, from the semantix fork) + semantix
#   (memory kernel) + example configs + install.sh + README, per platform.
#
# Usage: build-full.sh --version v0.3.0 [--platforms darwin-arm64,linux-amd64]
# Output: dist/semantix-agent-<version>-<platform>.tar.gz + SHA256SUMS
set -euo pipefail

VERSION=""
PLATFORMS="darwin-arm64"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"; shift 2 ;;
    --platforms)
      PLATFORMS="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    *)
      echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

# Anchored semver check (rejects v1..2, junk, injection).
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid version: $VERSION (want vX.Y.Z[...])" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FORK_ROOT="${FORK_ROOT:-$ROOT/../DeepSeek-Reasonix}"
GO="${GO:-/tmp/go2/go/bin/go}"
OUT="$ROOT/dist"
rm -rf "$OUT"
mkdir -p "$OUT"

for plat in ${PLATFORMS//,/ }; do
  os="${plat%-*}"; arch="${plat#*-}"
  case "$os" in darwin|linux) ;; *) echo "unsupported os: $os" >&2; exit 2 ;; esac
  case "$arch" in amd64|arm64) ;; *) echo "unsupported arch: $arch" >&2; exit 2 ;; esac

  PKG="semantix-agent-$VERSION-$plat"
  D="$OUT/$PKG"
  mkdir -p "$D"

  echo "building reasonix ($os/$arch)..."
  (cd "$FORK_ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    "$GO" build -trimpath -ldflags "-s -w" -o "$OUT/reasonix-$plat" ./cmd/reasonix)

  echo "building semantix ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    "$GO" build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$OUT/semantix-$plat" ./cmd/semantix)

  mv "$OUT/reasonix-$plat" "$D/reasonix"
  mv "$OUT/semantix-$plat" "$D/semantix"
  cp "$FORK_ROOT/reasonix.example.toml" "$D/" 2>/dev/null || true
  cp "$ROOT/semantix.example.toml" "$D/" 2>/dev/null || true
  cp "$ROOT/agent-skill/scripts/install.sh" "$D/semantix-install.sh" 2>/dev/null || true
  cat > "$D/README.md" <<EOF
# Semantix Agent $VERSION ($plat)

Complete downloadable coding agent with cross-session memory:
- \`reasonix\` — the coding-agent harness (fork of Reasonix, single-session
  prefix-cache discipline inherited, ~90%+ cache-hit on long sessions)
- \`semantix\` — the self-evolving memory kernel (L2 semantic injection,
  L3 verified reuse, evolution engine, cost dashboard)

## Quick start
1. ./semantix-install.sh v$VERSION   # installs both binaries + configs
2. edit reasonix.example.toml → set your provider + [semantix] section
3. reasonix --config reasonix.toml   # start the agent
4. semantix usage                    # cost dashboard after a session

## Memory
Sessions are captured by the harness sink; run
    semantix extract -input <session>.jsonl -scope project
to build the memory library, and the agent will inject relevant slices
automatically on similar tasks.
EOF
  chmod +x "$D/reasonix" "$D/semantix" "$D/semantix-install.sh" 2>/dev/null || true

  (cd "$OUT" && tar -czf "$PKG.tar.gz" "$PKG")
  rm -rf "$D"
  echo "packed $PKG.tar.gz"
done

(cd "$OUT" && shasum -a 256 semantix-agent-*.tar.gz > SHA256SUMS.txt)
echo "---"
ls -lh "$OUT" | awk '{print $5, $9}'
