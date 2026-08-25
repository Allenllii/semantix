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
    verbose = False
    opener: urllib.request.OpenerDirector | None = None
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *a):
        pass

    def debug(self, msg: str) -> None:
        if self.verbose:
            print(msg, file=sys.stderr, flush=True)

    def record(self, usage: dict, model: str) -> None:
        if not usage:
            return
        with LOCK, open(self.ledger, "a") as f:
            f.write(json.dumps({"ts": time.time(), "model": model, "usage": usage}) + "\n")

    def do_GET(self):
        self.forward("GET", None)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length) if length else b""
        # Ask spec-compliant upstreams to attach usage to streams.
        try:
            payload = json.loads(body)
            if payload.get("stream") and "stream_options" not in payload:
                payload["stream_options"] = {"include_usage": True}
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
        self.debug(f"{method} {self.path} -> {status}")
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
            try:
                obj = json.loads(data)
                self.record(obj.get("usage") or {}, obj.get("model", ""))
            except ValueError:
                pass

    @staticmethod
    def usage_from_event(obj: dict) -> tuple[dict, str]:
        """Pull a usage object out of one SSE event, across protocols:
        OpenAI chat chunks (top-level usage), OpenAI Responses events
        (response.completed carries response.usage), Anthropic messages
        (message_start.message.usage input side, message_delta.usage output)."""
        usage, model = {}, ""
        if isinstance(obj.get("usage"), dict) and obj["usage"]:
            usage = obj["usage"]
            model = obj.get("model") or ""
        response = obj.get("response") or {}
        if isinstance(response, dict) and isinstance(response.get("usage"), dict) \
                and response["usage"]:
            usage = response["usage"]
            model = response.get("model") or model
        msg = obj.get("message") or {}
        if isinstance(msg, dict) and isinstance(msg.get("usage"), dict):
            usage = {**usage, **msg["usage"]}
            model = msg.get("model") or model
        return usage, model

    def tee_stream(self, resp) -> None:
        buffer = b""
        usage, model = {}, ""
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
                    got, got_model = self.usage_from_event(obj)
                    if got:
                        usage = {**usage, **got}  # last/cumulative wins
                    model = got_model or obj.get("model") or model
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
