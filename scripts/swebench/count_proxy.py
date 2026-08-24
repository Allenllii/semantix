#!/usr/bin/env python3
"""Token-metering pass-through proxy for OpenAI-compatible endpoints.

Some harnesses do not surface provider token usage (codex 0.80's chat wire
reports zeros). This proxy forwards requests unchanged to the upstream base
URL and appends one JSON line per model response to a ledger file with the
provider-reported usage — including DeepSeek's prompt_cache_hit_tokens /
prompt_cache_miss_tokens — so the benchmark can meter any harness at the wire.

Streaming responses are teed to the client while scanning SSE chunks for the
final usage object. stream_options.include_usage is injected so OpenAI-spec
upstreams emit usage on streams; DeepSeek emits it regardless.

Usage: python count_proxy.py --upstream https://api.deepseek.com --ledger usage.jsonl [--port 0]
Prints "READY <port>" on stdout once listening.
"""

from __future__ import annotations

import argparse
import json
import os
import ssl
import sys
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

LOCK = threading.Lock()


def normalize_tool_message_order(messages: list) -> tuple[list, bool]:
    """DeepSeek rejects histories where an assistant tool_calls message is not
    immediately followed by its tool replies. codex 0.80's chat wire serializes
    a text+tool_call turn as [assistant(tool_calls), assistant(text), tool(...)],
    wedging the turn's prose between the call and its reply — and may wedge a
    synthetic user warning there too. Fold assistant text back into the
    tool_calls message's content (reconstructing the single completion the
    model actually produced) and shift wedged user messages to after the tool
    replies they reacted to. Valid sequences pass unchanged."""
    out: list = []
    i = 0
    changed = False
    while i < len(messages):
        m = messages[i]
        if isinstance(m, dict) and m.get("role") == "assistant" and m.get("tool_calls"):
            pending = {tc.get("id") for tc in m["tool_calls"] if isinstance(tc, dict) and tc.get("id")}
            texts, users, tools = [], [], []
            j = i + 1
            while j < len(messages) and pending:
                n = messages[j]
                if not isinstance(n, dict):
                    break
                if n.get("role") == "tool" and n.get("tool_call_id") in pending:
                    tools.append(n)
                    pending.discard(n.get("tool_call_id"))
                    j += 1
                elif n.get("role") == "assistant" and not n.get("tool_calls"):
                    texts.append(n)
                    j += 1
                elif n.get("role") == "user":
                    users.append(n)
                    j += 1
                else:
                    break
            if not pending and (texts or users):
                merged = dict(m)
                parts = [merged.get("content") or ""] + [t.get("content") or "" for t in texts]
                merged["content"] = "\n\n".join(p for p in parts if p) or None
                out.append(merged)
                out.extend(tools)
                out.extend(users)
                changed = True
                i = j
                continue
        out.append(m)
        i += 1
    return out, changed


def inject_reasoning(messages: list, store: dict) -> bool:
    """DeepSeek's thinking mode requires each replayed assistant tool_calls
    message to carry the reasoning_content it was generated with; codex 0.80
    predates that field and drops it. Re-attach from what this proxy captured
    on the response side (keyed by tool_call id)."""
    changed = False
    for m in messages:
        if (isinstance(m, dict) and m.get("role") == "assistant"
                and m.get("tool_calls") and not m.get("reasoning_content")):
            for tc in m["tool_calls"]:
                reasoning = store.get(tc.get("id")) if isinstance(tc, dict) else None
                if reasoning:
                    m["reasoning_content"] = reasoning
                    changed = True
                    break
    return changed


def build_opener() -> urllib.request.OpenerDirector:
    ctx = ssl.create_default_context()
    cafile = os.environ.get("SSL_CERT_FILE") or os.environ.get("REQUESTS_CA_BUNDLE")
    if cafile:
        ctx.load_verify_locations(cafile)
    handlers = [urllib.request.HTTPSHandler(context=ctx)]
    proxy = os.environ.get("HTTPS_PROXY") or os.environ.get("https_proxy")
    if proxy:
        handlers.insert(0, urllib.request.ProxyHandler({"https": proxy, "http": proxy}))
    return urllib.request.build_opener(*handlers)


