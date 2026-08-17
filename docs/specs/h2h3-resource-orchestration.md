# Spec：H2/H3 资源编排契约（Agile 2 · U37）

> 对应 Issue：U37（Agile 2 批次，见 `docs/Agile路线图.md` §2）
> 真源约束：`kernel/sched/sched.go`（`RoundInput` / `ToolCallInfo` / `RoundPlan` / `Decider`，接口已冻结于 U1）、
> `kernel/event/event.go`（12 种 Kind + `KindCount` 哨兵）、`patches/semantix-sched-prefetch.patch`（U13c 已接线现状）；
> 架构基线：`docs/reports/harness-refactor-blueprint.md` §3（资源目录 / 控制通道 / 闭环反馈）、
> `docs/Agent-Infra-架构设计.md` §5（调度决策输出）。
>
> **状态（2026-08-17）**：v1 草案，**先审后写**——本文档获批准前，U38-U43 不开工实现。
> 判级：Spec-Required（wire 契约扩展 + 跨仓库改动，同时命中两条）。

## 1. 背景与缺口

Agile 2 验收三条（路线图 §2）：① 工具可挂起/恢复 + 预算配额生效（H2）；② 调度演示：kernel 决策改变
harness 行为 + 可量化收益（H3）；③ 命中率/成本随使用提升曲线（H5）。

U13c（#123）之后的真实现状，与蓝图的差距：

| 能力 | 现状 | 缺口 |
|---|---|---|
| 观测上行 | ✅ 事件旁路（`internal/semantix/sink.go` 镜像 12 种事件） | 无资源目录——kernel 看不到 harness 有哪些工具/模型/预算 |
| 调度决策 | ⚠️ fork 侧 `internal/semantix/sched.go` 是 `RuleDecider` 的**本地移植**，并行分组/tier 已生效 | 决策不经 kernel——不构成「kernel 调配 harness」，规则升级要两仓同步 |
| 工具挂起/恢复 | ❌ | `RoundPlan` 无挂起语义；fork 侧无执行点 |
| 预算配额 | ❌ | 配置有 `[semantix] budget` 键，无 Controller 强制执行 |
| prefetch/evolve 闭环 | ⚠️ kernel 包已实现（U18/U18b） | `PrefetchHit`/`PrefetchWaste`/`EvolutionTick` 无人发射，参数更新不生效 |

## 2. 范围

**范围内**（本 spec 定义的契约）：

- C1 资源目录上报（上行，新增 event Kind）
- C2 调度指令通道（下行，`semantix sched decide --json` CLI 面 + `RoundPlan` 字段扩展）
- C3 预算配额模型（fork 侧 BudgetController 行为语义）
- C4 反馈闭环最小集（PrefetchHit/Waste + EvolutionTick 的发射点）

**不在范围内**：serve/unix-socket 传输面（U27 已落地，作为 Agile 3 升级路径，本期一律 CLI 子进程）；
会话级调度（蓝图 §6 決策：先工具级）；`kernel/sched.Decider` 接口签名变更（冻结，只加 `RoundPlan` 字段）；
H4 UI（#158 并行）。

## 3. C1 资源目录上报（上行）

新增 event Kind（追加在 `EvolutionTick` 之后、`KindCount` 之前，保持既有序号不变——
`ingest.JSONLSource` 按 int 反序列化，追加式演进不破坏旧会话文件）：

```go
// ResourceCatalog reports the harness resource inventory (tools/models/budget).
ResourceCatalog

type ResourceCatalogPayload struct {
    Tools  []ResourceTool  `json:"tools"`  // name, readOnly, suspended
    Models []ResourceModel `json:"models"` // id, tier ("flash"|"pro"), inputPrice, outputPrice
    Budget ResourceBudget  `json:"budget"` // limitUSD, spentUSD, window ("session"|"day")
}
```

发射时机：harness 启动后一次全量 + 目录变化时（工具注册/挂起状态变化）增量重发全量（幂等，
kernel 以最新一条为准）。fork 侧落点：`internal/semantix/bridge.go` 聚合，boot 时发射。

## 4. C2 调度指令通道（下行）

### 4.1 传输面

沿用 U13c 的既有模式（CLI 子进程 + JSON + 超时软降级），新增子命令：

```
echo '<RoundInput JSON>' | semantix sched decide --json
→ stdout: {"ok":true,"data":<RoundPlan JSON>}   （--json 统一信封，U22 契约）
```

