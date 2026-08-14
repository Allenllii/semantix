# Spec：投机预取 Planner MVP（Issue 62 / M1-U18）

> 对应 Issue：`#62 M1-U18: 投机预取 Planner MVP（kernel/prefetch，P2 暂缓可选）`
> 真源约束：接口冻结于 `kernel/prefetch/prefetch.go`（`PrefetchTask` / `Prefetcher`），本 PR **不改接口签名**；
> 架构基线：`docs/Agent-Infra-架构设计.md` §5.2（T-Slice 转移概率、只读预取、预算控制）；
> 事件契约：`kernel/event/event.go` 已定义 `PrefetchHit` / `PrefetchWaste`（本 PR 不发事件，见 §1 不在范围）。
>
> **状态（2026-08-13）**：本文档为 Issue 62 实现规格，先审后写。

## 1. 目标与范围

**核心目标**：实现 `kernel/prefetch/` 的 MVP——基于切片库 ToolPattern 切片学习
T-Slice 转移矩阵，在 LLM 流式等待期规划**只读**预取任务，并执行后将结果以
Result 切片入库。

**范围内**：

- `planner.go`：转移矩阵学习（工具名序列 → 下一工具概率，2-gram 计数）
- 预算控制：只读工具白名单 + 每轮 token 预算截断 + 任务数上限
- `runner.go`：执行只读预取任务 → 结果入库（Result 切片）
- 默认 `Executor`（slice-assembly）：按预测工具检索切片库并组装内容
- 测试：转移矩阵学习 / 预算截断 / 白名单拒绝写操作（fail-closed）
- spec 文档：本文档

**不在范围内**（后续单元，本 PR 不实现）：

- `PrefetchHit` / `PrefetchWaste` 事件发射与总线接线（事件类型已冻结，发射在
  evolve 闭环 / harness 挂载点接入时补，见 `docs/events.md` §3）
- evolve 自惩罚（waste/hit 比例降权信号源）——属 `kernel/evolve` 职责
- `embedding` 类任务执行（当前无 embedder，`PrefetchTask.Kind` 保留但不生成）
- `file` 类任务（低优先级；转移矩阵只有工具名、无参数，MVP 不猜测文件路径，
  见 §2.4 数据模型限制）
- `sched.Decider` 的 `RoundPlan.PrefetchIDs` 接线（联合决策属 M3 调度器）
- `kernel/prefetch/prefetch.go` 接口签名变更（冻结）

## 2. 设计

### 2.1 包结构（全部新增，不改既有文件）

```
kernel/prefetch/
├── prefetch.go   # 既有冻结接口，不改
├── planner.go    # 新增：Planner（转移矩阵 + 白名单 + 预算）实现 Prefetcher
├── runner.go     # 新增：Runner（执行任务 → Result 切片入库）+ Executor 接口
└── *_test.go     # 新增：planner_test.go / runner_test.go
```

### 2.2 转移矩阵（planner.go）

数据源：`slice.Store.List(Project)` 中 `Type == slice.ToolPattern` 的切片。
ToolPattern 切片的 `Content` 是**空格分隔的工具名序列**（见
`kernel/slice/extractor.go` 的 `toolPatternSlice`，如 `"grep readFile editFile test"`）。

- 2-gram 计数：对每条序列中相邻两工具 `A → B`，`count[A][B]++`、`total[A]++`；
- 转移概率：`P(B|A) = count[A][B] / total[A]`（未出现过的转移记 0）；
- 矩阵在 `Learn()` 时构建并缓存（并发读安全，`sync.RWMutex`）；
- 确定性：同一切片库 → 同一矩阵（map 遍历只用于构建，查询按概率降序、
  平局按工具名字典序，输出顺序字节稳定）。

### 2.3 Planner（实现 `Prefetcher` 接口）

```go
type Planner struct {
    Store     slice.Store   // ToolPattern 切片数据源（必填）
    WhiteList []string      // 只读工具白名单；空 = 空计划（fail-closed）
    Budget    int           // 每轮预算（token）；<=0 用 DefaultBudget
    MaxTasks  int           // 每轮任务数上限；<=0 用 DefaultMaxTasks
    // 内部：matrix + RWMutex，Learn() 填充
}

func (p *Planner) Learn() error
func (p *Planner) Plan(lastToolNames []string) ([]PrefetchTask, error)
```

`Plan` 决策链（严格顺序，保证 fail-closed 优先于预算优先于截断）：

1. 矩阵未学习 → 返回空计划（不报错，不猜测）；
2. `lastToolNames` 为空或全为空串 → 返回空计划；
3. 取最近一个工具名 `A`，按 `P(·|A)` 降序取候选（平局按工具名字典序）；
4. **白名单过滤（fail-closed）**：候选目标 `B ∉ WhiteList` 一律丢弃——
   非只读工具**永不**进入预取计划；`WhiteList` 为空时计划恒为空；
5. 预算截断：任务按概率降序逐个加入，累计 `Cost` 超过 `Budget` 即停止；
6. 任务数截断：不超过 `MaxTasks`。

生成的 `PrefetchTask`：

