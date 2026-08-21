# Spec v1 — 预取补 Markov 三指标 + Beta-Binomial 计数（Issue #272）

> 判级：指标透出部分 Spec-Exempt；Beta-Binomial 改判定模型 → **Spec-Required**。
> 本 spec 细化 Issue #272（2026-08 文献综述域 5 派生的 RFC），为 T-Slice 转移矩阵补全
> 1997 Markov Prefetcher 的评价三元组（coverage / accuracy / timeliness）并透出，
> 并把裸 EWMA 计数升级为带时间折扣的 Beta-Binomial 后验。基线为当前
> `feat/issue-272-markov-metrics`（HEAD=80b76bc，含 #262/#269/#270/#267/#268/#254/#255）。
> 与 #282（demote 吸收态 probation）**同文件面但独立**：本条只管度量与计数模型，
> 不含 #282 修复（已有独立分支 `fix/issue-282-probation`），依赖关系见 §1.3。

## 1. 目标与非目标

### 1.1 现状缺口（真源审计，2026-08-21）

| 缺口 | 证据 |
|---|---|
| hit/waste 反馈率不透出 | `kernel/prefetch/matrix.go` `MatrixPrefetcher.hit/waste`（map[string]float64 EWMA）仅服务 `demoted()`，`Stats` 只有 Transitions/Pairs/TopNext |
| coverage 无处统计 | 无"实际调用中被预取覆盖的比例"任何计数；`Observe` 只累计 bigram，不区分"该后继曾被 Plan 提议" |
| timeliness 无处统计 | `PrefetchHitPayload/PrefetchWastePayload`（`kernel/event/event.go:118-126`）只有 `Targets`，无预热/消费时间戳；harness `prefetchedInjectResult` 无预热完成时间 |
| 计数是裸 EWMA | `matrix.go` `ObserveHit/ObserveWaste` 用 `ewma()`（α=Decay=0.2）维护点估计，无先验平滑、无时间折扣配置、无置信信息；单次 waste 即令 `w/h=∞` 立即降权（小样本过激） |
| 报告无三曲线 | `scripts/evolution-curve/main.go` + `docs/reports/agile2-evolution-curve.md` 只有命中率/成本/conf 曲线，无 coverage/accuracy/timeliness |

### 1.2 本期范围

```text
kernel/prefetch（矩阵内）
  → Stats 扩展：per-key 后验(α,β)/均值/计数 + 全局 coverage/accuracy（分母一并透出）
  → 内部统计：Observe 对照最近一次 Plan 提议集统计 covered；累计 hit/waste 计数
  → Beta-Binomial 计数：hit/waste EWMA → 带时间折扣的 (α,β) 后验；demoted() 改用后验均值
kernel/event（wire additive）
  → PrefetchHit/Waste payload 增 LeadMs（消费-预热毫秒差，负=迟到）
harness（timeliness 数据流）
  → prefetchedInjectResult 增 WarmAt（预热完成时间，agent.go startInjectWarm 处打点）
  → bridge.RecordPrefetch 增 lead 参数并写入 payload
报告（#228 自进化曲线）
  → evolution-curve 脚本 + 报告新增 coverage/accuracy/timeliness 三条曲线
测试
  → matrix 单测：Stats 透出、Beta 判定行为（不误伤/可降级/可恢复）、漂移注入对比
```

### 1.3 非目标（列为后续，不进入本期）

- **#282 demote 吸收态 probation**：独立缺陷，已有 `fix/issue-282-probation`。本 spec 不
  实现它；其修复合入后 coverage 会因"永久拉黑的转移边重新可提议"而更准，但本条的
  coverage 口径不依赖 demote 状态（§2.1），不阻塞；
- evolve 引擎 `PrefetchConf` 语义变化：`ApplyEvolution` 写入的 `MinConf` 保持"转移概率
  阈值"语义不变（§2.3 说明为什么）；
- 真实会话采数（报告边界声明延续：依赖 #234 usage 重放 / #239 固定环境）；
- Plan 提议逻辑本身的行为变化（TopK/预算/ε 探针不动，只改判定模型与观测）。

## 2. 语义定义（单一真源）

### 2.1 Markov 三指标（移植自 Prefetching using Markov Predictors, ISCA'97）

