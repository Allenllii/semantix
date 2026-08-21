# Spec：Harness 合体 + 资源编排契约（Agile 2 · U37）

> 对应 Issue：U37（Agile 2 批次，见 `docs/Agile路线图.md` §2）
> 真源约束：`kernel/sched/sched.go`（`RoundInput` / `ToolCallInfo` / `RoundPlan` / `Decider`，接口冻结于 U1）、
> `kernel/event/event.go`（12 种 Kind + `KindCount` 哨兵）、`patches/semantix-sched-prefetch.patch`（U13c 成果，本期作迁移素材）；
> 架构基线：`docs/reports/harness-refactor-blueprint.md` §3/§4/§6、`docs/Agent-Infra-架构设计.md` §5。
>
> **状态（2026-08-21）**：v1.2-draft（#269 tier intent/近邻契约）；v1.1 的 U37 评审记录见 §10。
> **先审后写**——本文档获批准前，U38-U43 不开工实现。
> 判级：Spec-Required（新顶层目录/包结构 + 事件契约扩展 + 新配置面，多条命中）。

## 0. 战略决策（2026-08-17，Song）

**停止在 DeepSeek-Reasonix fork 仓库上改动。** Reasonix 的 agent 系统（主循环/工具/TUI 骨架）
直接**复用进 semantix 仓**（MIT 允许，保留 attribution），在 semantix 开长期集成分支，
换上我们自己的视觉（Semantix Design，蓝图 §4），与 kernel **进程内**结合。

这是蓝图 §6 风险项的正式落地（「fork 维护负担 → 评估后可能停止跟随上游，本项目自研方向已定」）。
后果：

- `patches/` 模式废弃（保留作历史与迁移素材，U13c 的接线成果按 §5 迁移）
- 「跨仓库 wire 契约」降级为「单仓进程内接口」——控制通道不再需要 CLI 子进程与 3s 超时软降级
- H4 视觉从「可并行的 UI 换肤」升级为集成分支的组成部分（#157 成果迁移、#158 重定位到本仓）
- **集成分支**：`harness-integration`（从 main 切出；U38-U43 的实现 PR 全部以它为 base）。
  **回合策略（R11）**：每个 U 的 PR 合入 `harness-integration` 后，即把该 U 增量回合 main
  （小步合入，不攒大 PR）；U38（vendor）作为首个大 PR 单独回合评审；不做「阶段末一次性整体回合」
  （避免大 diff 评审与长期漂移）。

## 1. 背景与缺口

Agile 2 验收三条（路线图 §2）：① 工具可挂起/恢复 + 预算配额生效（H2）；② 调度演示：kernel 决策改变
harness 行为 + 可量化收益（H3）；③ 命中率/成本随使用提升曲线（H5）。

现状与差距（U13c 已在 fork 验证过的能力按 §5 迁入本仓）：

| 能力 | 现状 | 缺口 |
|---|---|---|
| harness 代码 | 在 fork 仓（另一个 module） | 未进本仓：无 `harness/` 目录 |
| 观测上行 | ✅ fork 侧 sink 镜像 12 种事件（U13c） | 迁移后改为进程内直发总线；资源目录缺失 |
| 调度决策 | ⚠️ fork 侧 `internal/semantix/sched.go` 是 RuleDecider 的本地移植 | 迁移后直接 import `kernel/sched`，删除移植副本 |
| 工具挂起/恢复 | ❌ | `RoundPlan` 无挂起语义；无执行点 |
| 预算配额 | ❌ | 配置键存在，无 Controller 强制执行 |
| prefetch/evolve 闭环 | ⚠️ kernel 包已实现（U18/U18b） | `PrefetchHit`/`PrefetchWaste`/`EvolutionTick` 无人发射，参数不生效 |
| 视觉 | fork TUI 原样 + U33 复用面板（#168 已并入 fork） | Semantix Design（深色 + 语义绿 #2F967F）未落地 |

## 2. 范围

**范围内**：

- C0 Reasonix agent 系统 vendor 进本仓（目录/模块/构建/attribution）
- C1 资源目录（进程内构造 + 新增 `ResourceCatalog` 事件 Kind 用于落盘观测）
- C2 调度接线（harness 直接调用 `sched.Decider` + `RoundPlan` 字段扩展）
- C3 预算配额模型（BudgetController 阶梯降级）
- C4 反馈闭环最小集（PrefetchHit/Waste + EvolutionTick 发射点）
- C5 视觉基线迁移（U33 复用面板 + Semantix Design 主题 token）

**不在范围内**：桌面端 Wails 重画（#158，等 C0 落地后在本仓续作）；serve/JSON-RPC 对外协议
（Agile 3）；会话级调度（蓝图 §6：先工具级）；`Decider` 接口签名变更（冻结，只加 `RoundPlan` 字段）；
上游 Reasonix 的后续同步（正式脱钩）。

