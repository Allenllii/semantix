#!/bin/sh
# install.sh — one-line installer for the Semantix coding agent + memory kernel.
#
# Installs two binaries under ~/.local/bin:
#   semantix        the memory kernel AND the umbrella launcher — bare
#                   `semantix` starts the coding agent in the current folder;
#                   `semantix <subcommand>` runs a kernel command.
#   semantix-agent  the interactive coding agent (the umbrella execs it).
# Enables cross-session memory by default and prints the PATH step.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Gnosil/semantix/main/agent-skill/scripts/install.sh | sh
#   # pin a version / arch:
#   curl -fsSL .../install.sh | sh -s -- v0.4.0 arm64
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
BASE="https://github.com/$REPO/releases/download/$VERSION"
PKG="semantix-$VERSION-$OS-$ARCH.tar.gz"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "== semantix $VERSION ($OS/$ARCH) =="
mkdir -p "$BIN_DIR" "$HOME_DIR"

# 1. download bundle + checksums.
$CURL -o "$TMP/$PKG" "$BASE/$PKG"
$CURL -o "$TMP/SHA256SUMS.txt" "$BASE/SHA256SUMS.txt"

# 2. verify the checksum (shasum on macOS, sha256sum on most Linux).
if command -v shasum >/dev/null 2>&1; then
  (cd "$TMP" && grep " $PKG\$" SHA256SUMS.txt | shasum -a 256 -c -)
else
  (cd "$TMP" && grep " $PKG\$" SHA256SUMS.txt | sha256sum -c -)
fi

# 3. extract + install both binaries. Bundle layout:
#    semantix-<version>-<os>-<arch>/{semantix, semantix-agent, ...}
tar -xzf "$TMP/$PKG" -C "$TMP"
SRC="$TMP/semantix-$VERSION-$OS-$ARCH"
install -m 0755 "$SRC/semantix" "$BIN_DIR/semantix"
install -m 0755 "$SRC/semantix-agent" "$BIN_DIR/semantix-agent"

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

# 6. PATH hint (only when BIN_DIR is not already reachable).
case ":$PATH:" in
  *":$BIN_DIR:"*) : ;;
  *) echo "== add to PATH: export PATH=\"$BIN_DIR:\$PATH\"  (append to ~/.bashrc or ~/.zshrc) ==" ;;
esac
echo "== done. run 'semantix' inside any project to start the agent (that folder is the workspace). =="