| 指标 | 定义 | 计算位置 |
|---|---|---|
| coverage | 实际只读调用中，被预取覆盖（该后继曾进入最近一次 Plan 提议）的比例 = `coveredReadOnly / readOnlyCalls` | 矩阵内：`Observe` 对照最近一次 `Plan` 的提议集 |
| accuracy | 预取命中率 = `totalHits / (totalHits + totalWastes)`（反馈层面，与 #228 事件口径一致） | 矩阵内：`ObserveHit/ObserveWaste` 累计计数 |
| timeliness | 预热完成早于消费的提前量 `LeadMs = ConsumeAt − WarmAt`（正=赶趟；负=迟到）；聚合 = 会话内 hit 事件的平均 lead + 迟到率 | harness：`storePrefetch` 记 WarmAt，消费点算 lead，事件 payload 携带 |

分母约定：`readOnlyCalls=0` 或 `totalHits+totalWastes=0` 时对应指标输出 `N/A`（分母
0 不除，透出分母本身由消费端判断）。分母**只统计只读调用**：writer 工具被 N12 红线
禁止预取（`matrix.go` 只对 readOnly 后继提议），计入分母只会稀释 coverage 的语义。

### 2.2 Beta-Binomial 计数模型（替换裸 EWMA）

per-key（key = 候选工具名）维护 Beta 后验参数，先验 `α₀=β₀=1`（均匀，常量
`betaPrior=1.0`，不暴露配置）：

```text
ObserveHit(key):   α ← (1−Decay)·α + 1 ;  β ← (1−Decay)·β
ObserveWaste(key): β ← (1−Decay)·β + 1 ;  α ← (1−Decay)·α
```

- `Decay` 配置字段复用（默认 0.2），语义从"EWMA 平滑"改为"时间折扣"（每次反馈
  历史权重 ×(1−Decay)，与旧 EWMA 的遗忘数学同构）；
- 后验均值 `μ = α/(α+β)`；90% 置信下界 `L` 用 Wilson 近似（Beta 分位数等价、浮点
  n 可用、免特殊函数）；
- **`demoted()` 判定**：`n=α+β−2 > 0`（有反馈）且 `μ < 1/(1+WasteHitLimit)` →
  降权。无反馈（`n=0`，先验态）不降权（与现状 `w==0 → false` 一致）。

与旧 EWMA 判定的语义对齐与差异（评审点，见 §2.3）：

- 旧判定 `w_rate/h_rate > WasteHitLimit` 与 `μ < 1/(1+WasteHitLimit)` 在**无先验**下
  数学等价（`w/h>L ⟺ h/(h+w)<1/(1+L)`）；先验 +1 使 Beta 在**小样本下更保守**：
  单次 waste 不再立即降权（旧 `w/h=∞` 即降权），连续 waste 才降权；
- 连续 hit 可恢复（旧 waste 被折扣衰减），与 `TestWastePenaltyRecoversAfterSustainedHits`
  的产品承诺一致（§5 验算全兼容）。

### 2.3 两个评审点（对 issue 措辞的工程取舍，请审查确认）

1. **`MinConf` 语义不变**。issue 建议"MinConf 判定改用后验均值下界"。若照做，
   `MinConf` 从"转移概率阈值"变为"反馈置信阈值"，将与 evolve 闭环的既有约定冲突：
   `ApplyEvolution(after.PrefetchConf)` 直接写 `MinConf`（`matrix.go:41-55`），
   `PrefetchConf` 的 0.05–0.95 值域、`PrefetchStep=0.05` 调步（`evolve.go`）全部按
   "转移概率阈值"校准过。改语义 = 引擎参数含义漂移，需单独评审。故本条：转移概率
   门槛（`Plan` 的 `prob >= MinConf`）与反馈门槛（`demoted()`）**分开**，只把反馈门槛
   换成 Beta 后验；`MinConf` 保持原语义。
2. **判定用后验均值而非下界**。下界判定会让恢复路径（持续 hit 撤销降权）几乎不可达：
   验算 `TestWastePenaltyRecoversAfterSustainedHits`（WasteHitLimit=0.5）在 90% 下界
   判定下 15 次 hit 后 `L≈0.60 < 0.667` 仍不恢复，产品承诺"later hits may restore it"
   失效。故判定用 `μ`（与旧语义等价），`L`（90% 下界）作为**观测**随 `Stats` 透出，
   置信信息不参与判定。漂移"更稳"由 ①先验平滑（小样本不误伤）②`Decay` 可独立调参
   达成（§5 c3 行为断言，不承诺数学上更快的收敛速度——与 EWMA 同遗忘率时速度同构）。

## 3. 实现设计

### 3.1 `kernel/prefetch`：Stats 扩展与内部统计

`Stats` 新增（纯值快照，`Stats()` 现有签名不变、返回类型扩展字段）：

