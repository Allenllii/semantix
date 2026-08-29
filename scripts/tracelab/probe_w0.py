#!/usr/bin/env python3
"""probe_w0.py — W0 cross-session probe data prep (Issue #263 / efficiency plan W0).

Groups TraceLab sessions by project, orders them chronologically inside each
project, converts each session to semantix session JSONL (reusing
convert.py's row logic), and writes:

  out/<project>/0000-<session_id>.jsonl  (chronological order)
  out-control/<session_id>.jsonl         (one session per project: the
                                          cross-project negative control)
  out/manifest.json

The probe then runs per project dir (`semantix probe --dir out/<project>
--query-mode tools`) and over the control dir; the same-project number minus
the control number is the within-project transfer signal.

Note: random.Random is seeded for reproducible sampling only — this is not a
security context, so a CSPRNG is deliberately not used.
"""

import argparse
import gzip
import json
import pathlib
import random
import sys

sys.path.insert(0, str(pathlib.Path(__file__).parent))
from convert import _rows_for_step  # noqa: E402


def write_session(path: pathlib.Path, steps: list[dict]) -> None:
    with path.open("w") as f:
        for step in steps:
            for row in _rows_for_step(step):
                f.write(json.dumps(row, ensure_ascii=False) + "\n")


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--trace", type=pathlib.Path, required=True,
                    help="syfi_coding_trace.jsonl.gz")
    ap.add_argument("--out", type=pathlib.Path, required=True)
    ap.add_argument("--max-projects", type=int, default=40)
    ap.add_argument("--max-sessions-per-project", type=int, default=8)
    ap.add_argument("--min-sessions", type=int, default=4,
                    help="projects with fewer sessions are skipped")
    ap.add_argument("--seed", type=int, default=20260829)
    args = ap.parse_args()

    # Pass 1: session -> (project, first timestamp).
    sessions: dict[str, dict] = {}
    with gzip.open(args.trace, "rt") as f:
        for line in f:
            if not line.strip():
                continue
            step = json.loads(line)
            sid = step.get("session_id")
            if not sid or sid in sessions:
                continue
            evs = step.get("timing_events") or []
            ts = str(evs[0].get("timestamp") or "") if evs else ""
            sessions[sid] = {
                "project": step.get("project") or "unknown",
                "first_ts": ts,
                "rounds": [],
            }

    # Group by project, chronological order, pick eligible projects.
    by_project: dict[str, list[str]] = {}
    for sid, meta in sessions.items():
        by_project.setdefault(meta["project"], []).append(sid)
    for proj in by_project:
        by_project[proj].sort(key=lambda s: (sessions[s]["first_ts"], s))
    candidates = [(p, sids) for p, sids in by_project.items()
                  if len(sids) >= args.min_sessions]
    rng = random.Random(args.seed)  # reproducibility seed, not crypto
    rng.shuffle(candidates)
    picked_projects = candidates[: args.max_projects]
    picked: dict[str, list[str]] = {}
    for proj, sids in picked_projects:
        picked[proj] = sids[: args.max_sessions_per_project]
    wanted = {sid for sids in picked.values() for sid in sids}
    print(f"projects picked: {len(picked)}, sessions picked: {len(wanted)}", file=sys.stderr)

    # Pass 2: collect rounds for picked sessions (file order is already
    # chronological by round within a session).
    with gzip.open(args.trace, "rt") as f:
        for line in f:
            if not line.strip():
                continue
            step = json.loads(line)
            sid = step.get("session_id")
            if sid in wanted:
                sessions[sid]["rounds"].append(step)

    # Write same-project dirs + one-session-per-project control dir.
    written = 0
    manifest = {"projects": {}, "control": []}
    for proj, sids in picked.items():
        pdir = args.out / proj
        pdir.mkdir(parents=True, exist_ok=True)
        names = []
        for rank, sid in enumerate(sids):
            out = pdir / f"{rank:04d}-{sid.replace(':', '_')}.jsonl"
            write_session(out, sessions[sid]["rounds"])
            names.append(out.name)
            written += 1
        manifest["projects"][proj] = names
        # Control: first (earliest) session of each project.
        cdir = args.out / "control"
        cdir.mkdir(parents=True, exist_ok=True)
        cpath = cdir / f"{proj}__{sids[0].replace(':', '_')}.jsonl"
        write_session(cpath, sessions[sids[0]]["rounds"])
        manifest["control"].append(cpath.name)

    (args.out / "manifest.json").write_text(json.dumps(manifest, indent=2))
    print(f"ok: wrote {written} sessions across {len(picked)} projects -> {args.out}")


if __name__ == "__main__":
    main()
