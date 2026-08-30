#!/usr/bin/env python3
"""Stub OpenAI chat-completions upstream for the live gateway smoke test.
Echoes the request's system+messages tail into the completion text (so the
L2 injection block is observable), emits DeepSeek-style usage fields, and
logs every request body it receives to a JSONL file for inspection."""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, HTTPServer

LOG = sys.argv[2] if len(sys.argv) > 2 else "/tmp/semantix-run/upstream.jsonl"


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self.send_error(404)
            return
        n = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(n))
        with open(LOG, "a") as f:
            f.write(json.dumps({"ts": time.time(), "body": req}, ensure_ascii=False) + "\n")
        # Surface the injection block: last system segment + last user turn.
        sys_txt = " ".join(m.get("content", "") for m in req.get("messages", []) if m.get("role") == "system")
        has_reuse = "[semantix-reuse]" in sys_txt
        tail = sys_txt[-260:] if has_reuse else "(no injection block)"
        prompt_tokens = sum(len(str(m.get("content", ""))) // 4 for m in req.get("messages", []))
        out = {
            "id": "chatcmpl-stub",
            "object": "chat.completion",
            "model": req.get("model", "stub"),
            "choices": [{
                "index": 0,
                "message": {"role": "assistant", "content": f"stub-ok injection={has_reuse} tail={tail}"},
                "finish_reason": "stop",
            }],
            "usage": {
                "prompt_tokens": prompt_tokens,
                "completion_tokens": 30,
                "total_tokens": prompt_tokens + 30,
                "prompt_tokens_details": {"cached_tokens": 0},
                "prompt_cache_hit_tokens": 0,
                "prompt_cache_miss_tokens": prompt_tokens,
            },
        }
        payload = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_GET(self):
        if self.path.endswith("/models"):
            body = b'{"data":[{"id":"stub-chat"}]}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_error(404)

    def log_message(self, fmt, *args):
        pass


if __name__ == "__main__":
    addr = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1:8690"
    host, _, port = addr.rpartition(":")
    HTTPServer((host or "127.0.0.1", int(port)), Handler).serve_forever()
