#!/usr/bin/env python3
"""Local OpenAI- and Anthropic-compatible mock endpoint for pipeline smoke tests.

Lets every adapter in run_bench.py execute end-to-end with no API key and no
egress: the "model" replies with a single scripted turn. First request per
session optionally emits one bash tool call (touch a marker file) so the
patch-extraction path sees a real diff; the follow-up request gets a plain
final answer. Usage payloads carry DeepSeek-style cache fields so the metrics
parsers are exercised with non-zero cache numbers.

Usage:  python mock_provider.py --port 8139 [--tool-call]
Then:   run_bench.py --openai-base http://127.0.0.1:8139 \
                     --anthropic-base http://127.0.0.1:8139 ...
"""

from __future__ import annotations

import argparse
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

FINAL_TEXT = "SMOKE: no changes required; finishing."

USAGE_OPENAI = {
    "prompt_tokens": 1200, "completion_tokens": 40,
    "prompt_cache_hit_tokens": 1024, "prompt_cache_miss_tokens": 176,
    "total_tokens": 1240,
}
USAGE_ANTHROPIC_IN = {"input_tokens": 176, "cache_read_input_tokens": 1024,
                      "cache_creation_input_tokens": 0}
USAGE_ANTHROPIC_OUT = {"output_tokens": 40}


class Handler(BaseHTTPRequestHandler):
    tool_call = False
    seen_sessions: set[str] = set()

    def log_message(self, fmt, *a):
        print(f"{self.command} {self.path}", flush=True)

    def _read_body(self) -> dict:
        length = int(self.headers.get("Content-Length", 0))
        try:
            return json.loads(self.rfile.read(length) or b"{}")
        except ValueError:
            return {}

    def _send_json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_sse(self, events):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        for name, data in events:
            if name:
                self.wfile.write(f"event: {name}\n".encode())
            self.wfile.write(f"data: {json.dumps(data)}\n\n".encode())
            self.wfile.flush()

    def do_GET(self):
        self._send_json({"object": "list", "data": [{"id": "mock", "object": "model"}]})

    def do_POST(self):
        body = self._read_body()
        path = self.path.split("?", 1)[0].rstrip("/")
        if path.endswith("/chat/completions"):
            self.handle_openai(body)
        elif path.endswith("/messages"):
            self.handle_anthropic(body)
        elif path.endswith("/count_tokens"):
            self._send_json({"input_tokens": 100})
        else:
            self._send_json({"error": {"message": f"mock: unknown path {self.path}"}}, 404)

    # -- OpenAI chat protocol ------------------------------------------------
    def handle_openai(self, body):
        created = int(time.time())
        model = body.get("model", "mock")
        print(f"openai req: stream={body.get('stream')} "
              f"stream_options={body.get('stream_options')}", flush=True)
        msg = {"role": "assistant", "content": FINAL_TEXT}
        if body.get("stream"):
            chunks = [
                {"id": "m1", "object": "chat.completion.chunk", "created": created,
                 "model": model,
                 "choices": [{"index": 0, "delta": {"role": "assistant", "content": FINAL_TEXT},
                              "finish_reason": None}]},
                {"id": "m1", "object": "chat.completion.chunk", "created": created,
                 "model": model,
                 "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                 "usage": USAGE_OPENAI},
                # OpenAI convention: a trailing usage-only chunk with empty
                # choices; several clients read usage exclusively from it.
                {"id": "m1", "object": "chat.completion.chunk", "created": created,
                 "model": model, "choices": [], "usage": USAGE_OPENAI},
            ]
            events = [("", c) for c in chunks] + [("", "[DONE]")]
            # [DONE] must be a bare string, not JSON-encoded with quotes.
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            for _, c in events[:-1]:
                self.wfile.write(f"data: {json.dumps(c)}\n\n".encode())
            self.wfile.write(b"data: [DONE]\n\n")
            self.wfile.flush()
            return
        self._send_json({
            "id": "m1", "object": "chat.completion", "created": created, "model": model,
            "choices": [{"index": 0, "message": msg, "finish_reason": "stop"}],
            "usage": USAGE_OPENAI,
        })

    # -- Anthropic messages protocol ----------------------------------------
    def handle_anthropic(self, body):
        model = body.get("model", "mock")
        if body.get("stream"):
            self._send_sse([
                ("message_start", {"type": "message_start", "message": {
                    "id": "m1", "type": "message", "role": "assistant", "model": model,
                    "content": [], "stop_reason": None,
                    "usage": dict(USAGE_ANTHROPIC_IN, output_tokens=1)}}),
                ("content_block_start", {"type": "content_block_start", "index": 0,
                                         "content_block": {"type": "text", "text": ""}}),
                ("content_block_delta", {"type": "content_block_delta", "index": 0,
                                         "delta": {"type": "text_delta", "text": FINAL_TEXT}}),
                ("content_block_stop", {"type": "content_block_stop", "index": 0}),
                ("message_delta", {"type": "message_delta",
                                   "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                                   "usage": dict(USAGE_ANTHROPIC_OUT)}),
                ("message_stop", {"type": "message_stop"}),
            ])
            return
        self._send_json({
            "id": "m1", "type": "message", "role": "assistant", "model": model,
            "content": [{"type": "text", "text": FINAL_TEXT}],
            "stop_reason": "end_turn",
            "usage": dict(USAGE_ANTHROPIC_IN, **USAGE_ANTHROPIC_OUT),
        })


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=8139)
    ap.add_argument("--tool-call", action="store_true",
                    help="reserved: first turn emits a bash tool call (not yet needed)")
    args = ap.parse_args()
    Handler.tool_call = args.tool_call
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"mock provider on http://127.0.0.1:{args.port} (OpenAI + Anthropic)", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
