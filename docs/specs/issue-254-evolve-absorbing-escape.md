# Spec：evolve 闭环吸收态逃逸（ε-探索）+ TauL2/SuccessFloor 参数拆分（Issue 254）

> 对应 Issue：`#254 evolve 闭环吸收态：conf 关停预取后信号流消失、参数永久冻结`
> 真源约束：`Prefetcher` 接口冻结于 `kernel/prefetch/prefetch.go`（不改签名）；`evolve.Engine`
> 接口冻结于 `kernel/evolve/evolve.go`（不改方法集）。
> 架构基线：`docs/Agent-Infra-架构设计.md` §5.2（T-Slice 转移概率、只读预取、预算控制）与
> `docs/specs/issue-62-prefetch-planner.md`（预取计划器 MVP）。
> 证据来源：`docs/reports/agile2-evolution-curve.md` 实验 2（ON/OFF 因果对照，churn 场景）。
>
> **状态（2026-08-21）**：本文档为 Issue 254 实现规格，先审后写。

## 1. 目标与范围

**核心目标**：解决 evolve 闭环的**吸收态死锁**——当 `PrefetchConf` 被抬升越过全部候选转移
概率后，`Plan` 返回空 → 不再产生 `PrefetchHit/Waste` 事件 → 引擎再无信号 → `PrefetchConf`
永久冻结在关停位，churn 结束后无法自动恢复预取。同时**拆分**被错误复用的单一旋钮
`TauL2 → SuccessFloor`，让两个量纲不同的阈值各归其位。

**范围内**：

- `kernel/prefetch/matrix.go`：ε-探索——在「存在候选但全部低于 `MinConf`」时，以概率 `ε`
  放行最高转移概率的候选作为探针，维持最小 hit/waste 信号流。
- `kernel/evolve/evolve.go`（`Params`）：新增独立 `SuccessFloor` 字段并与 `TauL2` 解耦；
  新增 ε 相关常量/接线。
- `harness/semantix/evolution.go`：把 `SuccessFloor` 单独喂给 `sched.RuleDecider`，不再复用
  `TauL2`；把 ε 传给 `prefetch.MatrixPrefetcher`。
- 回归测试：分别覆盖「吸收态逃逸」与「参数拆分接线」。
- spec 文档：本文档。

**不在范围内**（后续单元，本 PR 不实现）：

- 文献调研 §9 建议 5 的**conf 时间衰减**与**旁路信号**两种逃逸机制（本轮仅选 ε-探索；
  其余作为后续候选，见 §5）。
- `demoted()` 对纯 waste 流的免疫缺陷（`WasteHitLimit` 门槛下的历史优等生不降级）——
  属另一 issue，不随本 PR。
- `PrefetchHit/Waste` 事件契约变更（事件类型已冻结，不改）。
- `kernel/prefetch/prefetch.go` 接口签名变更（冻结）。

## 2. 背景与根因

### 2.1 闭环链路（现状）

```
PrefetchConf ─(ApplyEvolution)→ MatrixPrefetcher.cfg.MinConf
     └──(min)──┐
               ▼
Plan(last) ──过滤 prob < MinConf──→ 候选列表 → PrefetchTask[]（可能为空）
               │
               ▼
     （有任务才） PrefetchHit / PrefetchWaste 事件
               │
               ▼
EvolutionLoop.observe ──(仅这两事件)──→ evolve.RecordSignal(prefetch_hit|prefetch_waste)
               │
               ▼
prefetchHitEWMA / prefetchWasteEWMA 更新 → maybeAdjustLocked ─→ 抬降 PrefetchConf
```

### 2.2 吸收态机制

1. `PrefetchConf` 被抬升（churn 期纯 waste 流触发 `PrefetchConf += PrefetchStep`），越过
   所有真实转移概率。
2. `Plan` 对每个候选 `prob < MinConf` 直接 `continue`（`matrix.go`），候选集为空 → 返回
   空任务。
3. 空计划 → 无实际预取 → 无 `PrefetchHit/Waste` 事件。
4. `EvolutionLoop.observe` **只**由这两类事件驱动 `RecordSignal`（`evolution.go`），事件
   停发 → 引擎的 `prefetchHitEWMA`/`prefetchWasteEWMA` **不再更新**（EWMA 只在有新信号时
   折叠）。
5. `maybeAdjustLocked` 对 `PrefetchConf` 的判断依赖这两个 EWMA 的大小关系；二者静止 →
   判断分支不触发 → `PrefetchConf` **永久冻结**。

**实证**（`agile2-evolution-curve.md` 实验 2，ON 组数据表）：第 21-25 会话 `conf` 恒为
`0.550`，且 `hit=waste=0`——信号消失、参数冻结，churn 结束后预取未自动恢复。

### 2.3 语义耦合（PR #228 评审点 4）

`TauL2` 的语义是「注入 slice 的高置信**相对置信阈值**」（驱动 `zone.TauLow`，量纲≈相似度/置信）。
`SuccessFloor` 的语义是「只读工具成功率的**行为门**」（量纲=成功率 0-1）。
现状 `evolution.go:99` 直接 `scheduler.ApplyEvolution(after.TauL2)`——同一数值被用来同时
控制两个不同量纲的门，二者强耦合，互相污染调参语义。

## 3. 设计

### 3.1 ε-探索（吸收态逃逸）

**目标**：保证「有候选但被 `MinConf` 全部过滤」时，只要 `ε > 0`，预取流**不会长期归零**，
从而让 hit/waste 信号回流、`PrefetchConf` 有机会下行。

**实现**（`kernel/prefetch/matrix.go`）：