- `Kind = "slice-assembly"`（本 MVP 唯一生成种类）；
- `Key = 预测的下一工具名 B`（执行器据此检索候选切片，见 §2.5）；
- `Cost`：估算 token 成本（每任务 `DefaultTaskCost` 常量），用于预算累计。

**默认常量**：`DefaultBudget = 1000`（token/轮）、`DefaultMaxTasks = 3`、
`DefaultTaskCost = 200`（token/任务）。

### 2.4 数据模型限制（诚实声明）

转移矩阵只含**工具名**，不含参数。因此无法从序列得知具体文件路径或切片 ID：
`file` 类任务（需要路径）在本 MVP 不生成，避免无依据的猜测；
`slice-assembly` 以"预测工具名"为检索键从切片库取候选，是唯一有依据的资源来源。

### 2.5 Runner 与 Executor（runner.go）

```go
// Executor 执行单个只读预取任务，返回将被存为 Result 切片的内容。
type Executor interface {
    Execute(ctx context.Context, t PrefetchTask) ([]byte, error)
}

// Runner 串行执行任务列表，成功结果以 Result 切片写入 Store。
type Runner struct {
    Store    slice.Store
    Executor Executor
    Scope    slice.Scope // 入库切片的作用域（默认 slice.Project）
}

func (r *Runner) Run(ctx context.Context, tasks []PrefetchTask) ([]string, error)
```

- 串行执行（MVP 保守；预取是后台低优先，不抢占真实工具调用）；每任务的
  **时长上限由调用方传入的 `ctx` 控制**（如等待期剩余时间）；
- 成功结果构造为 `slice.Slice{Type: slice.Result, Scope: r.Scope, Content: content,
  Meta: {SourceSession: "prefetch:" + t.Key}}` 后 `Store.Put`；切片 ID 由内容哈希
  决定（复用 `kernel/slice` 的确定性 ID 约定，同内容不重复入库）；
- `Run` 返回成功入库的切片 ID 列表；单个任务失败不影响其余任务
  （记录并继续，返回汇总错误）。

默认执行器 `SliceAssembler`（本 PR 提供）：

```go
type SliceAssembler struct {
    Index slice.Index // 候选切片检索（必填）
    K     int         // 每任务检索切片数；<=0 用 3
}
// Execute(t)：Index.Search(t.Key, K, Project) → 命中按 ID 字典序组装
// "--- slice <id> ---\n<content>" → 返回。
```

### 2.6 并发安全

`Planner` 的矩阵缓存读写走 `RWMutex`：`Learn` 持写锁重建，`Plan` 持读锁查询，
可被调度器并发调用（对齐 `sched` 并发契约）。`Runner` 无共享可变状态（`Store`
自身线程安全——`slice/fileStore` 已有互斥）。

## 3. 测试计划（与 issue 验收一一对应）

| 测试 | 断言 |
|---|---|
| `TestLearnTransitionMatrix` | 演示切片库（确定性构造）→ `Learn()` → `P(readFile\|grep)` 最高、`P(editFile\|readFile)` 精确值正确；重复 `Learn` 幂等 |
| `TestPlanPredictsTopK` | `Plan(["grep"])` → 返回概率降序、白名单内的任务；`Key`/`Kind` 正确 |
| `TestPlanBudgetTruncation` | `Budget` 很小 → 返回任务 Cost 总和 ≤ Budget，且高概率者优先保留 |
| `TestPlanWhiteListFailClosed` | 候选含 `writeFile`/`editFile`（非白名单）→ 永不进入计划；`WhiteList` 为空 → 计划恒空 |
| `TestPlanEmptyOrUnlearned` | 未 `Learn()` / 空 `lastToolNames` → 空计划不报错 |
| `TestRunnerStoresResultSlice` | 执行 slice-assembly 任务 → Store 出现 `Type==Result` 切片，内容含源切片文本 |
| `TestPlanConcurrent` | 并发 `Plan`（`-race`）无数据竞争 |

## 4. 验收标准（对应 issue 原文）

- [ ] `go vet ./...` 全绿
- [ ] `go test -race ./...` 全绿（含新增 `kernel/prefetch` 测试）
- [ ] 转移矩阵从演示切片库可学习（确定性测试，见 §3 第一行）
- [ ] 非只读工具永不进入预取计划（fail-closed，见 §3 白名单测试）

## 5. 风险与边界

| 风险/边界 | 等级 | 说明 |
|---|---|---|
| 转移矩阵只含工具名、无参数 | 已知限制 | `file` 类任务不生成；`slice-assembly` 以工具名为检索键，命中率待真实数据验证（M3） |
| 预取命中率未知 | 中 | 本 PR 只交付"规划 + 执行入库"能力；`PrefetchHit/Waste` 观测接线在后续 evolve 闭环，届时用真实数据调参 |
| 预算为 token 估算 | 低 | `Cost` 是常量估算，非真实计费；MVP 阶段用于截断语义验证 |
| 执行副作用 | 无（设计保证） | 只有白名单工具可被预测、只有 `slice-assembly` 执行、执行只读 Store/Index 操作 |
