# Spec v1 — promote 接线：多信号共识校验 + 失败教训双记忆（Issue #280）

> 判级：Spec-Required（新决策路径接线 + 落盘启用）。本 spec 落地 Issue
> #280「promote 接线时加多信号共识校验 + 失败教训双记忆」：让
> `kernel/promote`（当前零导入死代码）活起来，并在写入提升条目前加
> **双措辞共识门**，把被拒候选存入**独立失败记忆**（不进注入/检索）。
> 基线为当前 main（80bcb23，含 #278/#279）。

## 1. 目标与非目标

### 1.1 现状缺口（真源审计，2026-08-23）

| 缺口 | 证据 |
|---|---|
| promote 是死代码 | `kernel/promote`（promote.go + file_store.go）零导入：`Entry`/`Store`/`ContentVersion`/`CascadeInvalidate`/`NewFileStore` 实现完整，全仓无 `import "semantix/kernel/promote"` |
| 提升门槛=单次判定 | `judge.RuleGate.Chain`（`kernel/judge/judge.go:115-148`）串行 AND 门：指纹 → 规则 → **单次** `Judge.Confirm`（LLM 单 token yes/no）；rubric 六维 checklist（`llm.go:55-75`）压在一次调用里——Cannot Self-Correct 警告的结构性薄弱 |
| 失败无记忆 | judge 拒绝只进计数（`Obs.JudgeReject`）与 usage 日志；同一 (query,slice) 可反复被拒又反复进 judge——无黑名单、无「反复被拒直接拒绝」路径 |
| Security §4.2 部分未落地 | 版本传播（ContentVersion + CascadeInvalidate）已实现；容量有界（TTL/LRU）未落地（promote 无淘汰）；static-origin 标记随 #279 Origin 落地 |

### 1.2 本期范围

```text
1. 接线 promote（活起来）：L3 灰区 judge 批准 → 共识门 → promote.Put；
   后续同 (query, slice, version) 灰区命中先查 promote 免 judge。
2. 共识门（A-MemGuard 单机版）：写入 Entry 前双措辞重问
   （同一候选两种 rubric 表述，全过才提升）；单判定为可配置基线。
3. 失败教训双记忆：judge 拒绝 → 独立 Rejection store（不进注入/检索）；
   同 (query, slice) 拒绝 ≥ reject_limit → 黑名单，promote 写入直接拒绝。
4. 容量有界：promote 条目 TTL 惰性淘汰（Security §4.2.4）。
```

### 1.3 非目标（列为后续）

- **rubric 六维拆分投票**（issue 方案 a）：与双措辞（方案 b）二选一，
  评审定 **b**——六维拆分需重写 rubric 契约且 3 次调用成本更高，双措辞
  直接对应「单次措辞敏感」漏洞且只多 1 次极短调用；
- **用户否决信号**（SliceReject → Rejection）：harness 反馈通道缺失
  （#262 已注记），v1 只接 judge 拒绝；
- **LRU 容量淘汰**：v1 做 TTL（惰性删除），LRU 容量等实测压力数据；
- **verify 等 CLI 路径接 promote**：回放语义必须每次过 judge（可判定性），
  promote 免 judge 只进 gateway 生产路径；
- **共识门用于 L3 主判定**：主路径保持单次 judge（延迟/成本不变），共识
  只门控「提升资格」——这是 A-MemGuard「提升通道」靶心的精确对应。

## 2. 语义设计（单一真源）

### 2.1 promote 生命周期

```text
L3 灰区候选 (query, slice)
  ├─ promote 命中（同 query + 同 ContentVersion + TTL 内）→ 免 judge 复用
  │    （仍过指纹/隔离门；promote 只替代 judge 步骤）
  ├─ promote 未命中 → 单次 judge（现有路径，成本不变）
  │    ├─ 批准 → 共识门（双措辞重问，全过才写）
  │    │    ├─ 共识通过 → promote.Put（版本=T0 内容 sha256）
  │    │    └─ 共识失败 → 视为拒绝 → Rejection.Add
  │    └─ 拒绝 → Rejection.Add（失败教训）
  └─ 黑名单（同 (query,slice) 拒绝 ≥ reject_limit）→ promote 写入直接拒绝
       （judge 批准也拒写；L3 复用本身不受黑名单影响——黑名单只门控提升）
```

