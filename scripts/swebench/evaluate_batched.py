#!/usr/bin/env python3
"""Batched official evaluation for disk-constrained hosts.

The 50-instance subset's prebuilt images total ~3x the writable disk, so
instances are grouped (by repo, bounded batch size), and per batch: pull the
batch's images, evaluate that batch's instances across every run directory
(swebench.harness.run_evaluation -i ...), then prune images before the next
batch. A final --rewrite_reports pass per run rebuilds the full 50-instance
report from the accumulated per-instance logs.

Usage:
  python3 evaluate_batched.py --runs results/a results/b ... \
      --dataset data/swebench_verified.jsonl --ids subsets/verified-50-s20260824.txt \
      [--batch-size 9] [--max-workers 3]
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from collections import defaultdict
from pathlib import Path


def sh(cmd: list[str], cwd: Path | None = None) -> int:
    print("+", " ".join(str(c) for c in cmd), flush=True)
    return subprocess.run(cmd, cwd=cwd).returncode


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", nargs="+", required=True)
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--ids", required=True)
    ap.add_argument("--batch-size", type=int, default=9)
    ap.add_argument("--max-workers", type=int, default=3)
    ap.add_argument("--timeout", type=int, default=1800)
    args = ap.parse_args()

    dataset = Path(args.dataset).resolve()
    wanted = [l.strip() for l in Path(args.ids).read_text().splitlines()
              if l.strip() and not l.startswith("#")]
    rows = {}
    with open(dataset) as f:
        for line in f:
            row = json.loads(line)
            if row["instance_id"] in wanted:
                rows[row["instance_id"]] = row

    by_repo: dict[str, list[str]] = defaultdict(list)
    for iid in wanted:
        by_repo[rows[iid]["repo"]].append(iid)
    batches: list[list[str]] = []
    for repo in sorted(by_repo):
        ids = by_repo[repo]
        for i in range(0, len(ids), args.batch_size):
            batches.append(ids[i:i + args.batch_size])

    run_dirs = [Path(r).resolve() for r in args.runs]
    print(f"{len(wanted)} instances -> {len(batches)} batches x {len(run_dirs)} runs", flush=True)

    for n, batch in enumerate(batches, 1):
        print(f"=== batch {n}/{len(batches)} ({rows[batch[0]]['repo']}, {len(batch)} instances) ===", flush=True)
        for iid in batch:
            if sh(["docker", "pull", "-q", rows[iid]["image"]]) != 0:
                print(f"PULL_FAIL {iid} (evaluation may build locally or error)", flush=True)
        for run_dir in run_dirs:
            rc = sh([sys.executable, "-m", "swebench.harness.run_evaluation",
                     "--dataset_name", str(dataset),
                     "--predictions_path", str(run_dir / "preds.jsonl"),
                     "--max_workers", str(args.max_workers),
                     "--run_id", run_dir.name,
                     "--timeout", str(args.timeout),
                     "-i", *batch], cwd=run_dir)
            if rc != 0:
                print(f"BATCH_EVAL_NONZERO run={run_dir.name} batch={n} rc={rc}", flush=True)
        sh(["docker", "system", "prune", "-af"])

    print("=== rewrite full reports ===", flush=True)
    for run_dir in run_dirs:
        sh([sys.executable, "-m", "swebench.harness.run_evaluation",
            "--dataset_name", str(dataset),
            "--predictions_path", str(run_dir / "preds.jsonl"),
            "--run_id", run_dir.name,
            "--rewrite_reports", "true"], cwd=run_dir)
    print("EVAL_ALL_DONE", flush=True)


if __name__ == "__main__":
    main()
