#!/usr/bin/env python3
"""sample.py — reproducible stratified subsample of TraceLab sessions
(Issue #263).

Stratifies by provider (claude / codex) and, when present, the leading tool
family of a session, then draws the requested number of sessions with a fixed
seed so the subset is reproducible. Only session IDs are emitted here; the
downstream convert step reads the source trace and keeps only sampled IDs.
"""

import argparse
import json
import pathlib
import random
import sys


def session_key(rows: list[dict]) -> str:
    """A coarse task-type proxy: 'provider | first-tool-family'."""
    providers = {r.get("provider") for r in rows}
    prov = sorted(providers)[0] if providers else "unknown"
    first_tool = None
    for r in rows:
        tools = r.get("tools") or []
        if tools:
            first_tool = tools[0].get("tool_name")
            break
    family = str(first_tool or "none")
    return f"{prov}|{family}"


def sample(src: pathlib.Path, out: pathlib.Path, n: int, seed: int) -> None:
    sessions: dict[str, list[dict]] = {}
    with src.open() as f:
        for line in f:
            if not line.strip():
                continue
            step = json.loads(line)
            sid = step.get("session_id")
            if sid:
                sessions.setdefault(sid, []).append(step)

    strata: dict[str, list[str]] = {}
    for sid, rows in sessions.items():
        strata.setdefault(session_key(rows), []).append(sid)

    rng = random.Random(seed)
    # Distribute n across strata proportional to natural frequency using the
    # largest-remainder method so the quotas sum exactly to min(n, available)
    # and no small stratum silently disappears.
    counts = {k: len(v) for k, v in strata.items()}
    total = sum(counts.values()) or 1
    quota = {k: n * c / total for k, c in counts.items()}   # float ideal
    base = {k: int(q) for k, q in quota.items()}
    remainder = {k: q - int(q) for k, q in quota.items()}
    left = min(n, total) - sum(base.values())
    # Grant the largest fractional remainders first; ties broken by seed order.
    for k in sorted(remainder, key=lambda k: (-remainder[k], k)):
        if left <= 0:
            break
        take = min(left, counts[k] - base[k])
        base[k] += take
        left -= take
    picked: list[str] = []
    for strat, ids in strata.items():
        picked += rng.sample(ids, base[strat])

    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text("\n".join(picked) + "\n")
    print(f"wrote {len(picked)} session ids to {out}", file=sys.stderr)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--in", dest="src", type=pathlib.Path, required=True)
    ap.add_argument("--out", type=pathlib.Path, required=True)
    ap.add_argument("--n", type=int, default=200)
    ap.add_argument("--seed", type=int, default=20260821)
    args = ap.parse_args()
    sample(args.src, args.out, args.n, args.seed)


if __name__ == "__main__":
    main()