```go
type Stats struct {
    Transitions int
    Pairs       map[string]int
    TopNext     map[string]int

    // --- Issue #272 新增（Markov 三指标 + 计数模型）---
    ReadOnlyCalls int               // 只读调用总数（Observe 且 readOnly）
    CoveredCalls  int               // 其中被最近一次 Plan 提议覆盖的
    Coverage      float64           // CoveredCalls / ReadOnlyCalls（分母 0 → 0，消费端用分母判 N/A）
    TotalHits     int               // ObserveHit 累计
    TotalWastes   int               // ObserveWaste 累计
    Accuracy      float64           // TotalHits / (TotalHits + TotalWastes)（分母 0 → 0）
    HitRate       map[string]float64 // per-key 后验均值 μ（替代旧 EWMA 透出）
    Alpha         map[string]float64 // per-key 后验 α（含先验）
    Beta          map[string]float64 // per-key 后验 β（含先验）
    LowerBound    map[string]float64 // per-key 90% Wilson 下界 L（观测用）
    Hits          map[string]int     // per-key hit 计数
    Wastes        map[string]int     // per-key waste 计数
}
```

`MatrixPrefetcher` 内部变更：

- `hit/waste`（map[string]float64 EWMA）→ `alpha/beta`（map[string]float64 后验），
  `ObserveHit/ObserveWaste` 按 §2.2 更新；另加 `hitCount/wasteCount`（map[string]int）
  与 `totalHits/totalWastes`（int）；
- coverage 统计：新增 `lastPrev string`、`lastProposed map[string]bool`（`Plan` 每次
  返回前记录提议 key 集与入参 last 工具）；`Observe` 在加 bigram 后：若
  `prev == lastPrev && lastProposed[next]` 则 `coveredCalls++`；`readOnly` 时
  `readOnlyCalls++`。全部在 `m.mu` 临界区内，无锁外状态；
- `demoted()` 改用 §2.2 判定（`n==0` 不降权）；`Config.Decay` 注释更新为"Beta 时间
  折扣"；
- `Plan` 提议逻辑（TopK/预算/ε 探针/排序）**不改**，仅在返回前记录提议集。

### 3.2 `kernel/event`：payload additive 扩展

```go
type PrefetchHitPayload struct {
    Targets []string `json:"targets"`
    LeadMs  int64    `json:"lead_ms,omitempty"` // 消费 − 预热完成（正=赶趟，负=迟到）
}
// PrefetchWastePayload 同加 LeadMs（waste 无真实消费，LeadMs=0 语义="未消费"，见 §3.3）
```

wire-stable：旧日志/旧消费者忽略未知字段；`omitempty` 保持旧事件序列化不变。

### 3.3 harness：timeliness 数据流

- `harness/agent/prefetch_feedback.go`：`prefetchedInjectResult` 增 `WarmAt time.Time`；
  `agent.go:2955` `startInjectWarm` 构造时填 `WarmAt: time.Now()`（预热完成点）；
  `recordPrefetch` 计算 `lead := time.Since(got.WarmAt)` 传给 `RecordPrefetch`；
- `harness/semantix/bridge.go`：`RecordPrefetch(hit bool, targets []string, turn int)` →
  增参 `lead time.Duration`（内部唯一调用点 `agent.recordPrefetch`，同步改）；payload
  填 `LeadMs`。waste 路径 lead 传 0（无消费点，语义"未消费"）；
- `wastePrefetch`/`takePrefetch` 已有调用点自然带上 WarmAt。

### 3.4 `scripts/evolution-curve`：三曲线

- `row` 增 `Coverage/Accuracy/LeadMs` 字段；`playSession` 每会话取 `matrix.Stats()` 快照
  （coverage/accuracy），事件带确定性模拟时钟（`At` 基础上 lead 由种子确定，如
  `seq*80 ms` 正 lead——脚本是受控模拟，timeliness 验证的是**管线**：事件 → payload →
  聚合 → 曲线，不是真实时间，报告边界声明注明"模拟时钟"）；
- `writeReport` 增三张 `xychart-beta` 曲线（coverage/accuracy/timeliness × 会话）；
- CSV 增三列；`docs/reports/agile2-evolution-curve.md` 由脚本重生成（不手改）。

## 4. 契约与配置

- `Config.Decay`：语义改为 Beta 时间折扣，默认 0.2（值域 (0,1] 不变，校验不变）；
- `Config.WasteHitLimit`：语义改为"后验权重比阈值"（`wastes_weights/hits_weights >
  WasteHitLimit` ⟺ `μ < 1/(1+WasteHitLimit)`），默认 3.0 不变，值域校验不变；
- `Stats()` 签名与并发语义不变（`mu` 保护，快照复制）；
- `RecordPrefetch` 签名变更仅影响 `harness/agent/prefetch_feedback.go` 一处调用方，
  无外部消费者（`grep -rn RecordPrefetch` 仅 bridge 定义 + agent 调用）。

