#!/usr/bin/env python3
"""Merge run metrics with official evaluation reports into one comparison table.

Usage:
  python report.py --runs results/run-a results/run-b ... [--format md|json]

Each run directory needs metrics.jsonl (from run_bench.py); the resolve rate
column fills in when an official report json (evaluate.py output) is present,
and shows "n/a" otherwise.
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path


def load_run(run_dir: Path) -> dict:
    metrics = []
    mpath = run_dir / "metrics.jsonl"
    if mpath.exists():
        with open(mpath) as f:
            metrics = [json.loads(l) for l in f if l.strip()]
    report = None
    for p in sorted(run_dir.glob(f"*.{run_dir.name}.json")):
        report = json.loads(p.read_text())
    n = len(metrics)
    agg = {
        "run_id": run_dir.name,
        "harness": metrics[0]["harness"] if metrics else "?",
        "model": metrics[0]["model"] if metrics else "?",
        "instances": n,
        "resolved": report.get("resolved_instances") if report else None,
        "submitted": report.get("submitted_instances") if report else None,
        "resolve_rate": None,
        "wall_s_total": sum(m["wall_ms"] for m in metrics) / 1000,
        "wall_s_mean": (sum(m["wall_ms"] for m in metrics) / n / 1000) if n else 0,
        "input_tokens": sum(m["input_tokens"] for m in metrics),
        "output_tokens": sum(m["output_tokens"] for m in metrics),
        "cache_hit_tokens": sum(m["cache_hit_tokens"] for m in metrics),
        "cache_miss_tokens": sum(m["cache_miss_tokens"] for m in metrics),
        "cache_hit_rate": None,
        "cost_usd": sum(m["cost_usd"] or 0 for m in metrics),
        "empty_patches": sum(1 for m in metrics if m["empty_patch"]),
        "errors": sum(1 for m in metrics if m["error"]),
    }
    denom = agg["cache_hit_tokens"] + agg["cache_miss_tokens"]
    if denom:
        agg["cache_hit_rate"] = agg["cache_hit_tokens"] / denom
    if report and report.get("submitted_instances"):
        agg["resolve_rate"] = report["resolved_instances"] / report["submitted_instances"]
    return agg


def fmt(v, kind=""):
    if v is None:
        return "n/a"
    if kind == "pct":
        return f"{v:.1%}"
    if kind == "usd":
        return f"${v:.3f}"
    if kind == "s":
        return f"{v:,.0f}s"
    if kind == "k":
        return f"{v / 1000:,.1f}k"
    return str(v)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--runs", nargs="+", required=True)
    ap.add_argument("--format", default="md", choices=["md", "json"])
    args = ap.parse_args()

    rows = [load_run(Path(r)) for r in args.runs]
    if args.format == "json":
        print(json.dumps(rows, indent=2))
        return

    headers = ["harness", "model", "n", "resolved", "resolve %", "mean wall",
               "input tok", "output tok", "cache hit %", "cost (USD)", "empty", "err"]
    print("| " + " | ".join(headers) + " |")
    print("|" + "---|" * len(headers))
    for r in rows:
        resolved = "n/a" if r["resolved"] is None else f"{r['resolved']}/{r['submitted']}"
        print("| " + " | ".join([
            r["harness"], r["model"], str(r["instances"]), resolved,
            fmt(r["resolve_rate"], "pct"), fmt(r["wall_s_mean"], "s"),
            fmt(r["input_tokens"], "k"), fmt(r["output_tokens"], "k"),
            fmt(r["cache_hit_rate"], "pct"), fmt(r["cost_usd"], "usd"),
            str(r["empty_patches"]), str(r["errors"]),
        ]) + " |")


if __name__ == "__main__":
    main()
