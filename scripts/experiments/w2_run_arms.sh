#!/bin/bash
# W2 calibration: 3 retrieval arms on the easy + hard paraphrase sets.
set -u
SEM=/tmp/semantix
LIB=/tmp/tracelab/gwm1-lib.jsonl
ENVU=(env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY NO_PROXY='*')
SUM='
import json,sys
d=json.load(sys.stdin)["data"]
rows=[t for t in d["turns_tsv"] if t["session"]==1]
h=sum(1 for t in rows if t["zone"]=="hit"); g=sum(1 for t in rows if t["zone"]=="grey"); m=sum(1 for t in rows if t["zone"]=="miss")
s=[t["score"] for t in rows]
print(f"hit={h} grey={g} miss={m} mean_top1={sum(s)/len(s):.3f} min={min(s):.3f} max={max(s):.3f}")
'
for setname in para hard; do
  echo "== $setname =="
  for arm in bm25 hash bge; do
    case $arm in
      bm25) flags=(--retriever bm25);;
      hash) flags=(--retriever hybrid);;
      bge)  flags=(--retriever hybrid --embed-backend model --embed-base-url http://127.0.0.1:8688/v1 --embed-model bge-small-zh);;
    esac
    printf "  %-5s " "$arm"
    "${ENVU[@]}" "$SEM" probe --sessions "$LIB,/tmp/tracelab/gwm1-$setname.jsonl" "${flags[@]}" --json 2>/dev/null | python3.12 -c "$SUM"
  done
done
