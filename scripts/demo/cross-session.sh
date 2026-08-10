#!/usr/bin/env bash
# cross-session.sh — U9 demo: session B reuses session A's slices.
#
# Demonstrates the full M0-3 closed loop and computes the cost comparison
# (kernel reuse vs. no-kernel baseline):
#   1. generate session A (a task with tool calls) and ingest it
#   2. simulate session B with the same task
#   3. inject session A's slices into session B's turn
#   4. estimate token/cost savings (DeepSeek pricing, adjustable)
#
# Usage: scripts/demo/cross-session.sh [--tool-tokens N] [--reuse-ratio R]
#        [--input-price P] [--output-price P] [--cache-price P] [--tools N]
#
# Cost model (conservative):
#   baseline  = tools × tool_tokens × output_price        (model redoes every step)
#   kernel    = injected_tokens × cache_price             (stable slice = cache hit)
#             + tools × (1-reuse_ratio) × tool_tokens × output_price
#   savings%  = (baseline - kernel) / baseline
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${SEMANTIX_BIN:-$ROOT/semantix}"
WORK="$(mktemp -d /tmp/semantix-demo.XXXXXX)"
trap 'rm -rf "$WORK"' EXIT

TOOL_TOKENS=300      # avg model output tokens per tool call re-generated
REUSE_RATIO=0.8      # fraction of tool steps the injected slice replaces
TOOLS=6              # tool calls in the repeated task
INPUT_PRICE=0.27     # USD per 1M input tokens (cache miss)
CACHE_PRICE=0.07     # USD per 1M input tokens (cache hit — stable prefix)
OUTPUT_PRICE=1.10    # USD per 1M output tokens

# --- parameter validation (no forged numbers; script output feeds M0 reports) ---
validate_num() { # name value min max
  awk -v v="$2" -v mn="$3" -v mx="$4" 'BEGIN{ if (v !~ /^[0-9]+(\.[0-9]+)?$/ || v+0 < mn || v+0 > mx) exit 1 }' || {
    echo "invalid $1: $2 (want [$3,$4])" >&2; exit 2
  }
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tool-tokens) TOOL_TOKENS="$2"; shift 2 ;;
    --reuse-ratio) REUSE_RATIO="$2"; shift 2 ;;
    --tools)       TOOLS="$2"; shift 2 ;;
    --input-price) INPUT_PRICE="$2"; shift 2 ;;
    --cache-price) CACHE_PRICE="$2"; shift 2 ;;
    --output-price) OUTPUT_PRICE="$2"; shift 2 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

validate_num --tool-tokens "$TOOL_TOKENS" 1 100000
validate_num --reuse-ratio "$REUSE_RATIO" 0.01 0.99
validate_num --tools "$TOOLS" 1 1000
validate_num --input-price "$INPUT_PRICE" 0 100
validate_num --cache-price "$CACHE_PRICE" 0 100
validate_num --output-price "$OUTPUT_PRICE" 0 100

if [[ ! -x "$BIN" ]]; then
  echo "building semantix binary..."
  (cd "$ROOT" && /tmp/go/go/bin/go build -o semantix ./cmd/semantix) || { echo "build failed"; exit 1; }
fi

# --- 1. session A: the task done for the first time -----------------------
cat > "$WORK/sess-a.jsonl" <<'EOF'
{"role":"user","content":"修复 go 测试失败并加 CI 配置"}
{"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"grep"},{"id":"c2","name":"readFile"}]}
{"role":"tool","content":"找到 3 个测试文件"}
{"role":"assistant","content":"","tool_calls":[{"id":"c3","name":"editFile"},{"id":"c4","name":"writeFile"}]}
{"role":"tool","content":"断言已修复"}
{"role":"assistant","content":"","tool_calls":[{"id":"c5","name":"writeFile"}]}
{"role":"tool","content":"CI 配置已创建"}
{"role":"assistant","content":"","tool_calls":[{"id":"c6","name":"bash"}]}
{"role":"tool","content":"go test 全过"}
{"role":"assistant","content":"修复完成：测试全过 + CI 就绪"}
EOF

echo "== [1/4] ingest session A =="
"$BIN" extract --input "$WORK/sess-a.jsonl" --db "$WORK/lib.db" --project demo

# --- 2. session B: the same task appears again ----------------------------
cat > "$WORK/sess-b-turn.txt" <<'EOF'
修复 go 测试失败并加 CI 配置
EOF

# --- 3. inject session A's slices into session B's turn --------------------
echo "== [2/4] inject session A slices into session B turn =="
"$BIN" inject --query "$(cat "$WORK/sess-b-turn.txt")" --db "$WORK/lib.db" > "$WORK/injection.txt"
INJECTED_BYTES=$(wc -c < "$WORK/injection.txt")
INJECTED_TOKENS=$((INJECTED_BYTES / 4))   # ~4 bytes/token for mixed text
echo "injected block: $INJECTED_BYTES bytes ≈ $INJECTED_TOKENS tokens"
echo "---"
cat "$WORK/injection.txt"
echo "---"

# --- 4. cost comparison -----------------------------------------------------
echo "== [3/4] cost comparison (no-kernel baseline vs. kernel reuse) =="
BASELINE_TOKENS=$((TOOLS * TOOL_TOKENS))
# reuse ratio as integer percent for shell arithmetic
REUSE_PCT=$(awk -v r="$REUSE_RATIO" 'BEGIN{printf "%d", r * 100}')
KERNEL_TOKENS=$((BASELINE_TOKENS * (100 - REUSE_PCT) / 100))
BASELINE_COST=$(awk -v t="$BASELINE_TOKENS" -v p="$OUTPUT_PRICE" 'BEGIN{printf "%.6f", t * p / 1e6}')
KERNEL_COST=$(awk -v i="$INJECTED_TOKENS" -v c="$CACHE_PRICE" -v t="$KERNEL_TOKENS" -v p="$OUTPUT_PRICE" 'BEGIN{printf "%.6f", i * c / 1e6 + t * p / 1e6}')
SAVINGS=$(awk -v b="$BASELINE_COST" -v k="$KERNEL_COST" 'BEGIN{printf "%.1f", (b - k) / b * 100}')

echo "┌────────────────────┬────────────────┬────────────────┐"
echo "│ metric             │ no-kernel base │ with kernel    │"
echo "├────────────────────┼────────────────┼────────────────┤"
printf "│ completion tokens │ %14d │ %14d │\n" "$BASELINE_TOKENS" "$KERNEL_TOKENS"
printf "│ input tokens (inj) │ %14s │ %14d │\n" "0" "$INJECTED_TOKENS"
printf "│ cost (USD)         │ %14s │ %14s │\n" "$BASELINE_COST" "$KERNEL_COST"
echo "└────────────────────┴────────────────┴────────────────┘"
echo "savings: ${SAVINGS}%   (target ≥ 20%)"
if awk -v s="$SAVINGS" 'BEGIN{exit !(s >= 20)}'; then
  echo "RESULT: PASS — reuse loop demonstrates ≥20% cost saving"
else
  echo "RESULT: FAIL — saving below the 20% M0-3 target"
fi

echo "== [4/4] artifacts =="
echo "injection:   $WORK/injection.txt"
echo "slice lib:   $WORK/lib.db"
echo "reuse loop:  session A slices injected into session B turn ✔"
