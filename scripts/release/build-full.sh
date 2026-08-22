#!/usr/bin/env bash
# build-full.sh — package the downloadable semantix-agent bundle (v0.5.0+
# merged-repo era): three binaries built from this repo's cmd/ entries
#   semantix-agent   — merged coding agent (harness vendor, in-process kernel)
#   semantix         — memory-kernel CLI
#   semantix-gateway — OpenAI-compatible semantic-cache gateway
# plus the two config templates (semantix-agent.example.toml,
# semantix.example.toml), the standalone kernel installer, and a README.
#
# History: up to v0.4.1 this script built `reasonix` from the external fork
# checkout (FORK_ROOT) + `semantix` from this repo. v0.5.0/v0.6.0 were packaged
# by hand (`go build -trimpath -ldflags "-s -w -X main.version=..."` per entry
# + manual tar): version was stamped but commit/build-time were not (`semantix
# version --json` reports commit "unknown"). This rewrite restores scripted
# builds with full version/commit/build-time stamping.
#
# Usage: build-full.sh --version v0.7.0 [--platforms darwin-arm64,linux-amd64]
# Default platforms: darwin-arm64, darwin-amd64, linux-amd64, linux-arm64
# Output (dist/):
#   semantix-agent-<version>-<platform>.tar.gz   full bundle for humans
#   semantix-agent-<platform>.tar.gz             flat single-binary asset the
#                                                self-updater downloads
#                                                (`semantix-agent upgrade`)
#   semantix-<version>-<platform>                raw kernel binary for the
#                                                agent-skill curl installer
#   SHA256SUMS + SHA256SUMS.txt                  same checksums, both names
#                                                (updater reads the former,
#                                                install.sh the latter)
set -euo pipefail

VERSION=""
PLATFORMS="darwin-arm64,darwin-amd64,linux-amd64,linux-arm64"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      VERSION="$2"; shift 2 ;;
    --platforms)
      PLATFORMS="$2"; shift 2 ;;
    --version=*) VERSION="${1#*=}"; shift ;;
    --platforms=*) PLATFORMS="${1#*=}"; shift ;;
    *)
      echo "unknown arg: $1" >&2; exit 2 ;;
  esac
done

# Anchored semver check (rejects v1..2, junk, injection).
if [[ ! "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  echo "invalid version: $VERSION (want vX.Y.Z[...])" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO="${GO:-go}"
OUT="$ROOT/dist"
rm -rf "$OUT"
mkdir -p "$OUT"

COMMIT="$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# -buildvcs=false: the -ldflags stamps are the build-identity source of truth.
# Go's own VCS detection walks to the primary checkout's .git when building
# from a linked worktree (.claude/worktrees/*) and stamps the wrong revision.
GOFLAGS_VCS="-buildvcs=false"

for plat in ${PLATFORMS//,/ }; do
  os="${plat%-*}"; arch="${plat#*-}"
  case "$os" in darwin|linux) ;; *) echo "unsupported os: $os" >&2; exit 2 ;; esac
  case "$arch" in amd64|arm64) ;; *) echo "unsupported arch: $arch" >&2; exit 2 ;; esac

  PKG="semantix-agent-$VERSION-$plat"
  D="$OUT/$PKG"
  mkdir -p "$D"

  echo "building semantix-agent ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath "$GOFLAGS_VCS" \
    -ldflags "-s -w -X main.version=$VERSION -X main.gitCommit=$COMMIT -X main.buildTimeUTC=$BUILD_TIME" \
    -o "$D/semantix-agent" ./cmd/semantix-agent)

  echo "building semantix ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath "$GOFLAGS_VCS" \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.buildTime=$BUILD_TIME" \
    -o "$D/semantix" ./cmd/semantix)

  # semantix-gateway has no ldflags version vars yet; strip-only build.
  echo "building semantix-gateway ($os/$arch)..."
  (cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" "$GO" build -trimpath "$GOFLAGS_VCS" \
    -ldflags "-s -w" \
    -o "$D/semantix-gateway" ./cmd/semantix-gateway)

  # Config templates and the standalone kernel installer are required package
  # content — fail loudly if missing.
  cp "$ROOT/semantix-agent.example.toml" "$D/"
  cp "$ROOT/semantix.example.toml" "$D/"
  cp "$ROOT/agent-skill/scripts/install.sh" "$D/semantix-install.sh"
  cat > "$D/README.md" <<EOF
# Semantix Agent $VERSION ($plat)

合体 coding agent 与自进化 memory kernel——每一次会话，都让下一次会话更快、更便宜。

## 包内容
- \`semantix-agent\` — 合体 coding agent（TUI/print/run/serve/ACP），进程内挂载
  semantix kernel：L2 语义注入、每 turn 复用面板（📦 命中 / 💰 节省 / 🗂 来源）、
  调度编排（并行分组 / tier / 预算阶梯 / 工具挂起）
- \`semantix\` — memory kernel CLI（extract / search / lookup / verify / usage / dashboard）
- \`semantix-gateway\` — OpenAI 兼容语义缓存网关（可选，任何 OpenAI 兼容客户端
  把 base_url 指过来即可）
- \`semantix-agent.example.toml\` — agent 配置模板（provider + [semantix] 段）
- \`semantix.example.toml\` — kernel 配置模板
- \`semantix-install.sh\` — 独立 curl 安装器，只把 semantix memory kernel
  装进别的 agent 环境

## 快速开始
1. cp semantix-agent.example.toml ~/.semantix/semantix-agent.toml，填 provider key，
   确认 [semantix] enabled = true（或直接跑 \`semantix-agent setup\` 向导）
2. ./semantix-agent            # 交互 TUI（或 -p "task" 单发）
3. 会话结束后 ./semantix usage # 成本与命中仪表盘

## 记忆
会话由 harness sink 落盘到 .semantix/sessions/；运行
    semantix extract -input <session>.jsonl -scope project
构建切片库，此后相似任务会自动注入相关切片。

发布说明：https://github.com/Gnosil/semantix/releases/tag/$VERSION
EOF
  chmod +x "$D/semantix-agent" "$D/semantix" "$D/semantix-gateway" "$D/semantix-install.sh"

  (cd "$OUT" && tar -czf "$PKG.tar.gz" "$PKG")

  # Flat single-binary archive for `semantix-agent upgrade`: the updater
  # expects semantix-agent-<os>-<arch>.tar.gz with the binary at the root.
  (cd "$D" && tar -czf "$OUT/semantix-agent-$plat.tar.gz" semantix-agent)
  # Raw kernel binary for agent-skill/scripts/install.sh (curl flow).
  cp "$D/semantix" "$OUT/semantix-$VERSION-$plat"
  rm -rf "$D"
  echo "packed $PKG.tar.gz + semantix-agent-$plat.tar.gz + semantix-$VERSION-$plat"
done

# One checksum set over every asset, published under both names: the
# self-updater downloads the asset literally named SHA256SUMS, while the
# agent-skill install.sh fetches SHA256SUMS.txt.
(cd "$OUT" && shasum -a 256 semantix-agent-*.tar.gz semantix-"$VERSION"-* > SHA256SUMS && cp SHA256SUMS SHA256SUMS.txt)
echo "---"
ls -lh "$OUT" | awk '{print $5, $9}'
