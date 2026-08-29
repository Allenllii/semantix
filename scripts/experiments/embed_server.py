#!/usr/bin/env python3
"""Local OpenAI-compatible embeddings server for offline calibration
(W2 of the efficiency research plan). One endpoint:

  POST /embeddings  {"model": "...", "input": ["...", ...]}
  -> {"data": [{"embedding": [...]}, ...]}

Runs fastembed (ONNX, CPU) fully locally — no data leaves the machine.
Not a production service: loopback-only, no auth beyond the presence of
any Bearer header check being intentionally omitted.
"""

import argparse
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

from fastembed import TextEmbedding

MODEL = None
DIM = None


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if not self.path.endswith("/embeddings"):
            self.send_error(404)
            return
        n = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(n) or b"{}")
        texts = body.get("input") or []
        if isinstance(texts, str):
            texts = [texts]
        vecs = list(MODEL.embed(texts))
        out = {"data": [{"embedding": [float(x) for x in v]} for v in vecs]}
        payload = json.dumps(out).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, fmt, *args):
        pass


def main():
    global MODEL, DIM
    ap = argparse.ArgumentParser()
    ap.add_argument("--addr", default="127.0.0.1:8688")
    ap.add_argument("--model", default="BAAI/bge-small-zh-v1.5")
    args = ap.parse_args()
    print(f"loading {args.model} ...", flush=True)
    MODEL = TextEmbedding(model_name=args.model)
    DIM = len(next(MODEL.embed(["probe"])))
    print(f"ready dim={DIM}", flush=True)
    host, _, port = args.addr.rpartition(":")
    httpd = HTTPServer((host or "127.0.0.1", int(port)), Handler)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
