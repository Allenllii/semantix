#!/usr/bin/env python3
"""Batched, resumable official evaluation for disk- and rate-limit-constrained hosts.

The 50-instance subset's prebuilt images total ~3x the writable disk, and
anonymous Docker Hub pulls are rate-limited (429 after ~10 pulls/hour), so:

- Remaining work is recomputed from disk on every start: an evaluation is
  needed iff the run has a non-empty prediction for the instance and no
  report.json under logs/run_evaluation/<run_id>/. Re-running the driver
  resumes; finished work is never repeated.
- Instances whose images are already local are evaluated first (no pulls,
  and pruning afterwards frees their disk for the rest).
- Remaining instances are grouped by repo into bounded batches. Each batch's
  images are pulled with retry: images that fail (rate limit, transient
  registry errors) are retried after --pull-wait seconds, up to
  --pull-attempts rounds, letting the pull quota refill. Images still
  failing are skipped with PULL_GIVEUP and their instances dropped from the
  batch (a later resume picks them up).
- After each batch: docker system prune -af.
- A final --rewrite_reports pass per run rebuilds the full report from the
  accumulated per-instance logs.

Usage:
  python3 evaluate_batched.py --runs results/a results/b ... \
      --dataset data/swebench_verified.jsonl --ids subsets/verified-50-s20260824.txt \
      [--batch-size 9] [--max-workers 3] [--pull-attempts 10] [--pull-wait 660]
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
from collections import defaultdict
from pathlib import Path


def sh(cmd: list[str], cwd: Path | None = None) -> int:
    print("+", " ".join(str(c) for c in cmd), flush=True)
    return subprocess.run(cmd, cwd=cwd).returncode


def image_local(image: str) -> bool:
    return subprocess.run(["docker", "image", "inspect", image],
                          capture_output=True).returncode == 0


def pull_with_retry(images: list[str], attempts: int, wait: int) -> set[str]:
    got = {im for im in images if image_local(im)}
    pending = [im for im in images if im not in got]
    for attempt in range(1, attempts + 1):
        if not pending:
            break
        still = []
        for im in pending:
            if sh(["docker", "pull", "-q", im]) == 0:
                got.add(im)
            else:
                still.append(im)
        pending = still
        if pending and attempt < attempts:
            print(f"PULL_RETRY_WAIT {len(pending)} images pending after round "
                  f"{attempt}/{attempts}, sleeping {wait}s", flush=True)
            time.sleep(wait)
    for im in pending:
        print(f"PULL_GIVEUP {im}", flush=True)
    return got


def remaining_for_run(run_dir: Path, wanted: list[str]) -> list[str]:
    preds: dict[str, str] = {}
    with open(run_dir / "preds.jsonl") as f:
        for line in f:
            p = json.loads(line)
            preds[p["instance_id"]] = (p.get("model_patch") or "").strip()
    base = run_dir / "logs" / "run_evaluation" / run_dir.name
    done = {rep.parent.name for rep in base.glob("*/*/report.json")} if base.exists() else set()
    return [i for i in wanted if preds.get(i) and i not in done]


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", nargs="+", required=True)
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--ids", required=True)
    ap.add_argument("--batch-size", type=int, default=9)
    ap.add_argument("--max-workers", type=int, default=3)
    ap.add_argument("--timeout", type=int, default=1800)
    ap.add_argument("--pull-attempts", type=int, default=10)
    ap.add_argument("--pull-wait", type=int, default=660)
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

    run_dirs = [Path(r).resolve() for r in args.runs]
    needed = {rd: set(remaining_for_run(rd, wanted)) for rd in run_dirs}
    for rd in run_dirs:
        print(f"remaining {rd.name}: {len(needed[rd])}", flush=True)
    union = [i for i in wanted if any(i in v for v in needed.values())]

    local_first = [i for i in union if image_local(rows[i]["image"])]
    rest = [i for i in union if i not in local_first]
    by_repo: dict[str, list[str]] = defaultdict(list)
    for iid in rest:
        by_repo[rows[iid]["repo"]].append(iid)
    batches: list[list[str]] = [local_first] if local_first else []
    for repo in sorted(by_repo):
        ids = by_repo[repo]
        for i in range(0, len(ids), args.batch_size):
            batches.append(ids[i:i + args.batch_size])

    print(f"{len(union)} instances -> {len(batches)} batches x {len(run_dirs)} runs",
          flush=True)

    for n, batch in enumerate(batches, 1):
        print(f"=== batch {n}/{len(batches)} ({len(batch)} instances: "
              f"{' '.join(batch)}) ===", flush=True)
        sh(["df", "-h", "/"])
        got = pull_with_retry([rows[i]["image"] for i in batch],
                              args.pull_attempts, args.pull_wait)
        ready = [i for i in batch if rows[i]["image"] in got]
        for run_dir in run_dirs:
            ids = [i for i in ready if i in needed[run_dir]]
            if not ids:
                continue
            rc = sh([sys.executable, "-m", "swebench.harness.run_evaluation",
                     "--dataset_name", str(dataset),
                     "--predictions_path", str(run_dir / "preds.jsonl"),
                     "--max_workers", str(args.max_workers),
                     "--run_id", run_dir.name,
                     "--timeout", str(args.timeout),
                     "-i", *ids], cwd=run_dir)
            if rc != 0:
                print(f"BATCH_EVAL_NONZERO run={run_dir.name} batch={n} rc={rc}",
                      flush=True)
        sh(["docker", "system", "prune", "-af"])

    print("=== coverage after batches ===", flush=True)
    incomplete = False
    for rd in run_dirs:
        rem = remaining_for_run(rd, wanted)
        print(f"still missing {rd.name}: {len(rem)}"
              + (f" ({' '.join(rem)})" if rem else ""), flush=True)
        incomplete = incomplete or bool(rem)

    print("=== rewrite full reports ===", flush=True)
    for run_dir in run_dirs:
        sh([sys.executable, "-m", "swebench.harness.run_evaluation",
            "--dataset_name", str(dataset),
            "--predictions_path", str(run_dir / "preds.jsonl"),
            "--run_id", run_dir.name,
            "--rewrite_reports", "true"], cwd=run_dir)
    print("EVAL_INCOMPLETE (re-run to resume)" if incomplete else "EVAL_ALL_DONE",
          flush=True)


if __name__ == "__main__":
    main()