- 提升资格 = 共识通过；提升免 judge 资格 = TTL 窗口内未失效。
- 切片内容变化：`CascadeInvalidate` 级联失效（已有实现，接线时随
  store 变更调用——写路径在 slice 更新处，v1 在 promote Lookup 时惰性
  校验 ContentVersion 不匹配即忽略，配合现有 CascadeInvalidate 接口）。

### 2.2 双措辞共识（kernel/judge）

```go
// VariantJudge 是可被共识门包装的 judge：两种 rubric 措辞各判一次。
type VariantJudge interface {
    Judge
    ConfirmSecondary(ctx context.Context, c Candidate) (bool, error)
}

// Consensus 包装器：Confirm = 主措辞 && 次措辞（全过才 true）。
// 被包装者不实现 VariantJudge（如 NoopJudge、测试 stub）→ 退化为单判定
// （透明兼容，共识只对真实 LLM judge 生效）。
func Consensus(j Judge) Judge
```

- `LLMJudge` 实现 `ConfirmSecondary`：同一六维 checklist 的**换表述**
  rubric（`secondaryRubricPrompt`，措辞重排、示例互换、输出契约不变
  ——单 token yes/no）。
- 成本：每次提升资格判定多 1 次极短调用（约 1 输入 token ×2）；L3
  主路径零变化。
- `consensus = 1`（配置）→ 跳过次措辞，退化为单判定基线（对照实验
  与成本敏感部署）。

### 2.3 失败教训双记忆（kernel/promote 扩展）

```go
// Rejection 是一次失败教训：judge 拒绝/共识失败的黑名单证据。
type Rejection struct {
    SourceSliceID string
    Query         string
    Reason        string // "judge_declined" | "consensus_failed"
    RejectedAt    int64
}

// RejectionStore 独立命名空间（独立文件，不进注入/检索/检索索引）。
type RejectionStore interface {
    Add(r Rejection) error
    Count(sourceSliceID, query string) (int, error)
}
```

- 存储：`<promote_db 目录>/rejections.jsonl`（与 promote 条目同目录但
  **独立文件** = 独立命名空间；0600 原子写，容错读）。
- 黑名单：`Count ≥ reject_limit`（默认 2）→ promote.Put 直接拒绝
  （`ErrBlacklisted`，调用方计数/日志，不阻断 L3 复用）。
- 记忆不进注入/检索：Rejection store 只被 promote 决策消费——与
  A-MemGuard「失败蒸馏成教训单独存」对齐。

### 2.4 TTL 淘汰

- `promote_ttl_seconds`（默认 604800 = 7 天）：Lookup 时
  `PromotedAt + TTL < now` 的条目视为未命中并惰性删除（Security
  §4.2.4：不因"已验证"免除淘汰）。
- Rejection 同样 TTL（默认同 promote TTL；黑名单证据也过期，防永久
  冤案——A-MemGuard 双记忆的时效性）。

## 3. 接线（kernel/cache + gateway）

### 3.1 L3Decider 扩展（kernel/cache/l3.go，全部 nil 安全）

```go
// Promote 提供提升条目的查/写（nil → 无 promote 接线，现状不变）。
type Promote interface {
    // Lookup 报告 (query, slice) 是否在 TTL 内已提升且版本匹配。
    Lookup(sourceSliceID, query string, now int64) bool
    // Promote 在共识通过后写入提升条目。
    Promote(e Entry) error
    // Blacklisted 报告 (query, slice) 是否已被反复拒绝（写黑名单门）。
    Blacklisted(sourceSliceID, query string) bool
    // Rejected 记录一次失败教训（judge 拒绝 / 共识失败）。
    Rejected(sourceSliceID, query, reason string, now int64)
}
```

`L3Decider` 字段：`Promote Promote` + `Consensus judge.Judge`（共识门，
nil → 单判定）。`judgeGrey` 流程：

1. 灰区候选 → `Promote.Lookup(sliceID, query, now)` 命中 → 直接批准
   （`o.PromoteHit++`），跳过 judge；
