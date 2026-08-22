#!/usr/bin/env bash
# build-protected.sh — customer-distributable build with code protection.
#
# The stock release build (-trimpath -ldflags "-s -w") ships native binaries
# with no source, but a Go binary still carries its pclntab: every package
# path, function name and source file name (semantix/kernel/evolve.(*ewmaEngine)
# .maybeAdjustLocked, ...) plus `go version -m` module metadata including the
# exact git revision. That is enough to reconstruct the product's internal
# architecture with off-the-shelf tooling (strings / redress / Ghidra).
#
# This script produces the hardened variant for customer distribution:
#   * garble (mvdan.cc/garble) rewrites every package path / identifier of
#     this module and its dependencies to opaque hashes,
#   * -literals encrypts string literals (prompts, heuristics, endpoints),
#   * -tiny strips runtime/position metadata, -seed=random salts the name hashing,
#   * -trimpath -buildvcs=false -ldflags "-s -w" strip the rest.
# After building, a leak scan asserts the protection actually held: any
# recoverable semantix package path, known internal identifier, or module
# metadata fails the build. Host-native binaries also get a functional
# smoke run (version banner must carry the injected version).
#
# What this does NOT do: native code can always be disassembled with enough
# effort — obfuscation raises the cost of reverse engineering, it does not
# make it impossible. Secrets must never be embedded in the binary; the
# license/quota gate lives server-side (gateway [billing], gateway/quota.go).
#
# Usage:
#   scripts/release/build-protected.sh --version v0.3.2
#     [--platforms linux-amd64,darwin-arm64,...]   default: 6-platform matrix
#     [--binaries semantix,semantix-agent[,semantix-gateway]]
#     [--seed random|<base64>]                     default: random
#
# Env: GO (go binary), GARBLE (garble binary; auto-discovered in PATH and
# $HOME/go/bin), GARBLE_VERSION (pinned install when missing, default v0.17.0).
# Output: dist-protected/semantix-<version>-<platform>-protected.tar.gz + SHA256SUMS.txt
set -euo pipefail

VERSION=""
PLATFORMS="darwin-arm64,darwin-amd64,linux-amd64,linux-arm64,windows-amd64,windows-arm64"
BINARIES="semantix,semantix-agent"
SEED="random"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)      VERSION="$2"; shift 2 ;;
    --version=*)    VERSION="${1#*=}"; shift ;;
    --platforms)    PLATFORMS="$2"; shift 2 ;;
    --platforms=*)  PLATFORMS="${1#*=}"; shift ;;
    --binaries)     BINARIES="$2"; shift 2 ;;
    --binaries=*)   BINARIES="${1#*=}"; shift ;;
    --seed)         SEED="$2"; shift 2 ;;
    --seed=*)       SEED="${1#*=}"; shift ;;
    *) echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

# Anchored semver check (same contract as build-full.sh: rejects injection).
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid version: $VERSION (want vX.Y.Z[...])" >&2
  exit 2
fi
case "$SEED" in
  random) ;;
  *[!A-Za-z0-9+/=_-]*) echo "invalid seed: $SEED (base64 or 'random')" >&2; exit 2 ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO="${GO:-go}"
GARBLE_VERSION="${GARBLE_VERSION:-v0.17.0}"
OUT="$ROOT/dist-protected"
rm -rf "$OUT"
mkdir -p "$OUT"

# --- toolchain -------------------------------------------------------------
command -v "$GO" >/dev/null || { echo "go not found (set GO=)" >&2; exit 1; }
# garble rewrites the linker via -overlay, which Go refuses for files under
# GOMODCACHE — so an auto-downloaded toolchain (GOTOOLCHAIN=auto fetching the
# go.mod version into the module cache) cannot be used. Pin GOTOOLCHAIN=local
# and require a real install: on mismatch, install the go.mod version outside
# the module cache (e.g. from https://go.dev/dl/) and point GO= at it.
export GOTOOLCHAIN=local
GODIR="$(cd "$(dirname "$(command -v "$GO")")" && pwd)"
export PATH="$GODIR:$PATH"
if ! (cd "$ROOT" && "$GO" list ./cmd/semantix >/dev/null 2>&1); then
  echo "toolchain preflight failed: $GO (GOTOOLCHAIN=local) cannot load this module." >&2
  echo "garble needs a Go toolchain installed OUTSIDE the module cache matching go.mod ($(grep '^go ' "$ROOT/go.mod"))." >&2
  echo "install it from https://go.dev/dl/ and re-run with GO=/path/to/go/bin/go" >&2
  exit 1
