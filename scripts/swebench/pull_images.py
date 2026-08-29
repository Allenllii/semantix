#!/usr/bin/env python3
"""Pre-pull SWE-bench eval images for a subset, working around Docker Hub
anonymous-pull rate limits by fetching the Epoch AI ghcr mirror and retagging
to the canonical name in the dataset's `image` field (which run_evaluation
then finds locally).

Usage: python pull_images.py --dataset data/swebench_verified.jsonl \
           --ids subsets/verified-50-seed20260824.txt [--workers 3] [--remove]
"""

from __future__ import annotations

import argparse
import concurrent.futures as cf
import json
import subprocess
import sys
from pathlib import Path


def ghcr_name(instance_id: str) -> str:
    return f"ghcr.io/epoch-research/swe-bench.eval.x86_64.{instance_id}:latest"


def pull_one(inst: dict, remove: bool) -> str:
    canonical = inst["image"]
    iid = inst["instance_id"]
    if remove:
        for name in (canonical, ghcr_name(iid)):
            subprocess.run(["docker", "rmi", name], capture_output=True)
        return f"{iid}: removed"
    have = subprocess.run(["docker", "image", "inspect", canonical],
                          capture_output=True)
    if have.returncode == 0:
        return f"{iid}: already present"
    # Try Docker Hub first (works when not rate-limited), then the mirror.
    hub = subprocess.run(["docker", "pull", canonical], capture_output=True, text=True)
    if hub.returncode == 0:
        return f"{iid}: pulled from hub"
    mirror = ghcr_name(iid)
    proc = subprocess.run(["docker", "pull", mirror], capture_output=True, text=True)
    if proc.returncode != 0:
        return f"{iid}: FAILED ({hub.stderr.strip()[-80:]} | {proc.stderr.strip()[-80:]})"
    subprocess.run(["docker", "tag", mirror, canonical], check=True)
    subprocess.run(["docker", "rmi", mirror], capture_output=True)
    return f"{iid}: pulled from ghcr mirror"


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--dataset", required=True)
    ap.add_argument("--ids", required=True)
    ap.add_argument("--workers", type=int, default=3)
    ap.add_argument("--remove", action="store_true", help="delete instead of pull")
    args = ap.parse_args()

    wanted = {l.strip() for l in Path(args.ids).read_text().splitlines()
              if l.strip() and not l.startswith("#")}
    insts = [json.loads(l) for l in open(args.dataset)]
    insts = [i for i in insts if i["instance_id"] in wanted]
    missing = wanted - {i["instance_id"] for i in insts}
    if missing:
        sys.exit(f"ids not in dataset: {missing}")

    failed = 0
    with cf.ThreadPoolExecutor(max_workers=args.workers) as pool:
        for res in pool.map(lambda i: pull_one(i, args.remove), insts):
            print(res, flush=True)
            failed += "FAILED" in res
    print(f"done: {len(insts) - failed}/{len(insts)} ok")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