## 3. C0 Vendor 方案（新顶层目录）

```
semantix/
├── kernel/          # 既有，不动
├── gateway/         # 既有，不动
├── harness/         # 新增：Reasonix agent 系统落点（单 Go module 合并进本仓 module）
│   ├── agent/       # 主循环（run_loop / execute_batch / turnruntime / sampling_request）
│   ├── tool/        # 内置工具注册表
│   ├── control/     # 单 Controller 多前端骨架（蓝图 §2.2「最大资产」）
│   ├── tui/         # chatREPL/runAgent（bubbletea）→ Semantix Design 重画面
│   ├── provider/    # DeepSeek 前缀缓存 provider
│   └── ATTRIBUTION.md  # 上游 MIT 声明 + 脱钩 commit 记录
└── cmd/semantix-agent/  # 新增：合体后的可执行入口（TUI 主力形态）
```

- **模块路径**：全部改写为 `semantix/harness/...`（进本仓 module，不留 `reasonix` module）；
- **裁剪原则**：只搬 agent 系统运行必需（memory/skills/权限门控/事件流按需保留），
  desktop/、ACP、serve 等前端本期不搬（#158 时再取）；搬运清单在 U38 实现 PR 中逐目录列出；
  **判定标准（R1）**：agent 主循环直接 import 的包必搬，仅测试/工具链引用的不搬；
  最小判据 = `cmd/semantix-agent` 冒烟可跑；搬运返工 ≥3 次即触发「先最小集再回补」策略；
- **fork 里的 semantix 桥**（`internal/semantix/`）**不搬**——它是跨进程时代的产物，
  其职责由 §4 的进程内接线替代；U13c patch 中 agent 文件的改动点作为迁移对照表。

## 4. C1/C2 编排契约（进程内）

### 4.1 C1 资源目录

harness 启动时与目录变化时（工具注册/挂起变化）构造全量目录，直发事件总线（幂等，以最新为准）。
新增 event Kind（追加在 `EvolutionTick` 之后、`KindCount` 之前——`ingest.JSONLSource` 按 int
反序列化，追加式演进不破坏旧会话文件）：

```go
// ResourceCatalog reports the harness resource inventory (tools/models/budget).
ResourceCatalog

type ResourceCatalogPayload struct {
    Tools  []ResourceTool  `json:"tools"`  // name, readOnly, suspended
    Models []ResourceModel `json:"models"` // id, tier ("flash"|"pro"), inputPrice, outputPrice
    Budget ResourceBudget  `json:"budget"` // limitUSD, spentUSD, window ("session"|"day")
}
```

进程内已可直接读结构体，仍保留事件的原因：**落盘可观测**（会话 JSONL 回放）+ evolve 的信号源
+ 未来多 harness（Agile 3）时该 Kind 即外部上报契约。

### 4.2 C2 调度接线

- harness 每个工具轮直接调用 `sched.Decider.DecideRound`（默认注入 `RuleDecider`）；
  错误时降级为静态 `ReadOnly()` 分组（fail-open 三铁律），不再有子进程与超时层；
- 删除 fork 时代的本地移植副本（迁移后 `kernel/sched` 是唯一实现）；
- `RoundPlan` 字段扩展（契约演进，需批准；Go 加字段向后兼容，JSON 旧消费者忽略未知键）：

```go
type RoundPlan struct {
    ParallelGroups [][]string
    Tier           string
    TierReason     string     `json:"tierReason,omitempty"`
    InjectIDs      []string
    PrefetchIDs    []string
    // 新增：
    SuspendTools   []string `json:"suspendTools,omitempty"`   // 本轮应挂起的工具名（声明式全量）
    MaxParallel    int      `json:"maxParallel,omitempty"`    // 0 = 不覆盖（沿用 harness 默认，可能有限制）；>0 = 强制上限（R4）
    BudgetAction   string   `json:"budgetAction,omitempty"`   // "" | "degrade_tier" | "halt_prefetch" | "hard_stop"；未知值按无动作（fail-safe，R7）
}
```

`TierReason` 是机器可读、稳定的首个命中理由，供事件落盘与调度演示解释决策；它不参与执行。
常规 tier 规则按以下优先级短路：

1. 冻结的 turn intent 为 `mutation` / `persistent_action` → pro（`intent:<name>`）；
2. 本轮存在 writer → pro（`writer:<tool>`）；
3. 工具数超过 `ComplexTools` → pro（`complex:<count>`）；
4. 否则使用默认 tier（`default`）。

