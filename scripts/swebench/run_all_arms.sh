#!/usr/bin/env bash
# Sequential five-arm SWE-bench Verified generation round (frozen 50-instance
# subset, deepseek-v4-flash). Arms run one at a time so per-instance wall clock
# is not distorted by cross-arm CPU/API contention. Each arm resumes from its
# preds.jsonl, so rerunning this script after an interruption is safe.
set -u
cd "$(dirname "$0")"

DATASET=data/swebench_verified.jsonl
IDS=subsets/verified-50-s20260824.txt
MODEL=deepseek-v4-flash
WORKERS=4
LOG_DIR=results/logs
mkdir -p "$LOG_DIR"

run_arm() {
  local name="$1"; shift
  echo "=== ARM $name start $(date -u +%FT%TZ) ==="
  python3 run_bench.py --dataset "$DATASET" --ids "$IDS" --model "$MODEL" \
    --workers "$WORKERS" "$@" >> "$LOG_DIR/$name.log" 2>&1
  echo "=== ARM $name exit=$? end $(date -u +%FT%TZ) ==="
}

run_arm semantix    --harness semantix    --run-id semantix.$MODEL.20260824    --semantix-bin /home/user/semantix/bin/semantix-agent
run_arm dsh         --harness dsh         --run-id dsh.$MODEL.20260824
run_arm claude-code --harness claude-code --run-id claude-code.$MODEL.20260824
run_arm codex       --harness codex       --run-id codex.$MODEL.20260824       --codex-bin "$HOME/.local/codex080/node_modules/.bin/codex"
run_arm semantix-ablate --harness semantix --run-id semantix-ablate.$MODEL.20260824 --ablate all --semantix-bin /home/user/semantix/bin/semantix-agent

echo "ALL_ARMS_DONE $(date -u +%FT%TZ)"