fork 侧 `internal/semantix/sched.go` 改为：**先问 kernel（3s 超时），失败/超时软降级回本地移植版**
（fail-open 三铁律，与 inject/lookup 一致）。本地移植版从「权威实现」降级为「降级兜底」，
消除两仓规则漂移。

### 4.2 `RoundPlan` 字段扩展（契约演进，需批准）

```go
type RoundPlan struct {
    ParallelGroups [][]string
    Tier           string
    InjectIDs      []string
    PrefetchIDs    []string
    // 新增（Go 加字段向后兼容，JSON 旧消费者忽略未知键）：
    SuspendTools   []string `json:"suspendTools,omitempty"`   // 本轮起挂起的工具名；恢复 = 不再出现
    MaxParallel    int      `json:"maxParallel,omitempty"`    // 0 = 不限（沿用 harness 配置）
    BudgetAction   string   `json:"budgetAction,omitempty"`   // "" | "degrade_tier" | "halt_prefetch" | "hard_stop"
}
```

挂起语义：`SuspendTools` 是**声明式全量**（每轮下发当前应挂起集合），不是增量指令——
harness 无需维护指令历史，重启后由下一轮决策自然恢复。

### 4.3 fork 侧执行点

| 指令 | 执行点（Reasonix fork） |
|---|---|
| `ParallelGroups`/`MaxParallel` | `internal/agent/execute_batch.go`（U13c 已有，扩 MaxParallel 上限） |
| `SuspendTools` | `executeBatch` 前过滤：被挂起工具的调用立即返回 tool error「suspended by scheduler」 |
| `Tier` | `internal/agent/run_loop.go` `handleToolRound`（U13c 已记录，本期真正切模型） |
| `BudgetAction` | BudgetController（§5） |

## 5. C3 预算配额（BudgetController）

fork 侧新增 `internal/semantix/budget.go`：从 `[semantix] budget` 读 `limitUSD`，
以 `Usage` 事件累计 `spentUSD`。**阶梯降级**（正确性 > 缓存 > 并发 > 预取，架构 §5.1 优先级的反向裁剪）：

| 阈值 | 行为 |
|---|---|
| ≥ 70% | `halt_prefetch`：停止预取（赌博先停） |
| ≥ 90% | `degrade_tier`：强制 flash |
| ≥ 100% | `hard_stop`：拒绝新工具轮，向用户显式报错（不静默） |

预算状态随 C1 目录上报回流 kernel；kernel `RuleDecider` 读到后可在 `BudgetAction` 提前下发
（kernel 决策优先，harness 本地阈值为兜底——两层同规则，谁先触发谁生效）。

## 6. C4 反馈闭环最小集

- `PrefetchHit`/`PrefetchWaste`：发射点在 fork 侧 `startInjectWarm` 结果消费处
  （预热的注入块被本轮请求使用 = Hit，被丢弃/过期 = Waste）；payload 既有定义不变。
- `EvolutionTick`：`kernel/evolve` 在参数更新时发射（发射代码在 kernel 侧，随 ingest 落库）；
  本期参数生效面**只限** `RuleDecider` 的行为门阈值与 prefetch 信号源权重（架构 §6.2 在线层），
  离线重训不在本期。

## 7. 验收（对应 Agile 2 DoD）

- [ ] `semantix sched decide --json`：golden 用例（输入 RoundInput → 输出含 SuspendTools/BudgetAction 的 RoundPlan）
- [ ] fork 真实会话：kernel 下发挂起某工具 → 该工具调用被拒；恢复后可用（H2 门①）
- [ ] 预算演示：压到 70%/90%/100% 三阈值，行为逐级降级且用户可见（H2 门①）
- [ ] 调度演示报告：同一任务 kernel 决策 on/off 对比（延迟/成本/并发度量化），落 `docs/reports/`（H3 门②）
- [ ] PrefetchHit/Waste 与 EvolutionTick 在真实会话 JSONL 中出现且 `semantix search` 可检索（H5 前置）
- [ ] 两仓测试全绿：semantix `go test ./... -race`；fork `go build ./...` + patch 套用验证

## 8. 风险

- **wire 契约一旦发出即绑死下游**：Kind 只追加不重排；RoundPlan 只加 omitempty 字段；`--json` 信封不动。
- **fork patch 冲突面**：改动集中 `internal/semantix/` + 4 个 agent 文件（U13c 同一批文件），
  patch 更新走 `patches/` 惯例（U13c 先例）。
- **子进程延迟**：每工具轮一次 decide 调用（3s 超时兜底）；若实测延迟不可接受，
  升级路径是 serve 常驻（U27 已落地），契约不变只换传输。
