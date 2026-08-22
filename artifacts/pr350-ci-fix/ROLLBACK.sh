#!/usr/bin/env sh
set -eu
root="${1:-$(git rev-parse --show-toplevel)}"
target="$root/artifacts/semantix-v28-port/MODIFIED_FILE.go"
first_line=$(sed -n '1p' "$target" | tr -d '\r')
[ "$first_line" = '//go:build ignore' ] || {
  echo 'build constraint not found' >&2
  exit 1
}
tmp="$target.rollback.tmp"
tail -n +3 "$target" > "$tmp"
mv "$tmp" "$target"
