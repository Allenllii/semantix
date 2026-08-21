#!/usr/bin/env python3
"""convert.py — transform a TraceLab round_trace.jsonl into semantix ingest
session JSONL (Issue #263).

Input  : one JSON object per line — a TraceLab normalized step (one LLM
         invocation) with timing_events[] (user_message/reasoning/text/
         tool_call) and tools[].
Output : one directory with one *.{jsonl} per session, each line compatible
         with kernel/ingest.JSONLSource:
           {"role":"user",     "content": ..}
           {"role":"assistant","content": ..}
           {"role":"assistant","tool_calls":[{"id","name"}, ...]}
           {"type":"tool","tool_call_id": .., "name": .., "content": ..}

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
import sys


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
                    "type": "tool",
                    "tool_call_id": tid,
                    "name": tname,
                    "content": _char_marker(t),
                })
    flush_assistant()

    # Emit tool result lines right after the assistant step that called them.
    placed = 0
    out: list[dict] = []
    for r in rows:
        out.append(r)
        if r.get("role") == "assistant" and r.get("tool_calls"):
            for tr in tool_result_rows[placed:placed + len(r["tool_calls"])]:
                out.append(tr)
            placed += len(r["tool_calls"])
    if placed < len(tool_result_rows):
        out.extend(tool_result_rows[placed:])
    return out


def _char_marker(src: dict) -> str:
    """Sanitized data carries only counts, not bodies. Emit a compact marker
    so structure stays parseable and content can be backfilled later."""
    n = src.get("content_chars") or src.get("result_chars") or 0
    return f"(chars={n})"


def _content(kind: str, parts: list[str]) -> str:
    joined = "".join(parts) if parts else ""
    return joined if joined else f"({kind})"


def convert(trace: pathlib.Path, outdir: pathlib.Path) -> int:
    sessions: dict[str, list[dict]] = {}
    with trace.open() as f:
        for line in f:
            if not line.strip():
                continue
            step = json.loads(line)
            sid = step.get("session_id") or "unknown"
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


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--in", dest="src", type=pathlib.Path, required=True)
    ap.add_argument("--out", type=pathlib.Path, required=True)
    args = ap.parse_args()
    sys.exit(0 if convert(args.src, args.out) else 0)


if __name__ == "__main__":
    main()
