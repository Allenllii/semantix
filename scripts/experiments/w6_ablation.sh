#!/usr/bin/env bash
# w6_ablation.sh — offline ablation matrix over the W0 TraceLab tree
# (efficiency research plan W6). Arms:
#
#   A1  vanilla            : bm25, user-mode queries
#   A2  +hybrid(hash)      : bm25+vector fused, deterministic hash embedder
#   A3  +hybrid(model)     : same, real embeddings via the local embed server
#   A4  order              : A1/A2 re-run on the seeded-shuffled tree
#                            (curriculum H5: chronological vs random)
#   A5  T-slice admission  : tools-mode queries, same-project vs control dir
#                            (cross-project negative control, #268 evidence)
#   A6  T-slice granularity: tools-mode with --t-step-split on/off
#
# Each arm aggregates the probe's JSON envelope over every project dir; the
# control arm runs over the control dir. Output: one JSON file per arm under
# --out; docs/reports/w5-w6-ablation.md consumes them.
#
# Usage: w6_ablation.sh --w0 /tmp/tracelab/w0 --random /tmp/tracelab/w6-random \
#          --semantix /tmp/semantix-w6 --out /tmp/tracelab/w6 \
#          [--embed-base-url http://127.0.0.1:9001] [--embed-model bge-small-zh]

set -euo pipefail

W0=RANDOM=OUT=SEM=EMB_URL=EMB_MODEL=""
while [ $# -gt 0 ]; do
  case "$1" in
    --w0) W0="$2"; shift 2;;
    --random) RANDOM_TREE="$2"; shift 2;;
    --semantix) SEM="$2"; shift 2;;
    --out) OUT="$2"; shift 2;;
    --embed-base-url) EMB_URL="$2"; shift 2;;
    --embed-model) EMB_MODEL="$2"; shift 2;;
    *) echo "unknown flag: $1" >&2; exit 2;;
  esac
done
[ -n "${RANDOM_TREE:-}" ] || RANDOM_TREE="$W0" # order arm collapses to A1/A2
mkdir -p "$OUT"

# aggregate <name> -- <probe flags...>: run the probe over every project dir
# (and the control dir) and fold the JSON envelopes into one file.
aggregate() {
  local name="$1"; shift
  "$SEM" probe "$@" > "$OUT/${name}.json" 2>/dev/null || { echo "arm $name failed" >&2; return 0; }
}

for proj in "$W0"/project_*; do
  p=$(basename "$proj")
  # A1 / A2 / A3: retrieval arms on the chronological (curriculum) tree.
  aggregate "a1-bm25-user--$p"        --dir "$proj" --query-mode user --retriever bm25 --json
  aggregate "a2-hybrid-hash-user--$p" --dir "$proj" --query-mode user --retriever hybrid --json
  if [ -n "$EMB_URL" ]; then
    SEMANTIX_EMBED_API_KEY=offline aggregate "a3-hybrid-model-user--$p" \
      --dir "$proj" --query-mode user --retriever hybrid \
      --embed-backend model --embed-base-url "$EMB_URL" --embed-model "$EMB_MODEL" --json
  fi
  # A4: order ablation on the shuffled tree.
  if [ -d "$RANDOM_TREE/$p" ]; then
    aggregate "a4-bm25-user-shuffled--$p"        --dir "$RANDOM_TREE/$p" --query-mode user --retriever bm25 --json
    aggregate "a4-hybrid-hash-user-shuffled--$p" --dir "$RANDOM_TREE/$p" --query-mode user --retriever hybrid --json
  fi
  # A5 / A6: T-slice arms (tools mode) on the chronological tree.
  aggregate "a5-tools--$p"             --dir "$proj" --query-mode tools --retriever bm25 --json
  aggregate "a6-tools-tstepsplit--$p"  --dir "$proj" --query-mode tools --retriever bm25 --t-step-split --json
done

# Control: cross-project pool (T-slice leakage detector, A5 evidence).
aggregate "a5-control-tools" --dir "$W0/control" --query-mode tools --retriever bm25 --json
echo "arms written to $OUT" >&2