## 5. 验收标准

- [ ] **c1 Stats 透出三指标与后验**：`Stats()` 返回含
  Coverage/Accuracy/ReadOnlyCalls/CoveredCalls/TotalHits/TotalWastes 及 per-key
  HitRate/Alpha/Beta/LowerBound/Hits/Wastes；分母 0 时指标为 0 且分母透出（消费端
  可判 N/A）；旧字段 Transitions/Pairs/TopNext 不变；
- [ ] **c2 Beta 判定行为**：现有 4 个 waste-penalty 测试语义全兼容——单次 waste 不
  立即降权（新断言）、5 wastes 无 hit 降权、4 hits+1 waste 存活、5 hits+15 wastes
  降权、5 wastes+15 hits（WasteHitLimit=0.5）恢复；
- [ ] **c3 漂移注入实验**：`10 hits → 25 wastes → 20 hits` 序列下断言 ①第 1 次 waste
  不降权（先验平滑，旧 EWMA 同场景立即降权——行为差异明确可证）②waste 持续期间 μ
  单调下降并穿越降权阈值（漂移最终被识别）③恢复阶段 20 hits 后撤销降权（可恢复）；
- [ ] **c4 事件 wire additive**：`PrefetchHitPayload/PrefetchWastePayload` 含
  `lead_ms`，旧 payload 反序列化不报错、新字段缺省 0；
- [ ] **c5 timeliness 管线**：harness 端 `startInjectWarm→takePrefetch/wastePrefetch→
  RecordPrefetch` 事件携带 LeadMs（正=赶趟）；单测覆盖 store→take 的 lead 计算；
- [ ] **c6 报告三曲线**：`go run ./scripts/evolution-curve` 确定性重跑后
  `docs/reports/agile2-evolution-curve.md` 含 coverage/accuracy/timeliness 三张
  xychart 曲线，CSV 含新列；报告标注 timeliness 为模拟时钟；
- [ ] **c7 兼容与门禁**：`go vet ./...` 干净、`go test ./...` 全绿（新增测试覆盖
  c1–c5），`docs/Agile路线图.md` 状态同步。

## 6. 测试计划（按风险放置）

| 层 | 测试 | 覆盖 |
|---|---|---|
| kernel/prefetch | `TestStatsMarkovMetrics`（coverage/accuracy 计数、分母 0 → N/A 可判）、`TestStatsPerKeyPosterior`（α/β/μ/L 透出） | c1 |
| kernel/prefetch | `TestBetaSingleWasteDoesNotDemote`（1 waste 不降权 vs 旧行为）、`TestBetaWasteOnlyDemotes`、`TestBetaSustainedWasteDemotes`、`TestBetaRecoversAfterSustainedHits` | c2 |
| kernel/prefetch | `TestBetaDriftInjection`（10h→25w→20h 三断言） | c3 |
| kernel/prefetch | `TestCoverageTracksRecentPlan`（Observe 对照最近提议集） | c1 |
| kernel/event | `TestPrefetchPayloadLeadMsAdditive`（旧 payload 容错） | c4 |
| harness/agent | `TestRecordPrefetchLeadTime`（store→take 的 LeadMs） | c5 |
| scripts | `go run ./scripts/evolution-curve` 重跑对比报告 diff 仅预期列/曲线 | c6 |

## 7. 实施步骤（spec 审查通过后执行）

1. `kernel/prefetch`：后验计数 + demoted() 换判定 + Stats 扩展 + coverage 统计 + 单测（c1–c3）；
2. `kernel/event` + `harness`：LeadMs wire + WarmAt 打点 + RecordPrefetch 签名 + 单测（c4–c5）；
3. `scripts/evolution-curve`：三曲线 + CSV 列 + 报告重生成（c6）；
4. 全量 `go vet ./...`、`go test ./...`、`docs/Agile路线图.md` 同步（c7）；
5. 复核 diff 边界（仅本 issue 文件面），提交。

## 8. 参考

- Issue #272（RFC 提案，含 ISCA'97 / arXiv:2607.12236 / arXiv:2606.07846 文献依据）
- `docs/specs/issue-255-prefetch-feedback-decay.md`（hit/waste 反馈衰减既有约定）
- `docs/reports/agile2-evolution-curve.md`（U43 报告，发现 1/2 与本条的关系）
- `kernel/prefetch/matrix.go`、`kernel/event/event.go`、`harness/agent/prefetch_feedback.go`、
  `harness/semantix/bridge.go`、`scripts/evolution-curve/main.go`（当前真源）
- `fix/issue-282-probation`（同文件面独立 bug，本条不实现）