class Handler(BaseHTTPRequestHandler):
    upstream = ""
    ledger = ""
    opener: urllib.request.OpenerDirector | None = None
    protocol_version = "HTTP/1.1"
    # tool_call id -> reasoning_content captured from responses, replayed into
    # later requests (one proxy instance serves one agent session).
    reasoning_store: dict = {}

    def log_message(self, fmt, *a):
        pass

    def record(self, usage: dict, model: str) -> None:
        if not usage:
            return
        with LOCK, open(self.ledger, "a") as f:
            f.write(json.dumps({"ts": time.time(), "model": model, "usage": usage}) + "\n")

    def record_rejection(self, status: int, detail: bytes, body: bytes | None) -> None:
        """Forensics for provider 4xx: the message-sequence shape that was sent."""
        try:
            msgs = json.loads(body or b"{}").get("messages") or []
            shape = [{"role": m.get("role"),
                      "text": (m.get("content") or "")[:60] if isinstance(m.get("content"), str) else None,
                      "reasoning": bool(m.get("reasoning_content")),
                      "tool_calls": [tc.get("id") for tc in (m.get("tool_calls") or [])],
                      "tool_call_id": m.get("tool_call_id")} for m in msgs]
        except (ValueError, AttributeError):
            shape = None
        with LOCK, open(self.ledger, "a") as f:
            f.write(json.dumps({"ts": time.time(), "rejected": status,
                                "error": detail.decode("utf-8", "replace")[:300],
                                "shape": shape}) + "\n")

    def do_GET(self):
        self.forward("GET", None)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length else b""
        # Ask spec-compliant upstreams to attach usage to streams, and repair
        # provider-invalid tool-message ordering (codex 0.80 chat wire).
        try:
            payload = json.loads(body)
            dirty = False
            if payload.get("stream") and "stream_options" not in payload:
                payload["stream_options"] = {"include_usage": True}
                dirty = True
            if isinstance(payload.get("messages"), list):
                msgs, changed = normalize_tool_message_order(payload["messages"])
                if changed:
                    payload["messages"] = msgs
                    dirty = True
                if inject_reasoning(payload["messages"], self.reasoning_store):
                    dirty = True
            if dirty:
                body = json.dumps(payload).encode()
        except ValueError:
            pass
        self.forward("POST", body)

    def forward(self, method: str, body: bytes | None) -> None:
        url = self.upstream + self.path
        headers = {k: v for k, v in self.headers.items()
                   if k.lower() not in ("host", "content-length", "accept-encoding", "connection")}
        headers["Accept-Encoding"] = "identity"
        req = urllib.request.Request(url, data=body, headers=headers, method=method)
        try:
            resp = self.opener.open(req, timeout=600)
        except urllib.error.HTTPError as e:
            resp = e
        except Exception as e:
            self.send_response(502)
            msg = json.dumps({"error": {"message": f"count_proxy upstream error: {e}"}}).encode()
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(msg)))
            self.end_headers()
            self.wfile.write(msg)
            return

        status = resp.getcode()
        ctype = resp.headers.get("Content-Type", "application/json")
        self.send_response(status)
        self.send_header("Content-Type", ctype)
        streaming = "text/event-stream" in ctype
        if streaming:
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Connection", "close")
            self.end_headers()
            self.tee_stream(resp)
        else:
            data = resp.read()
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            if status >= 400 and method == "POST":
                self.record_rejection(status, data, body)
            try:
                obj = json.loads(data)
                self.record(obj.get("usage") or {}, obj.get("model", ""))
                for choice in obj.get("choices") or []:
                    msg = choice.get("message") or {}
                    reasoning = msg.get("reasoning_content")
                    if reasoning:
                        for tc in msg.get("tool_calls") or []:
                            if tc.get("id"):
                                self.reasoning_store[tc["id"]] = reasoning
            except (ValueError, AttributeError, TypeError):
                pass

    def tee_stream(self, resp) -> None:
        buffer = b""
        usage, model = {}, ""
        reasoning_parts: list[str] = []
        call_ids: list[str] = []
        while True:
            chunk = resp.read(4096)
            if not chunk:
                break
            try:
                self.wfile.write(chunk)
                self.wfile.flush()
            except (BrokenPipeError, ConnectionResetError):
                # Client hung up; keep draining upstream so usage still lands.
                pass
            buffer += chunk
            while b"\n\n" in buffer:
                event, buffer = buffer.split(b"\n\n", 1)
                for line in event.splitlines():
                    if not line.startswith(b"data:"):
                        continue
                    payload = line[5:].strip()
                    if payload == b"[DONE]":
                        continue
                    try:
                        obj = json.loads(payload)
                    except ValueError:
                        continue
                    model = obj.get("model") or model
                    if isinstance(obj.get("usage"), dict) and obj["usage"]:
                        usage = obj["usage"]  # last one wins (cumulative)
                    for choice in obj.get("choices") or []:
                        delta = choice.get("delta") or {}
                        if delta.get("reasoning_content"):
                            reasoning_parts.append(delta["reasoning_content"])
                        for tc in delta.get("tool_calls") or []:
                            if isinstance(tc, dict) and tc.get("id"):
                                call_ids.append(tc["id"])
                    # Anthropic-style events carry usage under message/delta.
                    msg = obj.get("message") or {}
                    if isinstance(msg.get("usage"), dict):
                        usage = {**usage, **msg["usage"]}
                        model = msg.get("model") or model
                    if obj.get("type") == "message_delta" and isinstance(obj.get("usage"), dict):
                        usage = {**usage, **obj["usage"]}
        if reasoning_parts and call_ids:
            reasoning = "".join(reasoning_parts)
            for cid in call_ids:
                self.reasoning_store[cid] = reasoning
        self.record(usage, model)


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream", required=True)
    ap.add_argument("--ledger", required=True)
    ap.add_argument("--port", type=int, default=0)
    args = ap.parse_args()
    Handler.upstream = args.upstream.rstrip("/")
    Handler.ledger = args.ledger
    Handler.opener = build_opener()
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"READY {server.server_address[1]}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
