# Spec v1 — L3 负向观测接线:judge 拒绝/误命中统计 + 校准报告(Issue #262)

> 判级:Spec-Required。本 spec 细化 Issue #262「L3 负向观测接线」,补齐 L3 运行时
> 负向事件的观测盲区(judge 拒绝不可见、误命中无反馈),并交付统一校准报告命令
> `semantix calibrate`。基线为当前 main;不扩展到 harness 端反馈信号、事件总线新
> Kind、真实 300 对样本(沿用 Issue #8 遗留口径)。

## 1. 目标与非目标

### 1.1 现状缺口(真源审计,2026-08-21)

| 缺口 | 证据 |
|---|---|
| 运行时 L3 决策零负向观测 | `kernel/cache/l3.go` `judgeGrey` 构造的 `judge.RuleGate` 未挂 `Stats`;`L3Decider` 无任何计数器;gateway 只写 `usage.Event{L3Reuse bool}` 一个布尔位 |
| 负向统计只存在于离线路径 | `judge.Stats`(Confirmed/RulesReject/Fingerprint/JudgeReject/JudgeApproved/NeedJudge)仅在 `verify` 离线回放中接线 |
| 误命中无反馈回路 | 用户对 L3 复用结果不满意而重试时,gateway 会再次命中同一缓存返回同一错误结果,既无统计也无行为纠正 |
| 校准报告形态不完整 | `eval-judge` 只有 consistency/ε_fa/Δerr_upper,无混淆矩阵、无误拒绝率 ε_fr、无 precision/recall/F1、不消费运行时统计 |

### 1.2 本期范围

```text
L3 运行时决策(DecideL3)
  → 负向计数接线(L3Decider.Obs:规则/指纹/隔离/judge 拒绝、批准、复用)
  → gateway 写入 usage 日志(per-turn 决策明细,仅 additive 字段)
  → 误命中反馈回路(同 session 对已复用 query 的重试 → 疑似误命中 + 绕过 L3)
  → semantix calibrate:离线校准(oracle 混淆矩阵)+ 运行时汇总 + 一致性门禁
```

### 1.3 非目标(列为后续 issue,不进入本期)

- harness/Reasonix 桥接侧的用户编辑/回滚反馈信号(SliceReject 发布者仍缺);
- `kernel/event` 新增 L3 事件 Kind(wire-stable 代价高,本期反馈走 usage 日志);
- 真实 300 对 judge 一致性实测(待 #20/#58 真实数据);
- judge 拒绝后的降级重试策略(本期只做统计与绕过,不做候选替换)。

## 2. 负向观测语义(单一真源)

### 2.1 拒绝分类(与现有 `judge.Stats` 口径对齐,按原因不可合并)

| 计数 | 语义 | 判定点 |
|---|---|---|
| `RulesReject` | 规则拒绝:clear miss(相似 < τ_low),或 grey 无 judge 的保守拒绝 | `RuleGate.Check` zone.Miss / grey-no-judge |
| `FingerprintReject` | 依赖失效:指纹门错误、mtime/sha256 变更、`L3Safe=false` 无 deps 拒绝 | `RuleGate.Chain` 指纹门 + `L3Decider.verified` |
| `IsolatedReject` | 上下文/model 隔离拒绝(Issue #133):带 stamp 查询与条目 stamp 不匹配 | `DecideL3` ContextHash/Model 校验 |
| `JudgeReject` | judge 判定拒绝(approve=false) | `RuleGate.Chain` judge 分支 |
| `JudgeApproved` | judge 判定批准 | 同上 |
| `Reused` | 最终复用(全部 gate 通过) | `DecideL3` 返回处 |

### 2.2 误命中双口径(分开呈现,禁止混算)

- **离线误批准率 ε_fa**:judge approve 但 oracle 判 reject 的占比 —— oracle 评估集口径(与 #7/#8 一致),由 `calibrate --audit` 计算;
- **运行时疑似误命中 L3FalseHit**:L3 复用后,同一 session 内出现与已复用 query 高度相似的新请求(用户重试) —— 启发式口径,报告与 CLI 输出中必须标注「疑似」。

## 3. 运行时接线

### 3.1 `kernel/cache`:L3Decider 负向观测

`L3Decider` 新增观测字段(`kernel/cache/l3.go`,nil 安全,不接线即零开销):

```go
// Obs 是 L3Decider 负向观测计数快照(纯值,可安全复制)。
type Obs struct {
    Candidates        int // Result 类型候选总数(检索命中且 type==Result)
    Grey              int // 判定为 grey 的候选数(无 judge 时同时计 RulesReject)
    RulesReject       int // clear miss / grey 无 judge 保守拒绝
    FingerprintReject int // 指纹门错误 / mtime/sha256 变更 / L3Safe=false
    IsolatedReject    int // 上下文/model 隔离拒绝
    JudgeReject       int // judge 判定拒绝
    JudgeApproved     int // judge 判定批准(后续 gate 仍可能兜底拒绝)
    Reused            int // 最终复用
}

// L3Decider 字段:
//   Obs *ObsAccum      — 线程安全累计器(nil 安全,Snapshot 读快照);
//   OnDecide func(Obs) — 每次 DecideL3 返回前回调本次调用增量(nil 安全)。
// gateway 并发路径用 per-request decider 副本 + 局部 ObsAccum.Snapshot 捕获
// 本次调用的精确 delta 写 usage 日志(避免并发请求混计数);ObsAccum/OnDecide
// 供单线程测试与诊断聚合。DecideL3 签名不变,不破坏 cache.Decider 接口。
```

计数规则:

1. `judgeGrey` 内改为 `gate := judge.RuleGate{Judge: d.Judge, Stats: &gs}`(局部
   `judge.Stats`),Chain 返回后把 `gs` 的 RulesReject/Fingerprint/JudgeReject/
   JudgeApproved 合并进 `d.Obs`,并 `Obs.Grey++`;
2. `DecideL3` 检索循环内:`s.Type == Result` 时 `Candidates++`;zone.Miss 时
   `RulesReject++`;ContextHash/Model 不匹配时 `IsolatedReject++`;
3. `verified` 失败(`FingerprintReject++`)由 `DecideL3` 调用点计数,不在
   `verified` 内部(nil-safe 保持函数签名不变);
4. 返回 `L3Result` 时 `Reused++`。

### 3.2 `kernel/usage`:Event 新字段与聚合(仅 additive)

`usage.Event` 新增(JSON wire-stable,旧日志/旧消费者不受影响):

```go
L3GreyCandidates    int  `json:"l3_grey_candidates,omitempty"`
L3JudgeReject       int  `json:"l3_judge_reject,omitempty"`
L3JudgeApproved     int  `json:"l3_judge_approved,omitempty"`
L3RulesReject       int  `json:"l3_rules_reject,omitempty"`
L3FingerprintReject int  `json:"l3_fingerprint_reject,omitempty"`
L3IsolatedReject    int  `json:"l3_isolated_reject,omitempty"`
L3FalseHit          bool `json:"l3_false_hit,omitempty"`
```

- `Summarize` 聚合为 `Summary` 对应字段(`L3FalseHits int` 等),不改变现有字段;
- `semantix usage --json` 输出补运行时负向汇总(usageJSON 扩展,additive)。

### 3.3 gateway:负向记录与误命中反馈回路

`Gateway` 新增(有界、并发安全):

```go
reuseMu  sync.Mutex
l3Reuses map[string]l3ReuseEntry // sessionID -> 最近一次 L3 复用(每 session 至多 1 条)
```

```go
type l3ReuseEntry struct {
    Query   string // 已复用请求的原始 query
    SliceID string
    At      time.Time
}
```

请求流程(`pipeline.go`):

1. **误命中检测(重试启发式)**:`DecideL3` 之前,查本 session 的复用记录;检测
   即消费该记录(无论相似与否,每记录至多触发一次)。若存在且新 query 与记录
   query 的归一化编辑距离 ratio ≥ `[cache] false_hit_sim`(-1 关闭检测,
   0/未指定 → 默认 0.6),则本轮**绕过 L3**(直接走 L2+上游,避免重复返回错误
   缓存)→ usage 事件记 `L3FalseHit=true`;
2. **正常 L3 路径**:`DecideL3` 返回非 nil 时,把本次 Obs delta 写入 usage 事件
   (L3Reuse=true 沿用),并记录/覆盖本 session 复用条目(有界:map 总条目上限
   1024,超限删除最旧条目;每 session 仅保留最近 1 条);
3. **未命中路径**:同样写入 Obs delta(决策明细,omitempty 时零值省略)。

内存上界:map 上限 1024 条目 + 每 session 1 条,LRU 语义,无界增长不可能。

归一化编辑距离:Levenshtein distance `d`,ratio = `1 - d/max(len1,len2)`(空串对
空串 = 1)。实现为 gateway 内私有 helper(`gateway/levenshtein.go`),单测覆盖。
阈值经 `CacheConfig.FalseHitSim`(`[cache] false_hit_sim`,默认 0.6,-1 关闭检测)
下发,不新增 CLI flag。

## 4. `semantix calibrate` 命令契约

### 4.1 用法与输入

```
semantix calibrate [--audit <oracle.tsv>] [--usage <usage.jsonl>]
                   [--stub yes|no|error] [--judge-base-url ...] [--judge-model ...]
                   [--judge-protocol openai|anthropic] [--p-prom 0.3]
                   [--min-consistency 95] [--json]
```

- `--audit` 与 `--usage` **至少给一个**(两者都给 → 完整报告;只给一个 → 对应部分);
- `--audit` 复用 eval-judge 的 TSV 格式 `query<TAB>cached_answer<TAB>oracle(approve|reject)`
  与 judge 构造(--stub / --judge-* + `SEMANTIX_JUDGE_API_KEY`);
- `--usage` 是 gateway 的 usage 日志路径(默认 `.semantix/usage.jsonl` 为空时
  按"无运行时数据"处理,不报错,报告输出 N/A)。

### 4.2 输出(文本 TSV / `--json` 信封)

**离线校准块**(judge 判定 vs oracle,仅 `--audit`):

```
confusion: tp=6 fp=1 tn=7 fn=0
consistency_pct  false_approve_pct  false_reject_pct  precision  recall  f1  delta_upper_pct
92.9              7.1                0.0               0.857      1.000   0.923  2.14
```

- TP = judge approve ∧ oracle approve;FP = approve ∧ oracle reject;
  TN = reject ∧ oracle reject;FN = reject ∧ oracle approve;
- consistency = (TP+TN)/N;ε_fa = FP/N;ε_fr = FN/N;
  precision = TP/(TP+FP);recall = TP/(TP+FN);F1 = 调和均值;
  Δerr_upper = ε_fa × p_prom(对齐 eval-judge)。

**运行时汇总块**(usage 日志,仅 `--usage`):

```
l3_reuses  l3_grey  judge_reject  judge_approved  rules_reject  fingerprint_reject  isolated_reject  false_hits  false_hit_rate_pct
12         5        2             3               40            1                    2                1           8.3
```

- false_hit_rate = L3FalseHits / L3Reuses(复用数为 0 时输出 `N/A`);
- 两个口径分栏输出,文本头部各自注明 `# offline (oracle)` / `# runtime (usage log)`,
  JSON 中分别为 `audit` 与 `runtime` 两个对象,禁止合并。

**门禁**:`--min-consistency`(默认 95,仅 `--audit` 时生效)不达标 → 退出码 3
(JSON 模式输出 §4.3 失败信封)。

### 4.3 退出码(对齐 U19 契约)

| 码 | 含义 |
|---|---|
| 0 | 通过(或仅运行时报告,无门禁) |
| 1 | 运行时/IO 错误(audit 文件打不开) |
| 2 | 用法/输入错误(audit 行格式错、缺 judge 后端、audit/usage 都没给) |
| 3 | 一致性门禁不达标(`--min-consistency`) |

usage 日志缺失/损坏按「无运行时数据」处理:文本输出 `# runtime (usage log): N/A — <原因>`,
JSON 输出 `runtime.na=true`,不退出 1(运行时观测失败开放,网关新部署无日志是常态)。

## 5. 验收标准

- [ ] **c1 运行时负向统计可查询**:`semantix usage --json` 输出含
  `l3_grey_candidates/judge_reject/judge_approved/rules_reject/fingerprint_reject/isolated_reject/false_hits`
  聚合(usage 日志为空时输出零值,不报错);
- [ ] **c2 拒绝分原因可见**:gateway 端到端(或单测)验证 L3Decider 各拒绝路径
  分别计数,规则/指纹/隔离/judge 四类拒绝互不混淆;
- [ ] **c3 误命中反馈回路闭环**:同 session 重试相似 query → 请求绕过 L3 走
  上游,usage 事件 `l3_false_hit=true`;不同 session 或低相似度不触发;
- [ ] **c4 校准报告完整**:`calibrate --audit`(stub yes/no 确定性)输出混淆矩阵
  + ε_fa/ε_fr + precision/recall/F1 + Δerr_upper;`--min-consistency` 不达标
  exit 3;`--json` 信封结构可解析;
- [ ] **c5 运行时汇总与离线分栏**:`calibrate --usage` 输出运行时汇总,误命中率
  复用数为 0 时输出 N/A;offline/runtime 口径不混算(JSON 两个对象);
- [ ] **c6 兼容性**:usage.Event 仅 additive,旧日志 JSON 可读、旧字段不删;
  `go vet ./...` 干净、`go test ./...` 全绿(新增测试覆盖 c1-c5)。

## 6. 测试计划(按风险放置)

| 层 | 测试 | 覆盖 |
|---|---|---|
| kernel/cache | `TestL3DeciderObsCounters`(hit/grey-judge-reject/isolated/fingerprint 路径) | c2 |
| kernel/usage | Summarize 新字段聚合 + 旧日志容错 | c1/c6 |
| gateway | `TestL3FalseHitRetryBypass`(同 session 重试)、`TestL3FalseHitSimThreshold`、`TestL3ReuseMapBound`(1024 上限) | c3 |
| cmd/semantix | `TestCalibrateConfusionMatrix`、`TestCalibrateGate`(exit 3)、`TestCalibrateRuntimeOnly`、`TestCalibrateJSONEnvelope` | c4/c5 |

## 7. 参考

- `docs/specs/gw4-m0m1-acceptance-spec.md` §3.5(L3 设计:judge fail-closed)
- `docs/reports/issue-07-acceptance.md`(误命中率 oracle 口径)
- `docs/reports/issue-08-acceptance.md`(eval-judge 一致性/ε_fa 基线、#8 遗留)
- `docs/issues/issue-01-l3-三段阈值.md`(灰色地带三段阈值)
- `kernel/cache/l3.go`、`kernel/judge/judge.go`、`kernel/usage/usage.go`、
  `gateway/pipeline.go`(当前真源)
