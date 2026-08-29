#!/usr/bin/env python3
"""fetch.py — download a TraceLab public coding-agent trace subset for offline
semantix evaluation (Issue #263).

The dataset is licensed CC BY 4.0 (credit TraceLab / SyFI Lab, UW). Only the
script and expected SHA-256 checksums live in this repo — the data itself is
downloaded to a caller-chosen path and never committed.

This is a skeleton: the real byte checksums must be filled in once a
network with direct access to the TraceLab release asset is available (this
host's clone of github.com/uw-syfi/TraceLab timed out).
"""

import argparse
import hashlib
import pathlib
import sys
import urllib.request

TRACELAB_REPO = "uw-syfi/TraceLab"
# Published release asset names (from LICENSE-DATASET.md / repo docs).
ASSET = "syfi_coding_trace.jsonl.gz"

# SHA-256 of ASSET. Verified 2026-08-29 from an actual download (see
# docs/reports/ for the measurement run); replace if upstream re-releases.
EXPECTED_SHA256 = {
    ASSET: "11ce51ec0a25e3d1d95b025bca2f7d1647e47571eb7cc968acd5fc64d4b4fb65",
}


def _sha256(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def download(out: pathlib.Path, allow_unverified: bool = False) -> pathlib.Path:
    """Fetch the trace asset to out/, verifying SHA-256 when non-placeholder."""
    url = f"https://github.com/{TRACELAB_REPO}/releases/latest/download/{ASSET}"
    dst = out / ASSET
    out.mkdir(parents=True, exist_ok=True)
    print(f"downloading {url} -> {dst}", file=sys.stderr)
    download_to(dst, url)
    got = _sha256(dst)
    want = EXPECTED_SHA256[ASSET]
    if want == "PLACEHOLDER-SHA256":
        # The repo-checked checksum is not filled yet: refuse to hand back an
        # unverified artifact unless explicitly overridden.
        if not allow_unverified:
            raise SystemExit(
                "checksum is a placeholder (not yet verified); "
                "pass --allow-unverified to accept the download anyway"
            )
        print(f"NOTE: checksum placeholder accepted via --allow-unverified ({got})",
              file=sys.stderr)
        return dst
    if got != want:
        print(f"checksum mismatch: got {got}, want {want}", file=sys.stderr)
        raise SystemExit(2)
    return dst


def download_to(dst: pathlib.Path, url: str) -> None:
    # Skeleton: streaming download from the release asset URL.
    req = urllib.request.Request(url, headers={"User-Agent": "semantix-tracelab"})
    with urllib.request.urlopen(req, timeout=60) as resp, dst.open("wb") as f:
        while True:
            chunk = resp.read(1 << 20)
            if not chunk:
                break
            f.write(chunk)


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--out", type=pathlib.Path, default=pathlib.Path("tracelab-data"),
                    help="destination directory for the downloaded asset")
    ap.add_argument("--allow-unverified", action="store_true",
                    help="accept a download even when the checksum is a placeholder")
    args = ap.parse_args()
    dst = download(args.out, allow_unverified=args.allow_unverified)
    print(f"ok: {dst}")


if __name__ == "__main__":
    main()