2. 未命中 → 现有单次 judge；
3. 批准 → `Consensus.Confirm`（双措辞；nil → 单判定）：
   - 通过 → `Promote.Promote(entry)`（`o.PromoteWritten++`）；
   - 失败 → `Promote.Rejected(..., "consensus_failed")`
     （`o.PromoteConsensusReject++`）；
4. 拒绝 → `Promote.Rejected(..., "judge_declined")`（复用现有拒绝路径）。

`Obs` 增加 additive 计数：`PromoteHit / PromoteWritten / PromoteRejected
/ PromoteBlacklisted`（#262 的 Obs 结构扩展，wire 无变化）。

### 3.2 gateway 配置与组装（gateway/config.go [cache] 段）

```toml
[cache]
promote_db = ""            # 空 = promote 接线禁用（现状）
promote_ttl_seconds = 604800   # 0 → 默认 7 天
promote_consensus = 2      # 2 = 双措辞共识；1 = 单判定基线
reject_limit = 2           # 0 → 默认 2；同 (query,slice) 拒绝数达此值进黑名单
```

- `validate`：`promote_consensus ∈ {1,2}`、`promote_ttl_seconds >= 0`、
  `reject_limit >= 0`。
- gateway `New`：`promote_db != ""` 时组装
  `promote.NewFileStore(db)` + `promote.NewRejectionFileStore(db 同目录)`，
  包装 TTL 适配器（实现 `L3Decider.Promote` 接口），注入 decider；
  `promote_consensus == 2` 时 `decider.Consensus = judge.Consensus(llmJudge)`。
- CLI verify/其他路径不接（§1.3）。

### 3.3 级联失效接线

`L3Decider` 写路径（slice 内容更新处）：v1 不做显式 CascadeInvalidate
调用——promote Lookup 时惰性比对 ContentVersion（Entry 存版本，命中
条件含版本匹配），切片更新自然失效。`CascadeInvalidate` 接口保留
（测试与未来批量回收用）。

## 4. 配置与可观测

- 无新 wire：usage 事件不加字段（promote 计数走 gateway 日志 +
  `Obs` 快照）；`Obs` additive 字段仅在 kernel/cache 内部。
- gateway 日志：promote 命中/写入/黑名单/共识拒绝各一行（`log.Printf`，
  与 judge 观测一致）。
- `promote_db` 路径与 slice store 同目录惯例：默认
  `<store 同目录>/promote.jsonl`（显式配置优先）。

## 5. 测试计划与验收标准

- [ ] kernel/judge：`Consensus` 包装器（双全过才 true；任一 false →
      false；非 VariantJudge 退化为单判定）；`ConfirmSecondary` 输出
      契约与主措辞一致（stub judge 断言两次调用）；consensus=1 跳过
      次措辞。
- [ ] kernel/promote：Rejection store（Add/Count、独立文件、容错读、
      TTL 过期）；黑名单（Count ≥ limit → Put 拒绝）；TTL 惰性淘汰
      （Lookup 过期条目未命中 + 删除）；文件 store 与 MemStore 行为
      一致。
- [ ] kernel/cache：judgeGrey 全路径——promote 命中免 judge（judge
      调用计数为 0）、批准后写入、共识失败不写 + Rejection、
      黑名单拒写、Obs 计数、nil Promote 行为与现状逐字节一致。
- [ ] gateway：配置校验；promote_db 组装（含 consensus=1 退化）；
      e2e——灰区命中两次，第二次免 judge（延迟/judge 计数可测）；
      MemoryGraft 式伪成功经验：共识门下提升失败率高于单判定基线
      （stub judge 主措辞过/次措辞拒的 fixture）。
- [ ] 回归：`go test ./...`（除既有 pre-existing 环境失败）全绿；
      `go vet ./...`、`git diff --check`。

## 6. 参考

- A-MemGuard（arXiv:2510.02373）：多记忆共识 + 双记忆（失败单独存），
  攻击成功率 -95%；
- MemoryGraft（arXiv:2512.16962）：promote 通道是攻击靶心；
- Cannot Self-Correct（arXiv:2310.01798）：单次自评不可靠；
- Security-安全设计.md §3.2 / §4.2（版本传播 + 容量有界）；
- kernel/promote 既有实现（Entry/ContentVersion/CascadeInvalidate）。
