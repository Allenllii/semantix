#!/usr/bin/env sh
set -eu
TARGET="${1:-.}"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PATCH="$SCRIPT_DIR/DIFF_FILE"
cd "$TARGET"
git rev-parse --is-inside-work-tree >/dev/null
git apply --check -R "$PATCH"
git apply -R "$PATCH"
printf '%s\n' 'ROLLBACK_RESULT=restored upstream/harness-integration behavior'