- `Config` 新增 `Epsilon float64`（默认 `0`，**向后兼容**：默认关闭，现有行为不变；由
  evolve 闭环接线时显式开启）。
- `Plan` 中，当「正常候选集为空 **且** 存在因 `prob < MinConf` 被过滤的候选」时：
  - 记录被过滤候选中 `prob` 最高者作为 `probe`；
  - 以概率 `ε`（`rand.Float64() < ε`）放行 `probe`，纳入返回任务。
- 探针任务照常计入 `Cost` 与 `TopK` 预算（与普通候选等权），不特判，避免破坏预算语义。
- **边界**：完全无转移知识（`total==0 || len(transitions)==0`）返回空，**不做** ε 探测——
  ε 探针仅覆盖「有知识但被阈值过滤」的吸收态，不引入无意义探测。
- **随机源**：注入 `*rand.Rand`（可选）或 `math/rand`，供测试用固定种子保证确定性。

**ε 默认与语义**：`Epsilon` 是 prefetch 的 `Config` 字段（默认 `0`，关闭）。harness 接线层在
`harness/semantix/evolution.go` 定义 `EscapeEpsilon = 0.1` 并通过类型断言 `interface{ SetEpsilon }`
启用（见 §3.3）。`Params` 不直接暴露 ε——ε 是链路级开关，不是每个会话快照要暴露的目标参数。

### 3.2 TauL2 / SuccessFloor 拆分

- `kernel/evolve/evolve.go`：
  - `Params` 新增 `SuccessFloor float64 \`json:"success_floor"\``；
  - 初始默认 `DefaultSuccessFloor = 0.7`（与 `sched.RuleDecider` 现有默认一致）；
  - `New` 支持 `Config.SuccessFloor` 覆盖。
- `harness/semantix/evolution.go`：
  - `l.scheduler.ApplyEvolution(after.TauL2)` → `l.scheduler.ApplyEvolution(after.SuccessFloor)`；
  - 不再用 `TauL2` 驱动行为门。
- `TauL2` 继续只驱动 `zone.TauLow`（注入置信），语义不再越界。

### 3.3 接线（evolution.go）

`EvolutionLoop.observe` 在参数变化后把两个自由度分别下发：

```go
if l.scheduler != nil {
    _ = l.scheduler.ApplyEvolution(after.SuccessFloor) // 行为门
}
if l.prefetcher != nil {
    _ = l.prefetcher.ApplyEvolution(after.PrefetchConf)
}
if eps, ok := l.prefetcher.(interface{ SetEpsilon(float64) error }); ok {
    _ = eps.SetEpsilon(EscapeEpsilon) // 开启吸收态逃逸（0.1）
}
```

> `SetEpsilon` 通过窄接口类型断言调用（`MatrixPrefetcher` 实现），避免修改 `EvolutionTuner`
> 冻结接口。scheduler 侧由 `after.SuccessFloor` 驱动，`TauL2` 不再越界到行为门。

### 3.4 包结构与改动面

```
kernel/prefetch/matrix.go       # Config.Epsilon、Plan ε 探针、DefaultEpsilon 挂靠处
kernel/evolve/evolve.go         # Params.SuccessFloor、DefaultSuccessFloor、Config 字段
harness/semantix/evolution.go   # 拆分接线 + ε 下发
docs/specs/issue-254-evolve-absorbing-escape.md   # 本文档
kernel/prefetch/matrix_test.go  # ε 逃逸回归（可选新增文件）
kernel/prefetch/escape_test.go  # 新增：吸收态逃逸确定性测试
harness/semantix/evolution_test.go # 参数拆分接线测试
```

## 4. 测试

- `TestPlanEpsilonProbeEscapesAbsorbingState`（`kernel/prefetch/escape_test.go`）：
  构造 `MinConf` 高、候选 prob 低 → 正常 Plan 空；`Epsilon=1.0` → Plan 返回 1 个 `probe`
  （最高 prob、非 demoted 的候选）；确认探针计入预算、`Epsilon=0`（默认）时行为不变。
- `TestPlanEpsilonZeroKeepsEmptyPlan`：`Epsilon=0`（默认）时保持空计划（向后兼容）。
- `TestPlanEpsilonRespectsNoData`：无转移知识时即使 `ε=1` 也返回空（不引入无意义探测）。
- `TestPlanEpsilonSkipsDemotedCandidates`：被 waste/hit 降级的候选不被选为探针。
- `TestEvolveSchedulerReceivesSuccessFloor`（`harness/semantix/evolution_test.go`）：参数变化后
  `scheduler` 收到的是 `after.SuccessFloor`（默认 0.7）而非 `after.TauL2`。
- 既有 `TestApplyEvolutionChangesNextPlanConfidence` 保持通过（默认 `Epsilon=0` 向后兼容）。

## 5. 后续候选（不在本轮）

- **conf 时间衰减**：无 hit/waste 信号 N epoch 后向默认值回归（需 evolve 增加「无信号」检测）。
- **旁路信号**：从工具实际执行流（非预取路径）补充转移观测，矩阵计数已做，仅 conf 缺逃逸。
- **`demoted()` 免疫修复**：hit EWMA 冷冻结、`WasteHitLimit` 下永不降级的问题。

## 6. 验收

- [ ] ε-探索：构造吸收态场景，`ε>0` 下 Plan 能周期性返回探针并触发 hit/waste 回流；`ε=0` 行为与现状一致。
- [ ] 参数拆分：`evolution.go` 不再把 `TauL2` 当 `SuccessFloor` 下发；`Params` 含 `success_floor` 字段。
- [ ] 全部相关测试通过：`go test ./kernel/evolve ./kernel/prefetch ./harness/semantix`。
- [ ] spec（本文档）已先行合入并审阅。
