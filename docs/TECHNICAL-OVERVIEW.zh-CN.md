# Semantix — 技术总览（中文）

> 这是 [README](../README.md) 的完整技术版。主页刻意保持精简；下面的内容面向想了解架构、机制、当前状态和数字出处的读者。
>
> English version: [TECHNICAL-OVERVIEW.md](./TECHNICAL-OVERVIEW.md)

**目录**：[为什么需要 Semantix](#为什么需要-semantix) · [核心概念](#核心概念) · [复用可视化](#复用可视化) · [仓库模块地图](#仓库模块地图) · [项目状态](#项目状态) · [安全设计](#安全设计) · [文档索引](#文档索引)

---

## 为什么需要 Semantix？

现在的 AI 编程助手在**单个会话内**已经做得很好：记得住上下文、会调工具、能从失败里恢复。但一关窗口，这些积累几乎全部清零。开个新会话，意味着：

- 项目是什么样、怎么构建、有什么规矩——重新交代一遍
- 昨天刚查过的东西，今天重新查一遍
- 同样的文件被重新翻出来读一遍
- 已经付过钱生成的答案，重新付钱再生成一遍
- 你的干活习惯，它一点都不记得

```text
传统 agent：

会话 A ────────────X   上下文结束
会话 B ────────────X   上下文重建
会话 C ────────────X   相似工作重做

Semantix：

会话 A ───────┐
会话 B ───────┼────► Semantix ────► 语义知识 / 行为模式 / 可复用结果
会话 C ───────┘                          │
                                         ▼
                                   下一个会话更好
```

拆开看是四个具体问题（详见 [GEO.md](./GEO.md)）：

1. **现有缓存只认"一字不差"**——模型厂商的前缀缓存要字节完全一致才命中；"跑一下 Go 测试"和"确保 Go 测试全过"明明是一回事，它当成两回事，重新收一遍钱
2. **跨会话的相似工作每次从零开始**——同样的上下文重新读、同样的工具序列重新跑
3. **调度靠写死的规则**——哪些操作可以同时跑、什么任务用什么档位的模型，都不会跟着你的习惯变
4. **等待时间白白流走**——模型逐字往外吐回答的那几秒到几十秒，助手就干等着，什么都不准备

---

## 核心概念

### 语义切片库（Semantic Slice Library）

Semantix 从历史会话中提取可复用的语义单元（切片），持久化到本地库：

| 切片 | 含义 | 用途 | 例子 |
|---|---|---|---|
| **P-Slice** | 任务模板 / 提示词模式 | L2 上下文注入 | `提交前先跑测试` |
| **C-Slice** | 上下文知识 | L2 上下文注入 | 项目结构、构建命令 |
| **T-Slice** | 工具调用模式 | 调度 / 预取 | `grep → readFile → editFile` |
| **R-Slice** | 可复用结果 | L3 结果复用 | 重复的查询或解释 |
| **M-Slice** | 记忆单元 | 检索 / 进化 | 用户或项目偏好 |

切片不是永久的：按历史命中率、时效、意图相关度、用户反馈打分——低价值切片衰减，高价值切片更易检索。目标是让库**越来越准，而不是越来越大**。

### 三级语义缓存

```text
┌────────────────────────────────────────┐
│ L3 · 验证后结果复用                     │  只读任务带指纹验证直接复用，不调模型
├────────────────────────────────────────┤
│ L2 · 语义切片注入                       │  语义相似 → 稳定字节 → 喂养 L1
├────────────────────────────────────────┤
│ L1 · 厂商前缀 / 字节缓存                │  相同前缀字节的被动复用
└────────────────────────────────────────┘
```

核心创新是「**语义层喂养字节层**」：两个会话的任务语义相似但字节不同（"跑一下 Go 测试" vs "确保 Go 测试全过"），普通前缀缓存视为无关；Semantix 检索出同一个规范切片、按固定顺序**原样注入**前缀区——语义命中就转化成了厂商字节缓存的真实命中。不用改你的编程助手，也不依赖厂商出新 API。

配套机制：**冻结期**（参数变更后注入集 ≥1 小时不变，防止进化抖动摧毁自己喂养的字节缓存）、**污染检测**（注入内容被用户改掉/回滚会降权）、L3 **fail-closed**（验证不过不复用，正确性 > 命中率）。

### 调度器与预取器

- **内核调度器**（[`kernel/sched`](../kernel/sched)）：按任务 intent 联合决策工具并发分组、模型 tier、注入切片与预取目标，附带行为学习门
- **投机预取器**（[`kernel/prefetch`](../kernel/prefetch)）：用 T-Slice 转移矩阵在 LLM 流式输出的等待期预取下一轮**只读**资源，waste/hit 比例自我惩罚
- **价值评分与淘汰**（[`kernel/slice`](../kernel/slice)）：命中/注入在线记账 → 价值 = 时效衰减·使用频次·注入成功率·反馈 → 库有上限（默认 5000），超限按价值归档、可还原——库越用越准而非越用越大。[`kernel/evolve`](../kernel/evolve) 的 EWMA 全局调参仍是 MVP（闭环接线待做）

---

## 复用可视化

跨会话复用看得见——以下均为真实命令输出（演示库实录）：

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

检索命中带 zone 图标与来源会话标注：

```text
$ semantix search --query "fix failing go test"
1. 🟢 score=4.331011 zone=hit id=619551c54af5437a scope=project from:2026-08-14-c9d4
   fix failing go test after refactor
2. 🟢 score=3.852740 zone=hit id=73b12bb117664106 scope=project from:2026-08-13-b7c2
   fix failing go test in kernel slice extractor
🎯 3/3 hits in 3 sessions
```

`verify` 回放门禁一眼可读（✅hit / 🟡grey / ❌miss + 分布条形 + 结论行）：

```text
# done: 4 replayed turns; zones hit=3 grey=0 miss=1 grey_ratio=0.0% (target 30.0%)
# zones: hit ██████░░ grey ░░░░░░░░ miss ██░░░░░░
# ✅ PASS relevance=75.0% (≥70%)
```

在合成回放对照实验中，跨会话复用节省了 **79.8%** 的成本（[reports/m0-cost-comparison.md](./reports/m0-cost-comparison.md)）。

<!-- repository-module-guide:start -->
## 仓库模块地图

下表按当前仓库结构说明可观察职责与聚焦验证方式；设计目标不会在这里被写成已经验证的生产性能。

| 模块 | 路径 | 职责 | 聚焦验证 |
|---|---|---|---|
| CLI | [`cmd/semantix`](../cmd/semantix) | 命令注册、统一 JSON 信封、退出码契约、维护与评估命令。 | `go test ./cmd/semantix -race` |
| Agent 可执行入口 | [`cmd/semantix-agent`](../cmd/semantix-agent) | 打包后的 Semantix 衍生 Agent 入口，包含崩溃捕获与构建版本接线。 | `go test ./cmd/semantix-agent` |
| Gateway | [`gateway`](../gateway)、[`cmd/semantix-gateway`](../cmd/semantix-gateway) | OpenAI 兼容代理、Anthropic 转换、SSE 转发、检索注入与 fail-open 上游路由。 | `go test ./gateway ./cmd/semantix-gateway -race` |
| 配置 | [`kernel/config`](../kernel/config) | 按内置值、TOML、环境变量、CLI 覆盖顺序解析配置，并保留来源与类型化错误。 | `go test ./kernel/config -race` |
| 事件摄取 | [`kernel/ingest`](../kernel/ingest) | 读取 harness JSONL 事件流，将规范化会话送入提取流程，无需依赖在线 harness。 | `go test ./kernel/ingest -race` |
| 语义切片 | [`kernel/slice`](../kernel/slice) | 切片类型、作用域、元数据、提取、文件存储、追加日志、压缩与维护。 | `go test ./kernel/slice -race` |
| BM25 检索 | [`kernel/bm25`](../kernel/bm25) | 本地搜索与混合检索使用的词法索引和 CJK 感知分词器。 | `go test ./kernel/bm25 -race` |
| 向量嵌入 | [`kernel/embed`](../kernel/embed) | Embedder 契约、确定性 hash embedder、模型嵌入与内存余弦向量索引。 | `go test ./kernel/embed -race` |
| Lookup 工具 | [`kernel/lookup`](../kernel/lookup) | 只读 `semantix_lookup` schema 与执行器，向 Agent harness 暴露排序后的切片命中。 | `go test ./kernel/lookup` |
| L2 注入 | [`kernel/inject`](../kernel/inject) | 在预算内选择完整切片，输出确定、经过 marker 转义的复用块。 | `go test ./kernel/inject -race` |
| 检索分区 | [`kernel/zone`](../kernel/zone) | 检索、验证和演化共用的 hit、grey、miss 三区分类器。 | `go test ./kernel/zone -race` |
| L3 缓存 | [`kernel/cache`](../kernel/cache) | 保守的结果复用判定接口与 L3 decider；无法证明安全时回退正常执行。 | `go test ./kernel/cache -race` |
| 依赖指纹 | [`kernel/fingerprint`](../kernel/fingerprint) | 捕获并验证文件依赖；项目状态变化时使原本可复用的结果失效。 | `go test ./kernel/fingerprint -race` |
| 复用 Judge | [`kernel/judge`](../kernel/judge) | 规则门、可选 LLM judge、提示清洗和判定统计，用于高风险 L3 候选。 | `go test ./kernel/judge -race` |
| 结果提升 | [`kernel/promote`](../kernel/promote) | 保存 judge 批准的可复用结果，记录内容版本，并按来源切片级联失效。 | `go test ./kernel/promote -race` |
| 调度器 | [`kernel/sched`](../kernel/sched) | 为每轮生成并行分组、预算动作、模型层级提示、注入 ID 与预取提示。 | `go test ./kernel/sched -race` |
| 预取 | [`kernel/prefetch`](../kernel/prefetch) | 离线规划器、转移矩阵学习、浪费感知在线预测与只读执行 runner。 | `go test ./kernel/prefetch -race` |
| 参数演化 | [`kernel/evolve`](../kernel/evolve) | 使用 EWMA 调整检索阈值和注入预算，参数变化有边界且可检查。 | `go test ./kernel/evolve -race` |
| 事件契约 | [`kernel/event`](../kernel/event) | 类型化 kernel 事件、payload、wire format 与同步进程内总线。 | `go test ./kernel/event -race` |
| 用量核算 | [`kernel/usage`](../kernel/usage) | 记录逐轮 token/cache 事件，汇总基线成本、实际成本与估算节省。 | `go test ./kernel/usage -race` |
| Semantix harness | [`harness`](../harness) | 随仓 Agent runtime：provider、工具、权限、扩展、会话、恢复、远程执行和 UI 契约。 | `go test ./harness/... -race` |
| Harness 桥接 | [`harness/semantix`](../harness/semantix) | 将 harness 事件镜像为会话 JSONL，并输出复用摘要，同时保持 Semantix fail-open。 | `go test ./harness/semantix -race` |
| Agent Skill | [`agent-skill`](../agent-skill) | 面向外部 harness 的自助安装、工具 schema、会话绕行 hook 与自测。 | `bash agent-skill/scripts/selftest.sh` |
| 部署 | [`deploy`](../deploy) | Gateway Docker 镜像、Compose 拓扑与支持环境变量展开的示例配置。 | `docker compose -f deploy/docker-compose.yml config` |
| 自动化脚本 | [`scripts`](../scripts) | 跨会话 demo、发布构建器和 Go 引导工具，供本地与发布流程使用。 | 在干净工作区运行对应 demo 或发布脚本。 |
| 规范与证据 | [`docs`](.) | 架构、安全、路线图、规格与验收报告，用于区分设计目标和实测结果。 | 每个已交付单元均核对对应验收报告。 |
| 集成补丁 | [`patches`](../patches) | 面向外部 Semantix fork 的版本化交付补丁，包含漂移说明和明确预检步骤。 | `git apply --check patches/semantix-sched-prefetch.patch` |
| 博客源文件 | [`blog`](../blog) | 技术文章的版本化 Markdown 源；网站内容测试校验元数据、链接和编码。 | `cd site && npm run test:content` |
| 官网 | [`site`](../site) | Next.js 产品站、文档、博客渲染、结构化数据、生成的 `llms-full.txt` 与内容质量测试。 | `cd site && npm run check` |
| CI 与工作流 | [`.github/workflows`](../.github/workflows) | 运行 Go vet/race 测试、完整网站检查和站点部署流程，并配置并发控制。 | GitHub 必需检查：`Go checks` 与 `Website checks`。 |
<!-- repository-module-guide:end -->

---

## 项目状态

> **v0.7.1 已发布**（2026-08-22，「品牌统一版」——全仓、二进制与落盘契约统一为 Semantix 命名，见 [releases/v0.7.1.md](./releases/v0.7.1.md)），打包 `semantix-agent` + `semantix` + `semantix-gateway`。**Agile 2（自进化闭环）已随 v0.6.0 收尾**（2026-08-21）——内核编排调度、投机预取、进化闭环接线（U37–U43）全部落地，并带上 GLM-5.x 缓存适配首批（前缀卫生 + per-provider 命中计量，GLM P0）。规模化之前剩余的门槛是**真实数据的跨会话命中率验证**（[#58](https://github.com/Gnosil/semantix/issues/58)，Agile 1 的 v1.0 门禁）。

| Agile | 里程碑 | 状态 |
|---|---|---|
| **1** | 首个可下载、可品牌化的 agent（v1.0） | 🚧 M0 ✅ · M1 接近完成（门禁 [#58](https://github.com/Gnosil/semantix/issues/58)）· CLI v2 ✅ · 复用可视化 CLI 侧 ✅ |
| **2** | 自进化闭环——内核反向调配助手的并发 / 预算 / 模型档位 | ✅ 完成并发布（v0.6.0，2026-08-21）：H2/H3 编排 + 进化闭环接线（U37–U43） |
| **3** | 多助手生态——任意编程助手都能接入 | ⏳ 路径已文档化，未开始 |

技术阶段 P0（可观测）✅ · P1（切片库）🚧 · P2（语义缓存）🚧 · P3（调度）✅ MVP · P4（预取）✅ MVP · P5（进化）✅ 闭环已接线（U43）。GLM-5.x 已作为一等 provider 接入（可与 DeepSeek 通过 `semantix setup` 切换），缓存适配 P0 已随 v0.6.0 落地，P1/P2 规划中。完整路线图见 [Agile路线图.md](./Agile路线图.md)。

### 参与验证（社区第一入口 👋）

[Issue #58](https://github.com/Gnosil/semantix/issues/58) 是给每一位使用者的第一个任务，**不需要写代码**：下载 semantix → 用你自己的真实 agent 会话跑 `semantix verify` → 把命中率和 zone 分布贴回 issue。汇总结果将决定 M0-Gate 是否通过（≥70%）。

---

## 安全设计

- 切片库文件权限 `0600`、目录 `0700`（原子写 + 防 symlink）
- 所有输出经消毒：ANSI/C1 剥离、TSV 公式注入防护、注入块标记转义
- L3 复用 fail-closed（指纹验证不过不复用）；缓存层故障 fail-open（不阻塞主循环）
- 零第三方运行时依赖（单二进制，唯一外部依赖 bbolt）

详见 [Security-安全设计.md](./Security-安全设计.md) 与 [SECURITY.md](../SECURITY.md)。

---

## 文档索引

| 文档 | 内容 |
|---|---|
| [QUICKSTART.md](./QUICKSTART.md) | 安装、命令参考、shell 补全、配置 |
| [TECHNICAL-OVERVIEW.md](./TECHNICAL-OVERVIEW.md) | 英文完整技术总览（架构、设计原则、路线图、指标） |
| [Agent-Infra-架构设计.md](./Agent-Infra-架构设计.md) | 完整架构设计（问题、分层、组件、风险、指标） |
| [总体架构-流程树.md](./总体架构-流程树.md) | 端到端流程树（含 mermaid） |
| [Agile路线图.md](./Agile路线图.md) | Agile 1–3 路线图与 DoD |
| [GEO.md](./GEO.md) / [GEO-guide.md](./GEO-guide.md) | 面向 AI 引擎的项目语义档案与深度解读 |
| [Security-安全设计.md](./Security-安全设计.md) | 威胁模型与安全机制 |
| [reports/glm-spike-week.md](./reports/glm-spike-week.md) | GLM 缓存事实核验与 AtomClub→GLM-5.2 网关预实验（不外推为 Z.AI 直连结论） |

官网：[semantix.ensureok.ai](https://semantix.ensureok.ai)
