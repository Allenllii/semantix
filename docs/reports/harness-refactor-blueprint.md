# Harness 重构蓝图（核心战略）

> 日期：2026-08-10 · 状态：规划（v1） · 对应：Agile 1 / U13-U15 + UI 重构
> **一句话目标：基于 Reasonix fork，重构出一个「资源可被 kernel 调配」的 Harness——让 kernel 成为 agent 的资源大脑，而不只是旁观者。**

---

## 1. 愿景与核心命题

**核心命题**：agent 的智能上限由「模型 × 上下文 × 资源」共同决定。当前 agent（Reasonix 等）的资源使用是**静态、单点、不可编排**的（工具集固定、并发固定、缓存只靠前缀）。我们的 kernel 已经验证了「切片复用闭环」（79.8% 成本节省），下一步是**让 kernel 能调配 harness 的全部资源**——这是本项目区别于所有现有 agent 的护城河。

**为什么必须重构 harness（而不是继续只做 kernel）**：
- kernel 再聪明，也只能通过 harness 暴露的口子发力——Reasonix fork 的挂载点（事件/注入/工具）是 U13 的最小接入，但要「调配所有资源」必须**深度重构 harness 的资源模型**
- Reasonix 有现成的完整能力（TUI、桌面端、serve、ACP、MCP、hooks、插件），重写会浪费 3-6 个月；**重构（基于其骨架改资源模型）** 是唯一理性路径

## 2. 现状盘点

### 2.1 我们已有的（kernel 侧）
| 能力 | 状态 |
|---|---|
| 事件契约（12 种）+ 事件旁路（U7） | ✅ kernel/event + kernel/ingest |
| 语义切片库 + BM25/向量检索（U5/U12） | ✅ kernel/slice + bm25 + embed |
| L2 注入 + lookup 工具（U8） | ✅ kernel/inject + lookup |
| 调度接口冻结（U1） | ✅ kernel/sched（接口，未实现） |
| 预取/进化占位 | 📦 kernel/prefetch + evolve（接口） |

### 2.2 Reasonix（harness 侧）现状
- **前端**：CLI TUI（chatREPL/runAgent）、Wails 桌面端（desktop/）、HTTP serve（runServe）、ACP
- **后端**：agent 主循环（runToolLoop）、工具注册表（builtin 20+）、provider（DeepSeek 前缀缓存）、memory/skills、权限门控、事件流（internal/event）
- **结构**：单 Controller 多前端（control.Controller）——**这是重构的最大资产**（一个后端骨架，换皮换资源模型即可）

## 3. 资源调配架构（核心设计）

```
┌─────────────────────────────────────────────────────────┐
│                     KERNEL（资源大脑）                     │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ sched     │ │ cache     │ │ prefetch │ │ evolve   │    │
│  │ 意图→资源  │ │ 三层缓存   │ │ 投机预取  │ │ 自进化    │    │
│  └────┬─────┘ └──────────┘ └──────────┘ └──────────┘    │
│  ┌────▼──────────────────────────────────────────────┐   │
│  │ ResourceRegistry（资源注册表）—— kernel 侧资源视图    │   │
│  │ tools / models / memory / sessions / jobs / budget │   │
│  └────┬──────────────────────────────────────────────┘   │
└───────┼───────────────────────────────────────────────────┘
        │ ① 资源目录上报（事件旁路扩展）
        │ ② 调度指令下发（控制通道）
┌───────▼───────────────────────────────────────────────────┐
│               HARNESS（Reasonix fork 重构）                 │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ ResourceLayer（新层）—— 现有骨架上的资源抽象           │  │
│  │  · ToolRegistry → 可挂起/恢复/并发分组（现在固定）      │  │
│  │  · ModelTier → 可选模型/缓存策略（现在固定 provider）  │  │
│  │  · MemoryBus → kernel 可读写记忆/切片（现在内部）      │  │
│  │  · JobQueue → 后台任务可见可调度（现在隐藏）           │  │
│  │  · BudgetController → token/成本配额（现在硬编码）     │  │
│  └──────────────────────────────────────────────────────┘  │
│  Agent.Run / tools / provider / memory / UI（骨架不动）     │
└────────────────────────────────────────────────────────────┘
```

**三个核心机制**：
1. **资源目录（Resource Catalog）**：harness 把自身资源建模为统一对象（名称/类型/容量/状态/成本），经事件总线上报 kernel——kernel 得到**全量资源视图**
2. **调度指令通道（Control Channel）**：kernel 的 sched 输出「意图 → 资源分配方案」经指令通道下发 harness 执行（挂起低价值工具、调整并发、切换缓存 tier、预热预取对象）
3. **闭环反馈**：harness 执行结果（命中/拒绝/延迟/成本）经事件回流 → evolve 调整策略

