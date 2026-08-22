#!/usr/bin/env python3
"""convert.py — transform a TraceLab round_trace.jsonl into semantix ingest
session JSONL (Issue #263).

Input  : one JSON object per line — a TraceLab normalized step (one LLM
         invocation) with timing_events[] (user_message/reasoning/text/
         tool_call) and tools[].
Output : one directory with one *.{jsonl} per session, each line compatible
         with kernel/ingest.JSONLSource (which reads the `role` field):
           {"role":"user",     "content": ..}
           {"role":"assistant","content": ..}
           {"role":"assistant","tool_calls":[{"id","name"}, ...]}
           {"role":"tool","tool_call_id": .., "name": .., "content": ..}

The sanitized TraceLab data strips real content (tools[].input, message
bodies) and keeps only character counts, so this converter preserves the
session turn structure and the tool-call sequence — the structure-level
signal used for transfer-matrix / hit-rate calibration. Content fields are
emitted as a compact marker so a later content-aware loader can fill them
from an unsanitized source if the license permits.
"""

import argparse
import json
import pathlib


def _rows_for_step(step: dict) -> list[dict]:
    """Convert one TraceLab step into semantix ingest rows (in order)."""
    rows: list[dict] = []
    events = step.get("timing_events") or []
    tools = {t.get("tool_call_id"): t for t in (step.get("tools") or [])}

    # Order of events within the step is authoritative for role turns.
    assistant_text: list[str] = []
    tool_calls: list[dict] = []
    tool_result_rows: list[dict] = []

    def flush_assistant() -> None:
        if assistant_text or tool_calls:
            row: dict = {"role": "assistant"}
            if assistant_text:
                row["content"] = _content("assistant", assistant_text)
            if tool_calls:
                row["tool_calls"] = tool_calls[:]
            rows.append(row)
        assistant_text.clear()
        tool_calls.clear()

    for ev in events:
        etype = ev.get("event_type")
        if etype == "user_message":
            flush_assistant()
            rows.append({"role": "user", "content": _char_marker(ev)})
        elif etype in ("text", "reasoning"):
            # Sanitized data exposes reasoning/text only as char counts.
            assistant_text.append(_char_marker(ev))
        elif etype == "tool_call":
            tid = ev.get("tool_call_id")
            tname = ev.get("tool_name")
            tool_calls.append({"id": tid, "name": tname})
            t = tools.get(tid)
            if t is not None:
                tool_result_rows.append({
                    "role": "tool",
                    "tool_call_id": tid,
                    "name": tname,
                    "content": _char_marker(t),
                })
    flush_assistant()

    # Emit each tool result right after the assistant row that called it,
    # keyed by tool_call_id so results attach to the correct call.
    results_by_id = {tr["tool_call_id"]: tr for tr in tool_result_rows}
    out: list[dict] = []
    emitted: set[str] = set()
    for r in rows:
        out.append(r)
        if r.get("role") == "assistant":
            for tc in r.get("tool_calls", []):
                tr = results_by_id.get(tc.get("id"))
                if tr is not None and tr["tool_call_id"] not in emitted:
                    out.append(tr)
                    emitted.add(tr["tool_call_id"])
    for tr in tool_result_rows:  # any results not paired to a row, in order
        if tr["tool_call_id"] not in emitted:
            out.append(tr)
            emitted.add(tr["tool_call_id"])
    return out


def _char_marker(src: dict) -> str:
    """Sanitized data carries only counts, not bodies. Emit a compact marker
    so structure stays parseable and content can be backfilled later."""
    n = src.get("content_chars") or src.get("result_chars") or 0
    return f"(chars={n})"


def _content(kind: str, parts: list[str]) -> str:
    joined = "".join(parts) if parts else ""
    return joined if joined else f"({kind})"


def convert(trace: pathlib.Path, outdir: pathlib.Path, only: set[str] | None = None) -> int:
    sessions: dict[str, list[dict]] = {}
    with trace.open() as f:
        for line in f:
            if not line.strip():
                continue
            step = json.loads(line)
            sid = step.get("session_id") or "unknown"
            if only is not None and sid not in only:
                continue
            sessions.setdefault(sid, []).append(step)

    outdir.mkdir(parents=True, exist_ok=True)
    count = 0
    for sid, steps in sessions.items():
        # Keep trace order; round_index is already chronological in the file.
        rows: list[dict] = []
        for s in sorted(steps, key=lambda x: x.get("round_index") or 0):
            rows.extend(_rows_for_step(s))
        safe = "".join(c for c in sid if c.isalnum() or c in "-_") or "session"
        with (outdir / f"{safe}.jsonl").open("w") as o:
            for r in rows:
                o.write(json.dumps(r, ensure_ascii=False) + "\n")
        count += 1
    return count


def read_ids(path: pathlib.Path) -> set[str]:
    """Read one session id per line (as produced by sample.py)."""
    ids = {ln.strip() for ln in path.read_text().splitlines() if ln.strip()}
    if not ids:
        raise SystemExit(f"no session ids in {path}")
    return ids


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--in", dest="src", type=pathlib.Path, required=True,
                    help="source trace round_trace.jsonl")
    ap.add_argument("--out", type=pathlib.Path, required=True,
                    help="output directory for per-session .jsonl files")
    ap.add_argument("--only", type=pathlib.Path, default=None,
                    help="optional file of session ids to subset (from sample.py)")
    args = ap.parse_args()
    only = read_ids(args.only) if args.only else None
    converted = convert(args.src, args.out, only=only)
    print(f"converted {converted} sessions to {args.out}")


if __name__ == "__main__":
    main()
