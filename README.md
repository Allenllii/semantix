<div align="center">

# Semantix

### Cross-session memory for AI coding agents — and a much smaller bill.

**Semantic Caching · Adaptive Scheduling · Speculative Prefetch · Cross-Session Learning**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg?style=flat-square)](./LICENSE)
[![Version](https://img.shields.io/badge/release-0.7.2-blue?style=flat-square)](https://github.com/Gnosil/semantix/releases)
[![GitHub stars](https://img.shields.io/github/stars/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/stargazers)
[![GitHub contributors](https://img.shields.io/github/contributors/Gnosil/semantix?style=flat-square&logo=github)](https://github.com/Gnosil/semantix/graphs/contributors)
[![Website](https://img.shields.io/badge/website-semantix.ensureok.ai-168b6d?style=flat-square)](https://semantix.ensureok.ai)

<a id="en"></a>
**English** | [简体中文](#zh)

</div>

<br/>

> **A coding agent loses its entire context when a session ends, and a provider's prefix cache hits only on byte-identical prompts — a single edit near the head invalidates everything after it.**
>
> Semantix addresses both: a complete coding agent with the memory kernel built in, and a standalone kernel that attaches to an agent already in use.

## Two ways to run it

**`semantix-agent` — the complete agent.** A CLI coding agent shipping with the memory kernel built in: extraction, retrieval, injection and the evolution loop are wired at boot, with provider presets for ~50 endpoints.

**The kernel — attached to an existing agent.** Harness-independent by design; the integration surface is a small set of hooks (tool registration, message interception, session export / event bypass). Three ways in:

| Path | Integration cost | Applies to |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **Tool registration** | two tool schemas (`semantix_lookup` / `semantix_inject`) | custom / self-hosted agents |
| **Gateway** | one base URL, no code change | any OpenAI-compatible client |

Fail-open throughout: on kernel error the agent falls back to its normal execution path.

## Cross-session memory

A project's build conventions, test layout and service flags have to be re-established in every new session. Semantix extracts reusable **slices** from finished sessions — task patterns, project knowledge, tool sequences, verified results — into a local scored library, and reinjects the relevant ones when a similar task appears. Each hit is labelled with its source session:

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

Hits, misses and manual corrections all feed back into slice scores and retrieval thresholds, so precision improves with use rather than volume alone. Type-aware eviction discards stale results first and retains project knowledge.

## Cache hit rate and cost

Provider prefix caches match byte-exactly: a hit requires the prompt to be identical to the previous one byte for byte, and a single differing character near the head invalidates everything after it.

The impact of that failure mode is routinely underestimated. Claude Code writes a per-request billing marker into the head of the system prompt. First-party endpoints strip it server-side; third-party endpoints do not, so there the line invalidates the cache from the first token. The spec behind the prefix-hygiene middleware attributes a **133× difference in hit rate** to that single header, and records cache spend rising **4–5×** when stripping is disabled. `semantix-gateway` strips it by default.

The design goal is to make the provider cache hittable rather than to work around it:

- **Byte-stable injection** — retrieved slices are ordered by ID rather than by score, so semantically similar requests produce byte-identical prefixes.
- **Prefix hygiene** — per-request attribution markers are stripped and the tool array is canonicalized by name, removing client enumeration order as a source of invalidation.
- **Provider awareness** — a vendor capability table, per-vendor cache lifetimes (DeepSeek's 24-hour on-disk context cache, Anthropic's 5-minute ephemeral window), budget-aware `cache_control` breakpoint placement, and per-model price tables.
- **L3 result reuse** — a verified result is returned without a model call, fail-closed.

### Per-provider adaptation

Cache behaviour is determined by the stack serving the model rather than by the model itself, and the spread is wide enough to require per-stack adaptation.

| Endpoint | Prompt-cache hit, steady turn | Where the number comes from |
| --- | --- | --- |
| DeepSeek | **99.8%** | provider-reported cache tokens |
| GLM | **97.5%** | week-long spike; ~97.6% telemetry ceiling |

Both are per-turn prompt-token hit rates — `hit / (hit + miss)` over cache token counts returned by the provider, not estimates. `semantix-agent` displays the current turn's rate alongside the session average in its status line, so the figure is verifiable on any workload.

A week-long GLM study measured the same model behind different hosts and found cache lifetimes differing several-fold: one stack held a prefix for 1–8 minutes with real expiry in `(8, 12]`; another maintained 96–98% for 120 seconds and had fallen to 28% at 301 seconds. This is why the TTL table is per-vendor rather than a single global value, and why GLM hit-rate telemetry carries a documented **~97.6%** reporting ceiling — trailing partial blocks never count as cached.

Method and raw runs: [docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md).

Beyond caching, the scheduler learns tool-usage patterns to parallelize eligible calls and to prefetch read-only context during model wait time.

### Measured

Real output from a small demo library (4 extracted sessions):

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

- **79.8% cost saved** on a synthetic replay comparison — methodology in [docs/reports/m0-cost-comparison.md](./docs/reports/m0-cost-comparison.md)
- **80% cache hit rate (L3/L2)** on the demo library above
- A replay gate (`semantix verify`) enforces **≥ 70% relevance**; validating that hit rate on real user sessions is the open v1.0 gate — [#58](https://github.com/Gnosil/semantix/issues/58)

**These are replay / demo measurements, not production benchmarks.** The full evidence trail — and everything else technical — lives in [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md).

## Install

Release binaries for **macOS and Linux** (arm64 / amd64) from [Releases](https://github.com/Gnosil/semantix/releases):

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

Full command reference, configuration and shell completion: [docs/QUICKSTART.md](./docs/QUICKSTART.md).

## Integrations

| Agent / harness | Integration path | Status |
| --- | --- | --- |
| DeepSeek-Reasonix | Built-in bundle: `[semantix] enabled=true` + `semantix_lookup` tool | ✅ shipped since v0.3.0 |
| Claude Code | Tool registration via `semantix_lookup` / `semantix_inject` schemas | ✅ documented (`agent-skill/tools/`) |
| LangChain apps | Middleware with two hooks (message rewrite + session extraction) | ✅ documented ([report](./docs/reports/langchain-middleware.md)) |
| Custom / self-hosted | Session bypass: export / event bypass / direct call | ✅ documented (`agent-skill/hooks/session-bypass.md`) |
| Any OpenAI-compatible client | `semantix-gateway` in front, custom base URL | ✅ shipped |

Cursor, Windsurf, Codex CLI, Gemini CLI, Copilot agent mode and Cline / Continue / Aider all reach the kernel through one of the paths above; none requires kernel changes. Priorities follow observed usage — concrete integration requests are tracked as [issues](https://github.com/Gnosil/semantix/issues) (template: `.github/ISSUE_TEMPLATE/integration_request.yml`).

## Documentation

| Doc | What's inside |
| --- | --- |
| [docs/TECHNICAL-OVERVIEW.md](./docs/TECHNICAL-OVERVIEW.md) | **Start here** — concepts, cache layers, scheduler, prefetch, evolution loop, architecture, module map, status, roadmap, metrics |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | Install, 30-second demo, command reference, configuration |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | Full architecture design (Chinese) |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 roadmap and definition-of-done (Chinese) |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | Threat model and security mechanisms |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | Self-serve integration for any harness |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | Website, product docs and the blog |

## Contributing

Semantix is early-stage. Contributions are welcome across semantic caching, retrieval, scheduling, speculative execution, evaluation methodology and harness adapters — see [CONTRIBUTING.md](./CONTRIBUTING.md). Branch naming `feat/<unit>`; PRs require green `go vet` and `go test -race`.

**Without writing code:** run `semantix verify` against your own sessions and post the resulting hit rate to [#58](https://github.com/Gnosil/semantix/issues/58). Aggregated community results decide the v1.0 gate.

Architectural assumptions are open to challenge; testing them is part of the work.

## License

[MIT](./LICENSE). [DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix) (MIT).

---

<div align="center">

<a id="zh"></a>

[English](#en) | **简体中文**

</div>

<br/>

> **编程 agent 在会话结束时丢失全部上下文；厂商的前缀缓存又仅在提示词逐字节一致时命中——靠近开头的一处改动，即令其后内容全部失效。**
>
> Semantix 同时处理这两个问题：既是内置记忆内核的完整编程 agent，也是可挂载至现有 agent 的独立内核。

## 两种形态

**`semantix-agent` —— 完整 agent。** 一个 CLI 编程 agent，记忆内核随包内置：提取、检索、注入与自进化闭环在启动时已接好，内置约 50 家端点的 provider 预设。

**内核 —— 挂载至现有 agent。** 设计上与 harness 解耦，集成面只是一小组 hook（工具注册、消息拦截、会话导出 / 事件旁路）。三种接法：

| 接入方式 | 集成成本 | 适用对象 |
| --- | --- | --- |
| **Agent skill** | `semantix install --target claude-code` | Claude Code |
| **工具注册** | 两个工具 schema（`semantix_lookup` / `semantix_inject`） | 自研 / 私有 agent |
| **网关** | 一个 base URL，无需改动代码 | 任何 OpenAI 兼容客户端 |

全程 fail-open：内核异常时，agent 回退至常规执行路径。

## 跨会话记忆

同一项目的构建约定、测试布局、服务启动参数，在每个新会话中都需重新建立。Semantix 从已结束的会话中提取可复用**切片**——任务模式、项目知识、工具序列、已验证结果——存入本地评分库，并在遇到相似任务时重新注入。每条命中均标注其来源会话：

```text
$ semantix search --query "修复 go 测试失败"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

命中、未命中与人工纠正均回流至切片评分与检索阈值，库的精度随使用提升，而非仅随体量增长；按类型分化的淘汰策略优先清除过期结果，保留项目知识。

## 缓存命中与成本

厂商的前缀缓存为**字节精确匹配**：仅当本次提示词与上次逐字节一致时命中，靠近开头的单个字符差异即令其后内容全部失效。

该失效模式的影响普遍被低估。Claude Code 在 system 提示词头部写入逐请求变化的计费标记；一方端点在服务端剥离该标记，第三方端点不剥离，因而在第三方端点上，该行导致缓存自首个 token 起即失效。前缀清洁中间件所依据的规格，将命中率差异归因于这一个 header，记录为 **133 倍**；关闭剥离后缓存开销升至 **4–5 倍**。`semantix-gateway` 默认执行剥离。

设计目标是使厂商缓存可命中，而非绕开它：

- **字节稳定注入** —— 检索结果按 ID 排序而非按分数排序，使语义相近的请求生成逐字节一致的前缀
- **前缀清洁** —— 剥离逐请求变化的归属标记，并将工具数组按名称规范化排序，消除客户端枚举顺序对前缀的干扰
- **厂商感知** —— 厂商能力表、按厂商区分的缓存有效期（DeepSeek 落盘 24 小时，Anthropic 5 分钟 ephemeral 窗口）、预算感知的 `cache_control` 断点放置、按模型价格表
- **L3 结果复用** —— 已验证结果直接返回，不产生模型调用，fail-closed

### 逐厂商适配

缓存行为取决于承载模型的服务栈，而非模型本身；其差异幅度足以要求逐栈适配。

| 端点 | 稳定轮次的 prompt 缓存命中 | 数字来源 |
| --- | --- | --- |
| DeepSeek | **99.8%** | 厂商回报的 cache token 计数 |
| GLM | **97.5%** | 一周专项实测；遥测上报天花板约 97.6% |

两者均为**单轮 prompt token 命中率**——`命中 / (命中 + 未命中)`，取自厂商返回的 cache token 计数，非估算值。`semantix-agent` 在状态栏同时显示当前轮次命中率与会话累计均值，该数值可在任意负载上直接核验。

为期一周的 GLM 专项实测显示：同一模型置于不同托管栈之后，缓存寿命相差数倍——某栈前缀保持 1–8 分钟，真实过期落在 `(8, 12]` 分钟；另一栈 120 秒内维持 96–98%，至 301 秒降至 28%。这是 TTL 表按厂商区分而非采用单一全局值的原因，也解释了 GLM 命中率遥测约 **97.6%** 的上报天花板——尾部不足一块的部分不计入 cached。

方法与原始数据：[docs/reports/glm-spike-week.md](./docs/reports/glm-spike-week.md) · [docs/reports/glm-p0-1-prefix-audit.md](./docs/reports/glm-p0-1-prefix-audit.md)。

缓存之外，调度器学习工具使用模式，将可并行的调用并行化，并在模型等待期预取只读上下文。

### 实测

真实命令输出（4 个会话的演示库实录）：

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
- 上述演示库 **80% 缓存命中率（L3/L2）**
- `semantix verify` 回放门禁要求相关性 **≥ 70%**；用真实用户会话验证命中率是当前的 v1.0 门禁 —— [#58](https://github.com/Gnosil/semantix/issues/58)

**以上是回放 / 演示测量，不是生产基准。** 完整证据链和全部技术细节见 [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md)。

## 安装

[Releases](https://github.com/Gnosil/semantix/releases) 提供 **macOS 与 Linux**（arm64 / amd64）二进制：

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh   # 安装 semantix-agent + semantix + 配置
```

或源码构建（Go 1.26+）：`go build -o semantix ./cmd/semantix`

**30 秒体验** —— 从历史会话提取切片并复用：

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search  --query "修复 go 测试失败" --db .semantix/project.db
semantix inject  --query "修复 go 测试失败" --db .semantix/project.db   # L2 注入块
semantix verify  --session <会话目录> --project demo                    # 回放门禁（≥70%）
semantix dashboard                                                      # 一屏复用仪表盘
```

完整命令参考、配置与 shell 补全：[docs/QUICKSTART.md](./docs/QUICKSTART.md)。

## 集成

| Agent / 助手 | 接入路径 | 状态 |
| --- | --- | --- |
| DeepSeek-Reasonix | 内置捆绑：`[semantix] enabled=true` + `semantix_lookup` 工具 | ✅ v0.3.0 起随包发布 |
| Claude Code | 工具注册（`semantix_lookup` / `semantix_inject` schema） | ✅ 已文档化（`agent-skill/tools/`） |
| LangChain 应用 | 两个 hook 的中间件（消息改写 + 会话提取） | ✅ 已文档化（[报告](./docs/reports/langchain-middleware.md)） |
| 自研 / 私有 agent | 会话绕行：export / 事件旁路 / 直接调用 | ✅ 已文档化（`agent-skill/hooks/session-bypass.md`） |
| 任何 OpenAI 兼容客户端 | 前面架 `semantix-gateway`，改 base URL | ✅ 已随包发布 |

Cursor、Windsurf、Codex CLI、Gemini CLI、Copilot agent 模式、Cline / Continue / Aider 均可经上述某一路径接入内核，**无一需要修改内核**。优先级依据实际使用情况确定；具体集成需求以 [issue](https://github.com/Gnosil/semantix/issues) 跟踪（模板：`.github/ISSUE_TEMPLATE/integration_request.yml`）。

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/TECHNICAL-OVERVIEW.zh-CN.md](./docs/TECHNICAL-OVERVIEW.zh-CN.md) | **从这里开始** —— 核心概念、三级缓存、调度与预取、复用可视化、模块地图、项目状态、安全设计 |
| [docs/QUICKSTART.md](./docs/QUICKSTART.md) | 安装、命令参考、shell 补全、配置 |
| [docs/Agent-Infra-架构设计.md](./docs/Agent-Infra-架构设计.md) | 完整架构设计（问题、分层、组件、风险、指标） |
| [docs/总体架构-流程树.md](./docs/总体架构-流程树.md) | 端到端流程树（含 mermaid） |
| [docs/Agile路线图.md](./docs/Agile路线图.md) | Agile 1–3 路线图与 DoD |
| [docs/Security-安全设计.md](./docs/Security-安全设计.md) · [SECURITY.md](./SECURITY.md) | 威胁模型与安全机制 |
| [agent-skill/SKILL.md](./agent-skill/SKILL.md) | 面向任意 harness 的自助集成 |
| [semantix.ensureok.ai](https://semantix.ensureok.ai) | 官网、产品文档与博客 |

## 参与贡献

Semantix 处于早期阶段。语义缓存、检索、调度、投机执行、评测方法、harness 适配器等方向均欢迎贡献，参与方式见 [CONTRIBUTING.md](./CONTRIBUTING.md)。分支命名 `feat/<unit>`，PR 需 `go vet` 与 `go test -race` 全绿。

**无需写代码的参与方式：** 以自有会话运行 `semantix verify`，将命中率结果提交至 [#58](https://github.com/Gnosil/semantix/issues/58)。社区汇总结果决定 v1.0 门禁是否通过。

架构假设均可质疑，验证它们本身即是这项工作的一部分。

## 许可与致谢

[MIT](./LICENSE)。[DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT）。

---

<div align="center">

### Semantix

**Every interaction should make the next one cheaper, faster, and smarter.**

**每一次交互，都让下一次更便宜、更快、更聪明。**

</div>
