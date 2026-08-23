<div align="center">

# Semantix

### A self-evolving agent kernel that learns how you work.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.1-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

<a id="en"></a>
**English** | [简体中文](#zh)

</div>

<br/>

> **Agents should not start from zero every session.**
>
> Semantix sits between an AI coding agent and its resources. It learns what you reuse, how you work, and what you are likely to need next — then turns that knowledge into semantic cache hits, smarter scheduling, safe prefetch, and lower-cost agent execution.

## What Semantix does

Provider prefix caches only hit on byte-identical prompts, and every new session rebuilds context from zero. Semantix adds a persistent memory and optimization layer around your agent:

- **Semantic Slice Library** — extracts reusable prompt / context / tool-pattern / result slices from your past sessions into a local, scored library.
- **Three-layer semantic cache** — L2 injects canonical slices deterministically, turning semantically similar requests into byte-stable prefixes that feed the provider's L1 prefix cache; L3 reuses verified results without a model call, fail-closed.
- **Adaptive scheduling & speculative prefetch** — learns your tool patterns to parallelize work and prepare read-only context during LLM wait time.
- **Self-evolving loop** — every hit, miss and correction feeds back into slice scores and thresholds, so the library gets more precise the more you use it.

It ships as three binaries — `semantix-agent` (a bundled coding agent), `semantix` (the kernel CLI) and `semantix-gateway` (an OpenAI-compatible proxy that adds cross-session reuse to any client) — plus an [agent skill](./agent-skill/SKILL.md) for existing harnesses. Fail-open by design: if the kernel ever misbehaves, your agent just runs normally.

## What you get

Real CLI output from a small demo library (4 extracted sessions):

```text
$ semantix dashboard

  semantix dashboard — reuse snapshot
  ------------------------------------------------

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions
```

Every hit shows its retrieval zone (🟢 hit · 🟡 grey · ⚪ miss) and which past session it came from:

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

- **79.8% cost saved** on a synthetic replay comparison — methodology in [docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)
- **80% cache hit rate (L3/L2)** on the demo library above
- A replay gate (`semantix verify`) enforces **≥ 70% relevance**; validating that hit rate on real user sessions is the open v1.0 gate — [#58](https://github.com/Gnosil/semantix/issues/58)

These are replay / demo measurements, not production benchmarks. The full evidence trail — and everything else technical — lives in [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md).

## How to use it

**Install** — release binaries (macOS / Linux / Windows) from [Releases](https://github.com/Gnosil/semantix/releases):

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh        # installs semantix-agent + semantix + config
```

Or build from source (Go 1.26+): `go build -o semantix ./cmd/semantix`

**Try it in 30 seconds** — extract slices from a past session, then reuse them:

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "fix failing go test" --db .semantix/project.db
semantix inject  --query "fix failing go test" --db .semantix/project.db   # L2 reuse block
semantix verify  --session <session-dir> --project demo                    # replay gate (≥70%)
semantix dashboard                                                         # one-screen snapshot
```

**Plug it into your agent**:

```bash
semantix install --target claude-code      # agent skill → ~/.claude/skills/semantix/
semantix install --target semantix-agent   # bundled integration, zero-step
```

Or run `semantix-gateway` (OpenAI-compatible) in front of any client that supports a custom base URL.

Full command reference, configuration and shell completion: [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Future Agent Integrations

The kernel stays harness-independent — "one kernel, many harnesses". The integration surface is a small set of hooks (tool registration, message interception, session export / event bypass), so a new coding agent can be attached without touching the kernel.

| Agent / harness            | Integration path                                                             | Status                                    |
| -------------------------- | ---------------------------------------------------------------------------- | ----------------------------------------- |
| DeepSeek-Reasonix          | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool           | ✅ shipped since v0.3.0                   |
| Claude Code                | Tool registration via `semantix_lookup` / `semantix_inject` schemas           | ✅ path documented (`agent-skill/tools/`) |
| LangChain apps             | Middleware with two hooks (message rewrite + session extraction)              | ✅ path documented (`docs/reports/langchain-middleware.md`) |
| Custom / self-hosted agent | Session bypass: export / event bypass / direct call                          | ✅ path documented (`agent-skill/hooks/session-bypass.md`) |
| OpenAI Codex CLI           | Tool registration (function calling) + session export                         | 🔜 candidate — same path as Claude Code   |
| Cursor                     | Session export + context hook                                                 | 🔜 candidate                               |
| Windsurf                   | Session export + context hook                                                 | 🔜 candidate                               |
| GitHub Copilot (agent mode)| Function-calling tool registration                                            | 🔜 candidate                               |
| Gemini CLI                 | Tool registration + session export                                            | 🔜 candidate                               |
| Cline / Continue / Aider   | Tool registration or session-bypass                                           | 🔜 candidate                               |

Priorities follow where users actually work, so the 🔜 rows are candidates, not commitments. If you maintain or use one of these agents and want a concrete integration, open an [integration request](https://github.com/Gnosil/semantix/issues) — the repo ships a template (`.github/ISSUE_TEMPLATE/integration_request.yml`).

## Documentation

The README stays intentionally short; the professional material lives in [`docs/`](./docs):

| Doc | What's inside |
|---|---|
| [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md) | **Start here** — concepts, cache layers, scheduler, prefetch, evolution loop, architecture, module map, status, roadmap, metrics |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | Install, 30-second demo, command reference, configuration |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | Full architecture design (Chinese) |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 roadmap and definition-of-done (Chinese) |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | Threat model and security mechanisms |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | Self-serve integration for any harness |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | Website, product docs and the blog |

## Contributing

Semantix is still early — contributions, criticism, experiments, and architecture discussions are welcome: semantic caching, retrieval, scheduling, speculative execution, evaluation methodology, harness adapters, and more. See [CONTRIBUTING.md](./CONTRIBUTING.md) (branch naming `feat/<unit>`; PRs need green `go vet` + `go test -race`).

**No code required**: run `semantix verify` on your own real agent sessions and post the hit rate to [#58](https://github.com/Gnosil/semantix/issues/58) — aggregated community results decide the v1.0 gate.

If you disagree with an architectural assumption, open an issue. Testing the assumptions is part of building the project.

## License

[MIT](./LICENSE). Semantix uses [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (MIT) as its initial architecture baseline — independently implemented, with attribution — and ships its derived harness as `semantix-agent`.

---

<div align="center">

<a id="zh"></a>

[English](#en) | **简体中文**

</div>

<br/>

> **AI 编程助手不该每开一个新会话就"失忆"。**
>
> Semantix 是给 Claude Code、semantix-agent 这类 AI 编程助手加装的**记忆与优化层**。你平时怎么干活，它就在旁边默默记下来：哪些事你反复在做、哪些项目背景每次都要重新交代、哪些答案其实上次已经生成过。等你下次开新会话干类似的活，助手直接接着上次的积累走——回答更快，token 账单更薄。

## Semantix 做了什么

模型厂商的前缀缓存只认字节完全一致的请求，而每个新会话都要从零重建上下文。Semantix 在你的助手旁边加了一层持久的记忆与优化：

- **语义切片库** —— 从历史会话提取可复用的任务模板 / 项目知识 / 工具模式 / 结果切片，存进本地带评分的库
- **三级语义缓存** —— L2 把语义相似的请求确定性地注入为字节稳定的前缀，喂养厂商的 L1 前缀缓存；L3 对验证通过的结果直接复用、不调模型（fail-closed）
- **自适应调度与投机预取** —— 学你的工具使用模式，把能并行的并行掉，在等模型输出的间隙提前准备只读上下文
- **自进化闭环** —— 每次命中 / 未命中 / 纠正都会反馈进切片评分和阈值，库越用越准而不是越用越大

随包发布三个二进制：`semantix-agent`（内置编程助手）、`semantix`（内核 CLI）、`semantix-gateway`（OpenAI 兼容网关，任何支持自定义 base URL 的客户端都能透明接入），另有面向现有助手的 [agent skill](./agent-skill/SKILL.md)。全程 fail-open：内核出任何问题，你的助手照常工作。

## 实测效果

以下为真实命令输出（4 个会话的演示库实录）：

```text
$ semantix dashboard

  semantix dashboard — reuse snapshot
  ------------------------------------------------

  💰 Cost savings
     paid        $ 0.0060
     baseline    $ 0.0141
     saved       $ 0.0080  (56.99%)
     ██████████████░░░░░░░░░░

  🎯 Cache hit rate (L3/L2)
     4 / 5 turns  (80.00%)
     L3 1 · L2 3
     ███████████████████░░░░░

  🗂 Zone distribution (library replay)
     hit  ████ 4   grey ██████ 6   miss  0

  📦 Slice library
     10 slices · 3 cross-session sessions
```

- 合成回放对照实验中节省 **79.8%** 成本（[docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)）
- 上述演示库 **80% 缓存命中率（L3/L2）**；检索命中带 zone 图标（🟢 hit · 🟡 grey · ⚪ miss）与来源会话标注
- `semantix verify` 回放门禁要求相关性 **≥ 70%**；用真实用户会话验证命中率是当前的 v1.0 门禁 —— [#58](https://github.com/Gnosil/semantix/issues/58)

以上是回放 / 演示测量，不是生产基准。完整证据链和全部技术细节见 [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md)。

## 快速上手

**安装** —— [Releases](https://github.com/Gnosil/semantix/releases) 提供 macOS / Linux / Windows 二进制：

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh   # 安装 semantix-agent + semantix + 配置
```

或源码构建（Go 1.26+）：`go build -o semantix ./cmd/semantix`

**30 秒体验** —— 从历史会话提取切片，然后复用它们：

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "修复 go 测试失败" --db .semantix/project.db
semantix inject  --query "修复 go 测试失败" --db .semantix/project.db   # L2 注入块
semantix verify  --session <会话目录> --project demo                    # 回放门禁（≥70%）
semantix dashboard                                                      # 一屏复用仪表盘
```

**接入你的编程助手**：

```bash
semantix install --target claude-code      # 安装 agent skill 到 ~/.claude/skills/semantix/
semantix install --target semantix-agent   # 内置集成，零步骤
```

或把 `semantix-gateway`（OpenAI 兼容）架在任何支持自定义 base URL 的客户端前面。

完整命令参考、配置与 shell 补全：[docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 未来集成计划

内核保持与 harness 解耦——「一个内核，多个助手」。集成面只是一小组 hook（工具注册、消息拦截、会话导出 / 事件旁路），接入新的编程助手不需要动内核。

| Agent / 助手 | 接入路径 | 状态 |
|---|---|---|
| DeepSeek-Reasonix | 内置捆绑：`[semantix] enabled=true` + `semantix_lookup` 工具 | ✅ v0.3.0 起随包发布 |
| Claude Code | 工具注册（`semantix_lookup` / `semantix_inject` schema） | ✅ 路径已文档化（`agent-skill/tools/`） |
| LangChain 应用 | 两个 hook 的中间件（消息改写 + 会话提取） | ✅ 路径已文档化（`docs/reports/langchain-middleware.md`） |
| 自研 / 私有 agent | 会话绕行：export / 事件旁路 / 直接调用 | ✅ 路径已文档化（`agent-skill/hooks/session-bypass.md`） |
| OpenAI Codex CLI | 工具注册（function calling）+ 会话导出 | 🔜 候选——与 Claude Code 同路径 |
| Cursor | 会话导出 + 上下文 hook | 🔜 候选 |
| Windsurf | 会话导出 + 上下文 hook | 🔜 候选 |
| GitHub Copilot（agent 模式） | function-calling 工具注册 | 🔜 候选 |
| Gemini CLI | 工具注册 + 会话导出 | 🔜 候选 |
| Cline / Continue / Aider | 工具注册或会话绕行 | 🔜 候选 |

优先级跟着用户实际在哪干活走，🔜 是候选集合而非承诺。如果你在维护或使用其中某个助手、想要一个具体的集成，欢迎提 [integration request](https://github.com/Gnosil/semantix/issues)（模板：`.github/ISSUE_TEMPLATE/integration_request.yml`）。

## 文档

README 刻意保持精简，专业内容都在 [`docs/`](./docs)：

| 文档 | 内容 |
|---|---|
| [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md) | **从这里开始** —— 核心概念、三级缓存、调度与预取、复用可视化、模块地图、项目状态、安全设计 |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | 安装、命令参考、shell 补全、配置 |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | 完整架构设计（问题、分层、组件、风险、指标） |
| [docs/总体架构-流程树.md](./docs/总体架构-流程树.md) | 端到端流程树（含 mermaid） |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 路线图与 DoD |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | 威胁模型与安全机制 |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | 面向任意 harness 的自助集成 |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | 官网、产品文档与博客 |

## 参与贡献

Semantix 还很早期——欢迎贡献、质疑、实验和架构讨论：语义缓存、检索、调度、投机执行、评测方法、harness 适配器等等。参与方式见 [CONTRIBUTING.md](./CONTRIBUTING.md)（分支命名 `feat/<unit>`，PR 需附 `go vet` + `go test -race` 全绿验证）。

**不需要写代码的第一入口 👋**：用你自己的真实 agent 会话跑 `semantix verify`，把命中率和 zone 分布贴回 [#58](https://github.com/Gnosil/semantix/issues/58)——社区汇总结果将决定 v1.0 门禁是否通过。

如果你不同意某个架构假设，开 issue 聊——验证假设本身就是这个项目的一部分。

## 许可与致谢

[MIT](./LICENSE)。设计基线为 [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT）——按「参考不抄」原则独立实现、保留 attribution，其衍生 harness 以 `semantix-agent` 随包发布。

---

<div align="center">

### Semantix

**Every interaction should make the next one cheaper, faster, and smarter.**

**每一次交互，都让下一次更便宜、更快、更聪明。**

</div>
