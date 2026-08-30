#!/usr/bin/env python3
"""probe_w6_order.py — W6 ablation data prep: the order arm (efficiency plan W6).

Takes a probe_w0 output tree (project dirs with chronologically-prefixed
session files) and writes a parallel tree whose session files carry a
seeded-random rank prefix. Same sessions, same library contents — only the
accumulation order differs — so `semantix probe` over the two trees isolates
the curriculum effect (research plan H5): earlier sessions feed the library
for later ones (chronological) versus arbitrary order.

Random.Random is seeded for reproducible shuffling only — not a security
context, so a CSPRNG is deliberately not used.
"""

import argparse
import json
import pathlib
import random
import shutil
import sys


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--src", type=pathlib.Path, required=True,
                    help="probe_w0 output tree (project subdirs of *.jsonl)")
    ap.add_argument("--out", type=pathlib.Path, required=True)
    ap.add_argument("--seed", type=int, default=20260831)
    args = ap.parse_args()

    rng = random.Random(args.seed)
    manifest = {}
    for pdir in sorted(p for p in args.src.iterdir() if p.is_dir() and p.name != "control"):
        sessions = sorted(f.name for f in pdir.glob("*.jsonl"))
        if len(sessions) < 2:
            continue
        shuffled = sessions[:]
        rng.shuffle(shuffled)
        odir = args.out / pdir.name
        odir.mkdir(parents=True, exist_ok=True)
        names = []
        for rank, name in enumerate(shuffled):
            dst = odir / f"{rank:04d}-{name.split('-', 1)[1]}"
            shutil.copyfile(pdir / name, dst)
            names.append(dst.name)
        manifest[pdir.name] = {"chronological": sessions, "shuffled": names}
    (args.out / "manifest.json").write_text(json.dumps(manifest, indent=2))
    print(f"ok: shuffled {len(manifest)} project trees -> {args.out}", file=sys.stderr)


if __name__ == "__main__":
    main()