**与 U7/U8 的关系**：U7 事件旁路 = 只读观测；U8 注入 = 单点干预；**本设计 = 全量资源编排**（观测 → 决策 → 执行 → 进化闭环）。

## 4. UI 重构方向（我们的设计语言）

**用户群体理解**（垂类假设）：
- 中文开发者为主，**专注垂类工作流**（修测试/CI/部署/文档），任务重复度高
- 使用场景：终端 TUI（主力）+ 桌面端（可视）+ 网页（展示）
- 痛点：现有 TUI 信息密度高但**不显示"复用/省了多少"**；桌面端与 CLI 割裂

**UI 设计语言（Semantix Design）**：
1. **复用可视化**：每个 turn 显示「命中切片数 + 节省成本 + 来源会话」——把 kernel 的价值变成用户可感知的界面元素（这是 Reasonix TUI 没有的杀手锏）
2. **资源仪表**：侧栏实时显示资源占用（模型/缓存/并发/预算）——用户能"看到"kernel 在调配资源
3. **深色主题 + 语义绿**（与 landing page #2F967F 一致）：建立品牌一致性
4. **桌面端重构**：Wails 框架保留，重画 UI 壳（复用可视化面板 + 资源仪表）

## 5. 分阶段路线（每阶段独立可交付）

| 阶段 | 内容 | 交付 | 依赖 |
|---|---|---|---|
| **H1 接入**（≈U13） | fork Reasonix → 挂载 U7/U8（事件旁路 + 注入工具）→ 跑通跨会话闭环 | 可用的「带 kernel 的 Reasonix」 | kernel M0 ✅ |
| **H2 资源层**（≈U14） | ResourceLayer 落地：ToolRegistry 可调度化 + BudgetController + 资源目录上报 | kernel 能看到并控制 harness 资源子集 | H1 |
| **H3 编排**（≈U15） | kernel sched 落地：意图→资源分配指令下发 + 反馈闭环 | 首个「kernel 调配 harness」演示 | H2 |
| **H4 UI 重构** | Semantix Design：TUI 复用可视化 + 资源仪表；桌面端重画 | 品牌化 UI | H2（并行可做） |
| **H5 预取/进化** | prefetch + evolve 落地（接入编排闭环） | 自进化 agent | H3 |

**每阶段验收标准**：H1（跨会话注入闭环真实会话跑通）；H2（工具可挂起/恢复 + 预算配额生效）；H3（调度演示：kernel 决策改变 harness 行为 + 可量化收益）；H4（UI 走查 + 用户试用）；H5（命中率/成本随使用提升曲线）。

## 6. 关键决策与风险

| 决策 | 选项 | 建议 | 理由 |
|---|---|---|---|
| fork 策略 | fork 全仓 vs 仅取骨架 | **fork 全仓 + 深度改造** | 保留 UI/desktop/serve 全部资产，改造集中 ResourceLayer |
| 语言 | Go（继承） | **Go** | kernel 已 Go，单二进制，零成本集成 |
| 调度粒度 | 工具级 vs 会话级 | **先工具级，后会话级** | 工具级可立即验证（并发/挂起），会话级等 H3 |
| UI 技术 | TUI（继承 bubbletea?）+ Wails | **继承现骨架重画** | 改 UI 壳不动交互逻辑 |

**风险**：
- fork 维护负担（Reasonix 上游更新）→ 评估后可能停止跟随上游（本项目自研方向已定）
- 重构范围膨胀 → 每阶段独立交付 + 验收标准锁死
- UI 重构周期长 → H4 与 H2/H3 并行，避免阻塞核心

## 7. 与 M0-Gate 的关系

M0-Gate 已通过（有条件继续）：本蓝图即「M1 核心路线」的执行方案——**H1-H3 完成即达成 M0-Gate 的完整愿景（kernel 调配 harness）**，H4 达成 Agile 1 的"首个可下载 agent"（品牌化产品）。

## 8. 立即行动项（下一步）

1. **H1 启动**：fork Reasonix → `harness/` 目录入 semantix 仓库（submodule 或 vendor）→ U7/U8 挂载
2. 与 Git 集成部署并行：site 自动上线已配置（.github/workflows/deploy-site.yml）
3. H1 期间同步产出 UI 设计稿（Semantix Design tokens）
