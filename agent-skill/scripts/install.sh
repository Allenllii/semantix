#!/bin/sh
# install.sh — one-line installer for the Semantix coding agent + memory kernel.
#
# Installs two binaries under ~/.local/bin (both come out of the release's
# full bundle, `semantix-agent-<version>-<os>-<arch>.tar.gz`):
#   semantix        the memory kernel AND the umbrella launcher — bare
#                   `semantix` starts the coding agent in the current folder;
#                   `semantix <subcommand>` runs a kernel command.
#   semantix-agent  the interactive coding agent (the umbrella execs it).
# Enables cross-session memory by default and prints the PATH step.
#
# Want ONLY the kernel dropped into another agent's environment (Claude Code,
# etc.)? Don't use this — install the agent, then run
# `semantix install --target claude-code` (or `--target custom --dir ...`).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh
#   # pin a version / arch:
#   curl -fsSL .../install.sh | sh -s -- v0.7.2 arm64
#
# Env overrides: SEMANTIX_BIN_DIR, SEMANTIX_HOME, SEMANTIX_RELEASE_BASE
# (point the last at a mirror or a local server to test without GitHub).
#
# POSIX sh (no bashisms) so `curl | sh` works everywhere.
set -eu

REPO="Gnosil/semantix"
VERSION="${1:-latest}"
ARCH="${2:-}"
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  darwin | linux) ;;
  *) echo "unsupported OS: $OS (darwin|linux)" >&2; exit 2 ;;
esac
if [ -z "$ARCH" ]; then
  case "$(uname -m)" in
    x86_64 | amd64) ARCH=amd64 ;;
    arm64 | aarch64) ARCH=arm64 ;;
    *) echo "unsupported arch: $(uname -m) (x86_64|arm64)" >&2; exit 2 ;;
  esac
fi
case "$ARCH" in
  amd64 | arm64) ;;
  *) echo "invalid arch: $ARCH (amd64|arm64)" >&2; exit 2 ;;
esac

CURL="curl -fSL --retry 5 --retry-delay 2"

# Resolve 'latest' to the newest published release tag via the GitHub API.
if [ "$VERSION" = "latest" ]; then
  VERSION="$($CURL -s "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$VERSION" ] || { echo "could not resolve the latest release tag" >&2; exit 1; }
fi
# Anchor the tag (reject junk that would only 404 at download time).
case "$VERSION" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "invalid version: $VERSION (want vX.Y.Z or 'latest')" >&2; exit 2 ;;
esac

BIN_DIR="${SEMANTIX_BIN_DIR:-$HOME/.local/bin}"
HOME_DIR="${SEMANTIX_HOME:-$HOME/.semantix}"
BASE="${SEMANTIX_RELEASE_BASE:-https://github.com/$REPO/releases/download/$VERSION}"
# The release's full bundle carries semantix-agent + semantix + gateway + config
# templates. We install the two binaries the umbrella needs; the rest stays in
# the tarball. Asset + inner directory share this name (build-full.sh).
PKG="semantix-agent-$VERSION-$OS-$ARCH.tar.gz"
DIR="semantix-agent-$VERSION-$OS-$ARCH"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "== semantix $VERSION ($OS/$ARCH) =="
mkdir -p "$BIN_DIR" "$HOME_DIR"

# 1. download bundle + checksums.
$CURL -o "$TMP/$PKG" "$BASE/$PKG"
$CURL -o "$TMP/SHA256SUMS.txt" "$BASE/SHA256SUMS.txt"

# 2. verify the checksum. SHA256SUMS.txt lists every platform's assets, so pull
#    the one line whose filename is exactly ours (tolerating the "*" binary-mode
#    marker some shasum builds emit) and feed only that to the checker — passing
#    the whole file would fail on the other platforms' absent files.
SUM_LINE="$(awk -v f="$PKG" '{n=$2; sub(/^\*/,"",n); if (n==f) print}' "$TMP/SHA256SUMS.txt")"
[ -n "$SUM_LINE" ] || { echo "install: no checksum entry for $PKG" >&2; exit 1; }
if command -v shasum >/dev/null 2>&1; then
  printf '%s\n' "$SUM_LINE" | (cd "$TMP" && shasum -a 256 -c -)
else
  printf '%s\n' "$SUM_LINE" | (cd "$TMP" && sha256sum -c -)
fi

# 3. extract + install both binaries out of the bundle directory.
tar -xzf "$TMP/$PKG" -C "$TMP"
SRC="$TMP/$DIR"
for b in semantix semantix-agent; do
  [ -f "$SRC/$b" ] || { echo "install: $b missing from $PKG" >&2; exit 1; }
  install -m 0755 "$SRC/$b" "$BIN_DIR/$b"
done

# 4. enable cross-session memory by default — only on a fresh install; never
#    clobber an existing user config.
CFG="$HOME_DIR/config.toml"
if [ ! -f "$CFG" ]; then
  cat > "$CFG" <<'TOML'
# Semantix agent user config. Cross-session memory is on: the agent mirrors
# sessions to the kernel and injects reusable context on similar tasks. Delete
# the [semantix] block to disable. Provider and model are set on first run —
# the agent guides you through it.
[semantix]
enabled = true
binary  = "semantix"
inject  = true
budget  = 4096
TOML
  echo "== memory enabled: $CFG =="
fi

# 5. smoke test the kernel binary.
"$BIN_DIR/semantix" version >/dev/null 2>&1 \
  || { echo "install: smoke test failed" >&2; exit 1; }
echo "== installed: $BIN_DIR/semantix + $BIN_DIR/semantix-agent =="

# 6. make BIN_DIR reachable. If it is already on PATH we are done; otherwise
#    append the export to the shell's rc (idempotently) so new terminals find
#    `semantix` — that is what lets the one-liner "just work" without a manual
#    step. We also print the line to run in the CURRENT shell right away.
case ":$PATH:" in
  *":$BIN_DIR:"*) : ;; # already reachable — nothing to do
  *)
    LINE="export PATH=\"$BIN_DIR:\$PATH\""
    case "${SHELL:-}" in
      */zsh)  RC="$HOME/.zshrc" ;;
      */bash) [ "$OS" = darwin ] && RC="$HOME/.bash_profile" || RC="$HOME/.bashrc" ;;
      *)      RC="$HOME/.profile" ;;
    esac
    if [ -f "$RC" ] && grep -qF "$BIN_DIR" "$RC" 2>/dev/null; then
      echo "== PATH already set in $RC (new terminals will find semantix) =="
    elif printf '\n# added by semantix install.sh\n%s\n' "$LINE" >> "$RC" 2>/dev/null; then
      echo "== added $BIN_DIR to PATH in $RC =="
    else
      echo "== could not edit $RC — add this line to your shell rc: $LINE =="
    fi
    echo "== to use it in THIS terminal right now, run: $LINE =="
    ;;
esac
echo "== done. run 'semantix' inside any project to start the agent (that folder is the workspace). =="
