# Semantix — 快速上手（Quickstart）

> 自进化 Agent Kernel：把 coding agent 的中间产物切片化、跨会话复用，
> 降低成本并让 kernel 逐步获得调配 harness 资源的能力。
> 架构细节见 `docs/reports/harness-refactor-blueprint.md` 与 `docs/Agent-Infra-架构设计.md`。

## 安装

### 方式一：GitHub Release（推荐）

**完整产品（v0.3.0+）**：`semantix-agent-v0.3.0-<platform>.tar.gz`——reasonix（coding-agent harness）+ semantix（记忆内核）+ 示例配置 + 安装脚本：

```bash
tar -xzf semantix-agent-v0.3.0-darwin-arm64.tar.gz
cd semantix-agent-v0.3.0-darwin-arm64
./semantix-install.sh v0.3.0   # 安装两个二进制 + 配置
reasonix --config reasonix.toml   # 启动完整 agent
```

**仅内核**：从 [Releases](https://github.com/Gnosil/semantix/releases) 下载对应平台二进制：

| 平台 | 文件 |
|---|---|
| macOS Intel | `semantix-v0.3.0-darwin-amd64` |
| macOS Apple Silicon | `semantix-v0.3.0-darwin-arm64` |
| Linux amd64/arm64 | `semantix-v0.3.0-linux-{amd64,arm64}` |
| Windows amd64/arm64 | `semantix-v0.3.0-windows-{amd64,arm64}.exe` |

```bash
chmod +x semantix-v0.3.0-darwin-arm64
sudo mv semantix-v0.3.0-darwin-arm64 /usr/local/bin/semantix
```

校验：`shasum -a 256 -c SHA256SUMS.txt`

### 方式二：源码构建

```bash
git clone https://github.com/Gnosil/semantix.git && cd semantix
go build -o semantix ./cmd/semantix   # 需要 Go 1.26+
```

## 30 秒体验

```bash
# 1. 从历史会话提取切片（Reasonix/Claude Code 风格 JSONL：每行 {role,content,tool_calls}）
semantix extract --input session.jsonl --db .semantix/project.db --project demo

# 2. 语义检索（三模式）
semantix search --query "修复 go 测试失败" --db .semantix/project.db
semantix search --query "修复 go 测试失败" --retriever vector
semantix search --query "修复 go 测试失败" --retriever hybrid

# 3. L2 注入块（会话 B 复用会话 A 的切片）
semantix inject --query "修复 go 测试失败" --db .semantix/project.db
# → [semantix-reuse] ... [/semantix-reuse]

# 4. 离线回放验证（M0-2：命中率 ≥70% 达标）
semantix verify --session <会话目录> --project demo > eval.tsv
# 逐行标 ✅/❌，命中率 = ✅/总行数
```

## 命令参考

命令树按四组组织，`semantix help` 按组列出全部命令；每个子命令
`semantix <command> --help` 查看其全部 flags。

**kernel 运维**

| 命令 | 用途 | 关键参数 |
|---|---|---|
| `extract` | 会话 JSONL → 语义切片入库 | `--input` `--db` `--scope` `--project` |
| `search` | 检索切片 | `--query` `--retriever bm25\|vector\|hybrid` `--limit` `--json` |
| `verify` | 离线回放命中率验证（门禁）；`--calibrate` 分桶校准报告 | `--session` `--holdout` `--db` `--strict` `--calibrate` `--labels` |
| `eval` | 检索策略比较（单阈值 vs 三段） | `--set` `--train-frac` `--tau-*` |
| `eval-judge` | LLM judge 真实性评估（门禁） | `--stub` `--audit` `--min-consistency` |
| `calibrate` | L3 负向校准报告（judge vs oracle + 运行时汇总） | `--audit` `--usage` `--stub` `--min-consistency` |
| `usage` | 成本节省统计 | `--db` `--evolve-db` |
| `lookup` | semantix_lookup 工具（JSON） | `--query` `--limit` `--scope` `--evolve-db` |
| `inject` | L2 注入块（规范序/预算截断） | `--query` `--budget` `--k` `--evolve-db` |

**产品与管理**：`doctor` 健康检查（db / config / embedder / judge，任一 FAIL 退出码 3）、
`install` 一键安装、`version`。

`semantix install` 按 `agent-skill/` 现有文档（SKILL.md + tools/ + hooks/ + config/ + scripts/）
落盘到目标 harness，幂等可重跑，`--uninstall` 精确移除安装的文件：

| 目标 | 默认落盘位置 | 说明 |
|---|---|---|
| `reasonix` | `~/.semantix/agent-skill/` | fork 已内置集成；落参考文档 + 打印 `[semantix] enabled=true` 配置步骤 |
| `claude-code` | `~/.claude/skills/semantix/` | Claude Code agent skills 目录，重启后生效 |
| `custom` | `--dir` 必填 | 任意目录；`--source`/`SEMANTIX_SKILL_DIR` 指定 agent-skill 源 |

```bash
semantix install --target claude-code          # 安装到 ~/.claude/skills/semantix/
semantix install --target custom --dir ./agent  # 自定义目录
semantix install --target claude-code --uninstall   # 卸载（仅移除 install 记录的文件）
```

**维护**：`gc`（清理过期/低权重切片，评分 + 上限淘汰）。切片库是单个 JSONL 文件，备份/恢复直接
`cp`（`slice.Export`/`Import` 的行格式仍是库内归档与 harness ccswitch 的内部机制，
不再暴露为独立 CLI 命令）。淘汰是**类型感知 + 确定性**的（Issue #277）：`result`/`tool_pattern`
先出局、`prompt`/`context` 最耐淘汰，`--json` 输出 `evicted_by_type` 分布；`gc` 默认重算价值
权重并按 `store.max_slices`（默认 5000）归档超限切片到 `<db>.archive.jsonl`。

**退出码契约**（所有命令统一）：

| 码 | 语义 |
|---|---|
| 0 | 成功 |
| 1 | 运行错误（IO、db、检索失败） |
| 2 | 用法错误（未知命令、flag 非法） |
| 3 | 门禁未达标（`verify --strict`、`eval-judge` 一致性） |

## 配置

复制 `semantix.example.toml` 为 `semantix.toml` 并按需修改（当前 CLI 参数优先）。

## 网关部署（Docker）

[Semantix Gateway](cmd/semantix-gateway)（`cmd/semantix-gateway`）是 OpenAI 兼容网关：
给任意支持自定义 `base_url` 的客户端（Claude Code / chatbox / IDE 插件）透明加上
L1/L2/L3 语义缓存。与 New API（API 中转面板）搭配部署，三条命令起步：

```bash
# 1. 生成配置（编辑其中的 ${VAR} 对应环境变量，或写入 .env）
cp deploy/semantix-gateway.toml.example deploy/semantix-gateway.toml

# 2. 设置密钥（只走环境变量，不落配置文件）
export SEMANTIX_GATEWAY_KEY=change-me        # New API 渠道密钥字段的值
export DEEPSEEK_API_KEY=change-me            # 上游 LLM 的 key

# 3. 起服务（New API 面板 + 网关两容器；网关 8080 仅内网）
docker compose -f deploy/docker-compose.yml up -d --build
```

验证：

```bash
docker compose -f deploy/docker-compose.yml ps          # 两服务均 healthy
curl http://127.0.0.1:3000/                             # New API 面板
curl http://127.0.0.1:3000/v1/models -H "Authorization: Bearer <New API token>"
```

然后在 New API 管理后台建渠道：类型「自定义」，代理地址 `http://semantix-gateway:8080`，
密钥填 `SEMANTIX_GATEWAY_KEY` 的值，模型填 `deepseek-chat`；渠道可用性检测用
`GET /healthz`（网关探活失败会返回 503，New API 自动禁用该渠道）。

完整设计见 `docs/specs/newapi-gateway-design.md`；非 Docker 直跑（systemd/launchd）
见该文档 §5.1。

### 客户端怎么判断「这次命中了没有」

- **非流式**：直接读响应头 `x-semantix-cache: hit | miss`；
- **流式（`stream:true`）：不要读响应头**。网关**有**发这个头（直连网关 `curl -D -` 能看到），
  但 New API 中继 SSE 时会把它剥掉——它的流式路径只复制一个写死的白名单
  （`X-Reasoning-Included` / `X-Codex-Turn-State`），其余上游响应头一律丢弃，且**没有配置项可以放行**
  ——白名单写死在代码里（读 New API v1.0.0-rc.25 源码确认，剥头本身是 GW4 实机实测）。
  这是 New API 的行为，不是网关缺陷。

  流式场景以**网关 usage 日志为准**（`[ingest] usage_log` 指向的 JSONL，`"l3_reuse":true` 即 L3 命中）：

  ```bash
  tail -n 5 .semantix/gateway-usage.jsonl        # 路径见 semantix-gateway.toml
  semantix usage --db .semantix/gateway-usage.jsonl
  ```

  配了灰区 judge（`[cache] judge_api_key`）且该轮确实有候选落进灰区时，这一行还会带
  `"judge":[{…}]`，写明是 judge 判了不可复用（`"verdict":"declined"`）、调用失败降级
  （`"verdict":"fail_closed"`）、还是指纹门在 judge 之前就拒了（`"verdict":"skipped"`，不产生
  judge 费用）——排障时先看这个字段，别去猜。细节见设计文档 §3.4.1 与 §4.3。

## 安全约定

- 切片库文件权限 `0600`、目录 `0700`（原子写 + 防 symlink）
- 所有输出经消毒（ANSI/C1 剥离、TSV 公式注入防护、注入块标记转义）
- 零第三方运行时依赖（单二进制）；检索默认 `hybrid`（BM25 + 哈希 embedding）

## 反馈

- 缺陷/建议：[Issues](https://github.com/Gnosil/semantix/issues)
- 架构与路线：`docs/reports/m0-gate.md`、`docs/reports/harness-refactor-blueprint.md`