预算动作优先级仍高于上述规则；`degrade_tier` 覆盖 tier 时，理由为
`budget:degrade_tier`。Intent 必须来自 `beginRunTurn` 已冻结的 `TaskPolicy`，不得在每个工具轮
重复分类，避免同一 turn 内路由漂移。

历史近邻信号分第二阶段接入。近邻投票的最小证据记录必须同时包含
`session_id`、`tier`、`task_success` 与查询相似度；只有“在 flash 上失败”的近邻才可投升级票，
且近邻合计只占一票。`SliceStats.Hits/Rejected/UserFeedback` 分别描述使用、注入污染和显式反馈，
**不得**替代任务成败或历史 tier。当前持久化模型尚无上述二字段时，调度器必须按冷启动处理并
退回前述确定性规则，不能从切片统计猜测结果。

挂起语义：`SuspendTools` 是**声明式全量**（每轮下发当前应挂起集合），不是增量指令——
harness 不维护指令历史，恢复 = 下一轮不在集合中。

`BudgetAction` 语义（R5）：`degrade_tier` 表示本轮强制 `Tier = "flash"`——run_loop 读取时
**BudgetAction 优先于 `Tier` 字段**（kernel 预算决策覆盖常规 tier 决策）；二者独立下发时以
BudgetAction 为准。未知/非法 `BudgetAction` 值按**无动作**处理（fail-safe，R7），枚举值在
`kernel/sched` 集中以常量定义，禁止魔法字符串散落。

### 4.3 执行点（harness/ 内，对照 U13c patch 迁移）

| 指令 | 执行点 |
|---|---|
| `ParallelGroups`/`MaxParallel` | `harness/agent/execute_batch.go`（U13c 已验证的替换点） |
| `SuspendTools` | `executeBatch` 前过滤：被挂起工具先从本轮 `ParallelGroups` 剔除（不计入并发组，R6），调用立即返回 tool error「suspended by scheduler」 |
| `Tier` | `harness/agent/run_loop.go` 工具轮末（U13c 记录 tier → 本期真正切模型） |
| `BudgetAction` | BudgetController（§5） |
| `InjectIDs`/预热 | `sampling_request.go` 注入块 + `startInjectWarm`（U13c 已验证，进程内化） |

## 5. C3 预算配额（BudgetController）

`harness/agent/budget.go`：从配置读 `limitUSD`，以 `Usage` 事件累计 `spentUSD`。
**阶梯降级**（正确性 > 缓存 > 并发 > 预取，架构 §5.1 优先级反向裁剪）：

| 阈值（触发 / 恢复） | 行为 |
|---|---|
| ≥ 70% / < 65% | `halt_prefetch`：停止预取（赌博先停）；回落到 65% 以下恢复（R8 回滞带防抖动） |
| ≥ 90% / < 85% | `degrade_tier`：强制 flash；回落到 85% 以下恢复原 tier（R8） |
| ≥ 100% | `hard_stop`：拒绝**新**工具轮（在途轮完成收尾、响应照常返回，不中断，R9），向用户显式报错（不静默）；不设自动恢复，需用户处理/重置 |

预算状态随 C1 目录进总线；`RuleDecider` 读到后可经 `BudgetAction` 提前下发
（kernel 决策优先，harness 本地阈值兜底——两层同规则，谁先触发谁生效）。

**双层 authority（R10）**：harness 本地以 `Usage` 事件实时累计的 `spentUSD` 为**权威**；
C1 目录快照异步、可能滞后，kernel 经 `BudgetAction` 下发的是**预判/提前量**——
落地一律以 harness 本地实时值为准，kernel 快照仅供提前决策，不作为复核依据。

## 6. C4 反馈闭环最小集

- `PrefetchHit`/`PrefetchWaste`：发射点在注入预热结果消费处（预热块被本轮请求使用 = Hit，
  被丢弃/过期 = Waste）；payload 既有定义不变；
- `EvolutionTick`：`kernel/evolve` 参数更新时发射；本期参数生效面**只限** RuleDecider 行为门阈值
  与 prefetch 信号源权重（架构 §6.2 在线层），离线重训不在本期。

## 7. C5 视觉基线（Semantix Design 最小集）

- 主题 token：深色基调 + 语义绿 `#2F967F`（蓝图 §4，与 landing page 一致）；
- U33 复用面板（每 turn：📦 命中切片数 / 💰 节省成本 / 🗂 来源会话）从 fork 迁入 `harness/tui/`，
  数据源从 CLI 子进程改为进程内直读；
- 资源仪表（侧栏实时资源占用）**本期只留挂点不实现**（等 C1-C3 数据稳定后单独 issue）。

## 8. 验收（对应 Agile 2 DoD）