fi
GARBLE="${GARBLE:-}"
if [[ -z "$GARBLE" ]]; then
  if command -v garble >/dev/null; then
    GARBLE=garble
  elif [[ -x "$HOME/go/bin/garble" ]]; then
    GARBLE="$HOME/go/bin/garble"
  else
    echo "garble not found — installing mvdan.cc/garble@$GARBLE_VERSION ..."
    "$GO" install "mvdan.cc/garble@$GARBLE_VERSION"
    GARBLE="$("$GO" env GOPATH)/bin/garble"
  fi
fi
echo "using: $("$GO" version | head -1) + $("$GARBLE" version | head -1)"

# --- leak scan -------------------------------------------------------------
# Sentinels that a *plain* build leaks via the pclntab. If any survive in the
# protected binary, obfuscation silently failed → hard error. Kept in one
# place so new high-value internals can be added as they appear.
LEAK_PATTERNS=(
  'semantix/kernel'
  'semantix/harness'
  'semantix/gateway'
  'semantix/cmd'
  'ewmaEngine'
  'LLMJudge'
  'quotaEngine'
  'maybeAdjustLocked'
  'BurntSushi'
)

verify_binary() {
  local bin="$1" plat="$2"
  local leaks=0
  for pat in "${LEAK_PATTERNS[@]}"; do
    if strings -a "$bin" | grep -q -- "$pat"; then
      echo "LEAK [$plat $(basename "$bin")]: pattern '$pat' recoverable" >&2
      leaks=1
    fi
  done
  # Module metadata must be gone: `go version -m` either fails outright or
  # reports no semantix module path / vcs revision.
  if "$GO" version -m "$bin" 2>/dev/null | grep -qE 'mod[[:space:]]+semantix|vcs\.revision'; then
    echo "LEAK [$plat $(basename "$bin")]: go version -m still reveals module metadata" >&2
    leaks=1
  fi
  [[ $leaks -eq 0 ]] || return 1
  echo "   leak scan clean: $(basename "$bin") ($plat)"
}

smoke_host_binary() {
  # Functional smoke, host platform only: the obfuscated binary must run and
  # report the injected version (proves -ldflags -X survived garble).
  local bin="$1" name="$2"
  local got
  case "$name" in
    semantix)        got="$("$bin" version 2>/dev/null | head -1 || true)" ;;
    semantix-agent)  got="$("$bin" --version 2>/dev/null | head -1 || true)" ;;
    semantix-gateway) return 0 ;;  # needs a config file; covered by leak scan
  esac
  if [[ "$got" != *"${VERSION#v}"* && "$got" != *"$VERSION"* ]]; then
    echo "SMOKE FAIL [$name]: version banner missing $VERSION: '$got'" >&2
    return 1
  fi
  echo "   smoke ok: $name → $got"
}

# --- build matrix ----------------------------------------------------------
HOST_OS="$("$GO" env GOOS)"
HOST_ARCH="$("$GO" env GOARCH)"

for plat in ${PLATFORMS//,/ }; do
  os="${plat%-*}"; arch="${plat#*-}"
  case "$os" in darwin|linux|windows) ;; *) echo "unsupported os: $os" >&2; exit 2 ;; esac
  case "$arch" in amd64|arm64) ;; *) echo "unsupported arch: $arch" >&2; exit 2 ;; esac
  ext=""; [[ "$os" == windows ]] && ext=".exe"

  PKG="semantix-$VERSION-$plat-protected"
  D="$OUT/$PKG"
  mkdir -p "$D"

  for name in ${BINARIES//,/ }; do
    case "$name" in semantix|semantix-agent|semantix-gateway) ;; *)
      echo "unknown binary: $name (allowed: semantix, semantix-agent, semantix-gateway)" >&2; exit 2 ;;
    esac
    echo "-- garble $name ($os/$arch)"
    (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOFLAGS=-buildvcs=false \
      "$GARBLE" -literals -tiny -seed="$SEED" build \
        -trimpath -ldflags "-s -w -X main.version=$VERSION" \
        -o "$D/$name$ext" "./cmd/$name")
    verify_binary "$D/$name$ext" "$plat"
    if [[ "$os" == "$HOST_OS" && "$arch" == "$HOST_ARCH" ]]; then
      smoke_host_binary "$D/$name$ext" "$name"
    fi
    chmod +x "$D/$name$ext"
  done

  cp "$ROOT/semantix.example.toml" "$D/" 2>/dev/null || true
  (cd "$OUT" && tar -czf "$PKG.tar.gz" "$PKG")
  rm -rf "$D"
  echo "packed $PKG.tar.gz"
done

(cd "$OUT" && { command -v shasum >/dev/null && shasum -a 256 semantix-*.tar.gz || sha256sum semantix-*.tar.gz; } > SHA256SUMS.txt)
echo "---"
echo "protected artifacts in $OUT:"
ls -lh "$OUT" | awk 'NR>1 {print "  " $5 "\t" $9}'
