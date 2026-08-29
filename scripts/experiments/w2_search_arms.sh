#!/bin/bash
# Candidate-level W2 calibration: for each hard paraphrase, search the
# library under bm25 vs hybrid+bge and record the top-1 zone + score.
set -u
SEM=/tmp/semantix
DB=/tmp/tracelab/w2store/project.db
ENVU=(env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY SEMANTIX_EMBED_API_KEY=local-dev SEMANTIX_EMBED_BASE_URL=http://127.0.0.1:8688/v1 SEMANTIX_EMBED_MODEL=bge-small-zh NO_PROXY='*')
python3.12 - <<'EOF' > /tmp/tracelab/hard_queries.txt
import json
for line in open('/tmp/tracelab/gwm1-hard.jsonl'):
    d=json.loads(line)
    if d['role']=='user': print(d['content'])
EOF
SUM=scripts/experiments/summarize_search.py
i=0
while IFS= read -r q; do
  i=$((i+1))
  printf "t%02d  " "$i"
  bm=$("${ENVU[@]}" "$SEM" search --query "$q" --db "$DB" --retriever bm25 --limit 1 --json 2>/dev/null | python3.12 "$SUM")
  bg=$("${ENVU[@]}" "$SEM" search --query "$q" --db "$DB" --retriever hybrid --embedder model --limit 1 --json 2>/dev/null | python3.12 "$SUM")
  echo "bm25=[$bm]  bge=[$bg]"
done < /tmp/tracelab/hard_queries.txt