- [ ] `go build ./...` + `go test ./... -race` 单仓全绿（harness 并入后）
- [ ] `cmd/semantix-agent` 真实会话跑通：注入/复用面板显示（C0+C5 冒烟）
- [ ] 挂起演示：kernel 下发挂起某工具 → 调用被拒；恢复后可用（H2 门①）
- [ ] 预算演示：压到 70%/90%/100% 三阈值，行为逐级降级且用户可见（H2 门①）
- [ ] 调度演示报告：同一任务 kernel 决策 on/off 对比（延迟/成本/并发度量化），落 `docs/reports/`（H3 门②）
- [ ] PrefetchHit/Waste 与 EvolutionTick 出现在会话 JSONL 且 `semantix search` 可检索（H5 前置）

## 9. 风险

- **vendor 一次性成本**：模块路径改写 + 裁剪面判断失误会拖长 U38 → 对策：先最小可跑
  （agent+tool+control+tui+provider），冒烟绿了再谈裁剪回补；
- **wire 契约演进纪律不因进程内而放松**：事件 Kind 只追加不重排；`RoundPlan` 只加 omitempty 字段
  ——会话 JSONL 是落盘契约，旧文件必须永远可回放；
- **与上游脱钩**：Reasonix 后续修复不再自动获得 → `ATTRIBUTION.md` 记录脱钩 commit，
  重大上游修复按需手工 cherry-pick；
- **集成分支长期漂移**：`harness-integration` 与 main 的偏差随时间增大 → 每完成一个 U 即回合一次
  main（小步合入，不攒大 PR）。


## 10. U37 评审记录（2026-08-17）

> 评审载体：Issue #188（Agile 2 开工门禁）。评审范围：C0 裁剪原则（§3）、`RoundPlan` 新字段
> 语义（§4.2）、预算三阈值（§5）、`harness-integration` 分支策略（§0/§9）。
> 处置口径：R# 已回写正文的，正文标注同号；「待办」项由对应 U 实现 PR 落实。

| # | 评审项 | 发现 | 处置 |
|---|---|---|---|
| R1 | C0 裁剪 | 「按需保留」缺判定标准，裁剪面判断失误会拖长 U38 | 已修订 §3：判定标准 = 主循环直接 import 必搬；冒烟可跑为最小判据；返工 ≥3 次触发「先最小集再回补」 |
| R2 | C0 裁剪 | ATTRIBUTION 需列明 vendored commit 与逐目录来源 | 待办（U38）：列出上游 main-v2 快照 sha + 逐目录来源 + 脱钩 commit |
| R3 | C0 裁剪 | fork 桥不搬判定成立；U13c 迁移对照表需含每点迁移结果 | 待办（U38）：对照表两栏（改动点 + 迁移结果） |
| R4 | RoundPlan | `MaxParallel=0` 语义二义（「不限」vs「沿用配置」） | 已修订 §4.2：0 = 不覆盖（沿用 harness 默认）；>0 = 强制上限 |
| R5 | RoundPlan | `BudgetAction=degrade_tier` 与 `Tier` 字段交互未定义 | 已修订 §4.2：BudgetAction 优先于 Tier，degrade_tier 强制本轮 flash |
| R6 | RoundPlan | `SuspendTools` 与 `ParallelGroups` 并发组计数交互未定义 | 已修订 §4.3：被挂起工具先从分组剔除（不计并发度） |
| R7 | RoundPlan | `BudgetAction` 未知值无降级路径 | 已修订 §4.2：未知值按无动作（fail-safe）；枚举集中常量定义 |
| R8 | 预算 | 70/90/100 无回滞，阈值边界抖动 | 已修订 §5：恢复阈值 65/85，hard_stop 不自动恢复 |
| R9 | 预算 | `hard_stop` 对在途工具轮的处理未定义 | 已修订 §5：在途轮完成收尾不中断，仅拒绝新轮 |
| R10 | 预算 | 双层触发 authority 未定（kernel 快照可能滞后） | 已修订 §5：harness 本地实时 spentUSD 为权威，kernel 下发为预判 |
| R11 | 分支 | 「阶段末整体回合」与「每 U 回合」表述冲突 | 已修订 §0：每 U 合入即回合 main（小步）；U38 大 PR 单独评审；取消阶段末一次性回合 |
| R12 | 分支 | 长期集成分支需 CI 保护 | 待办：`harness-integration` 设全量 CI（build + race）分支保护，回合 main 前必须绿 |

**结论**：四项评审范围（C0 / RoundPlan / 预算 / 分支）未发现阻断性设计缺陷；R1-R11 为语义
澄清与边界补全（已回写正文），R2/R3/R12 为 U38 实现 PR 待办。**本 spec 评审通过（v1.1），
U38-U43 可解锁开工**——最终批准与 issue 关闭由 Song 执行（#188 闭环）。
