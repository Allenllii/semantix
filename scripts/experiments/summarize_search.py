#!/usr/bin/env python3
"""Aggregate one `semantix search --json` result from stdin into 'zone score'."""
import json
import sys

d = json.load(sys.stdin)["data"]
r = (d or [{}])[0]
print(f"{r.get('zone', '?'):>4} {r.get('score', 0):.4f}")
