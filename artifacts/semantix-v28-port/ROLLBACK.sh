#!/usr/bin/env bash
set -euo pipefail
SOURCE_REPO="${1:?source repo required}"
TARGET_ROOT="${2:?target root required}"
BASE="0b62f6b6e6365cbb4750b969828501b12bad4c24"
FILES=(
  harness/boot/boot.go
  harness/config/config.go
  harness/config/system_prompt_test.go
  harness/control/controller.go
)
NEW_FILES=(
  harness/control/semantic_review_test.go
  harness/semanticreview/reviewer.go
  harness/semanticreview/reviewer_test.go
  docs/reports/semantix-v16-v28-prompt-stack.md
)
for file in "${FILES[@]}"; do
  mkdir -p "$TARGET_ROOT/$(dirname "$file")"
  git -C "$SOURCE_REPO" show "$BASE:$file" > "$TARGET_ROOT/$file"
done
for file in "${NEW_FILES[@]}"; do rm -f "$TARGET_ROOT/$file"; done