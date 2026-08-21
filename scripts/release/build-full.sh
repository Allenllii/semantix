#!/usr/bin/env bash
# build-full.sh — package the complete downloadable agent:
#   semantix-agent (coding-agent harness) + semantix (memory kernel)
#   + example configs + install.sh + README, per platform.
#
# Everything builds from this repository — the harness is vendored under
# harness/ and compiled via ./cmd/semantix-agent (no external fork checkout).
#
# Usage: build-full.sh --version v0.5.1 [--platforms darwin-arm64,linux-amd64]
# Default platforms: darwin-arm64, darwin-amd64, linux-amd64, linux-arm64
# Output (dist/):
#   semantix-agent-<version>-<platform>.tar.gz   full bundle for humans
#   semantix-agent-<platform>.tar.gz             flat single-binary asset the
#                                                self-updater downloads
#                                                (`semantix-agent upgrade`)
#   semantix-<version>-<platform>                raw kernel binary for the
#                                                agent-skill curl installer
#   SHA256SUMS + SHA256SUMS.txt                  same checksums, both names
#                                                (updater reads the former,
#                                                install.sh the latter)
set -euo pipefail

VERSION=""
PLATFORMS="darwin-arm64,darwin-amd64,linux-amd64,linux-arm64"

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
GO="${GO:-go}"
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

  echo "building semantix-agent ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    "$GO" build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$D/semantix-agent" ./cmd/semantix-agent)

  echo "building semantix ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    "$GO" build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$D/semantix" ./cmd/semantix)

  cp "$ROOT/semantix-agent.example.toml" "$D/" 2>/dev/null || true
  cp "$ROOT/semantix.example.toml" "$D/" 2>/dev/null || true
  cp "$ROOT/agent-skill/scripts/install.sh" "$D/semantix-install.sh" 2>/dev/null || true
  cat > "$D/README.md" <<EOF
# Semantix Agent $VERSION ($plat)

Complete downloadable coding agent with cross-session memory:
- \`semantix-agent\` — the coding-agent harness (single-session prefix-cache
  discipline, ~90%+ cache-hit on long sessions)
- \`semantix\` — the self-evolving memory kernel (L2 semantic injection,
  L3 verified reuse, evolution engine, cost dashboard)

## Quick start
1. install -m 0755 semantix-agent semantix ~/.local/bin/
2. semantix-agent setup                 # interactive provider wizard, or:
   cp semantix-agent.example.toml semantix-agent.toml && edit it
3. semantix-agent                       # start the agent
4. semantix usage                       # cost dashboard after a session

Data lives in ~/.semantix (user) and .semantix/ (project).
semantix-install.sh is the standalone curl installer that drops only the
semantix memory kernel into another agent environment.

## Memory
Sessions are captured by the harness sink; run
    semantix extract -input <session>.jsonl -scope project
to build the memory library, and the agent will inject relevant slices
automatically on similar tasks.
EOF
  chmod +x "$D/semantix-agent" "$D/semantix" "$D/semantix-install.sh" 2>/dev/null || true

  (cd "$OUT" && tar -czf "$PKG.tar.gz" "$PKG")

  # Flat single-binary archive for `semantix-agent upgrade`: the updater
  # expects semantix-agent-<os>-<arch>.tar.gz with the binary at the root.
  (cd "$D" && tar -czf "$OUT/semantix-agent-$plat.tar.gz" semantix-agent)
  # Raw kernel binary for agent-skill/scripts/install.sh (curl flow).
  cp "$D/semantix" "$OUT/semantix-$VERSION-$plat"
  rm -rf "$D"
  echo "packed $PKG.tar.gz + semantix-agent-$plat.tar.gz + semantix-$VERSION-$plat"
done

# One checksum set over every asset, published under both names: the
# self-updater downloads the asset literally named SHA256SUMS, while the
# agent-skill install.sh fetches SHA256SUMS.txt.
(cd "$OUT" && shasum -a 256 semantix-agent-*.tar.gz semantix-"$VERSION"-* > SHA256SUMS && cp SHA256SUMS SHA256SUMS.txt)
echo "---"
ls -lh "$OUT" | awk '{print $5, $9}'
