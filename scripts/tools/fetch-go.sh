#!/usr/bin/env bash
# fetch-go.sh — download Go source surface of Gnosil/DeepSeek-Reasonix@main-v2
# via the contents API (github.com:443 + codeload + raw are unreachable from
# this network; api.github.com works). Concurrency 12, resume-safe.
set -uo pipefail

TOKEN="$(gh auth token)"
REPO="Gnosil/DeepSeek-Reasonix"
REF="main-v2"
OUT="/tmp/dsr-fetch"
mkdir -p "$OUT"

fetch() {
  local p="$1"
  local dst="$OUT/$p"
  if [[ -s "$dst" ]]; then return 0; fi   # resume
  local resp enc
  resp="$(curl -s --max-time 40 -H "Authorization: Bearer $TOKEN" \
    "https://api.github.com/repos/$REPO/contents/$p?ref=$REF")"
  enc="$(printf '%s' "$resp" | jq -r '.content // empty' 2>/dev/null)"
  if [[ -z "$enc" ]]; then
    echo "FAIL $p" >> "$OUT/errors.log"
    return 1
  fi
  mkdir -p "$(dirname "$dst")"
  # atomic write: decode to a temp file, then rename — a truncated/bad file
  # is never left behind where the resume check ([[ -s $dst ]]) would trust it.
  if printf '%s' "$enc" | base64 -d > "$dst.tmp" 2>/dev/null && mv "$dst.tmp" "$dst" 2>/dev/null; then
    :
  else
    rm -f "$dst.tmp"
    echo "FAIL $p" >> "$OUT/errors.log"
    return 1
  fi
}

# worker mode: script --fetch <path>
if [[ "${1:-}" == "--fetch" ]]; then
  fetch "$2"
  exit $?
fi

LIST="$1"   # file with one path per line
: > "$OUT/errors.log"
xargs -P 12 -I{} "$0" --fetch {} < "$LIST"
echo "done: $(find "$OUT" -type f ! -name errors.log | wc -l) files, errors: $(wc -l < "$OUT/errors.log")"
