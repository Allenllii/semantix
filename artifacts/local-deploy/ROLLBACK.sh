#!/usr/bin/env bash
set -euo pipefail
target="${1:?deployment root required}"
rm -f "$target/semantix-agent-v28.exe"
