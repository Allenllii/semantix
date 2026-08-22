# Spec v1 — judge 调用失败独立计数：JudgeError（Issue #245）

> 判级：Spec-Required。本 spec 修复 `kernel/judge` 把「judge 调用出错」记入 `RulesReject`
> 的口径污染，新增 `JudgeError` 计数并沿 #262 已铺好的观测链一路接到 CLI。基线为
> `feat/issue-272-markov-metrics` @ `35c46ec`（#262 已合入，commit `80b76bc`）。
> 不改任何判决行为——fail-closed 语义逐字保持，只改计数归属。

## 0. 一句话

judge 打不通和 judge 说「不」是两回事，现在它们记在同一个桶里；本期把前者拆成
`JudgeError`，让观测方能区分「judge 拒绝」与「judge 不可用」。

## 1. 现状与证据

### 1.1 缺陷本体

`kernel/judge` 的 `RuleGate.Chain` 在 `Judge.Confirm` 返回 error 时计入 `RulesReject`：

```go
ok, err := g.Judge.Confirm(ctx, c)
if err != nil {
    g.count(Stats{RulesReject: 1})            // ← 网络错误/超时/解析失败记成「规则拒」
    return Reject, "judge error: conservative reject", err
}
```

而 `RulesReject` 的字段注释是 `miss / grey-without-judge`。结果是 `JudgeReject`
低估、`RulesReject` 高估，且这条污染会穿透整条 #262 观测链一路到 CLI。

### 1.2 真源审计（2026-08-21，当前工作树）

| 事实 | 锚点符号 | 证据 |
|---|---|---|
| judge error 记入 RulesReject | `kernel/judge/judge.go` `RuleGate.Chain` | 上方代码块 |
| `Chain` 内 `if g.Judge == nil` 分支是死代码 | `kernel/judge/judge.go` `RuleGate.Chain` | `go test -covermode=count ./kernel/judge/` 输出块 `judge.go:131.20,136.3 2 0`（块体执行 0 次），其守卫 `judge.go:131.2,131.20 1 3` 求值 3 次 |
| 死代码成因 | `RuleGate.Check` | `Check` 只在 `default:`（grey）分支且 `g.Judge != nil` 时返回 `NeedJudge`；`Chain` 的 `if v != NeedJudge { return }` 之后必然 `g.Judge != nil`。`Check`/`Chain` 均为值接收者，两次读的是同一份不可变副本；typed-nil 接口在两处对称，不构成例外 |
| `kernel/judge` 自己的测试从不传 `Stats` | `RuleGate.count` | 覆盖率块 `judge.go:151.20,153.3 1 0`——`g.Stats.add(s)` 从未执行；`judge_test.go` 里 `Stats` 零匹配。计数语义目前只在三层之外的 `kernel/cache` 被间接验证 |
| 污染跨包传播点 | `kernel/cache/l3.go` `L3Decider.judgeGrey` | 折叠块 `o.RulesReject += gs.RulesReject` 把 judge 故障并入 L3 的规则拒 |
| `judgeGrey` 有两个调用臂 | `kernel/cache/l3.go` `DecideL3` | `zone.Grey` 分支，以及 Issue #260 的词法支持降级（`zone.Hit` 但 lexical support < `LexicalFloor`）也会走 judge。`JudgeError` 因此不是「grey 专属」计数 |
| `Stats` 无 json tag，却已在 CLI 出网 | `cmd/semantix/verify.go` `verifySummary.Judge` | `Judge *judge.Stats \`json:"judge,omitempty"\`` —— 字段名以 PascalCase 直接进 `verify --json`；`NeedJudge` 今天就在泄漏，尽管无人读它 |
| `waste` 总数今天**包含** judge 故障 | `cmd/semantix/verify.go` `runVerify` | 表达式为 `JudgeReject+Fingerprint+RulesReject`，judge 故障当前藏在 `RulesReject` 里 |
| calibrate 运行时行是 9 列硬断言 | `cmd/semantix/calibrate_test.go` `TestCalibrateRuntimeOnly` | 字面量 `"1\t0\t0\t0\t0\t0\t0\t1\t100.0"` |
| 基线在本机不是绿的 | — | `go test ./cmd/semantix/...` 现有 17 个失败，全部为 `TempDir RemoveAll cleanup: unlinkat ...lib.db.journal: The process cannot access the file`（Windows 文件锁），非逻辑失败。见 §6.3 |

### 1.3 非目标

- **不修**指纹门的同类缺陷。`Chain` 的 `fingerprint.Verify` 出错与「指纹已变更」同样都记
  `Stats{Fingerprint: 1}`，结构上与本 issue 完全同型，但拆它会牵动 `Obs.FingerprintReject`
  与 `l3_fingerprint_reject` 线格式。**单独开 issue**，本期只在代码注释里留指针。
- 不改 judge 后端、不改重试策略、不改任何 fail-closed 行为。
- 不新增事件总线 Kind（沿用 #262 的 usage 日志通道）。

## 2. 语义定义（单一真源）

`JudgeError` = **judge 调用本身失败**（传输错误、超时、响应解析失败），即 judge 不可用。
它既不是 `JudgeReject`（judge 明确 approve=false），也不是 `RulesReject`（规则判定拒绝）。

拒绝分类在 #262 §2.1 表格上新增一行，五类**按原因互斥、不可合并**。

> 注意层级：下表是 `cache.Obs` 层的完整口径。`judge.Stats` 层只有其中四类
> （无 `IsolatedReject`——隔离判定发生在 `DecideL3`，不经 `RuleGate`；且
> `Obs.FingerprintReject` 在 `Stats` 里叫 `Fingerprint`）。两层的总数因此不同，
> `verify` 打印的 waste 是 `judge.Stats` 层的四类之和。

| 计数 | 语义 | 判定点 |
|---|---|---|
| `RulesReject` | clear miss，或 grey 无 judge 的保守拒绝 | `RuleGate.Check`；`DecideL3` 的 `zone.Miss` 臂 |
| `FingerprintReject` | 指纹门错误 / mtime\|sha256 变更 / `L3Safe=false` | `RuleGate.Chain` 指纹门 + `L3Decider.verified` |
| `IsolatedReject` | 上下文/model 隔离不匹配（#133） | `DecideL3` |
| `JudgeReject` | judge 判定拒绝（approve=false） | `RuleGate.Chain` judge 分支 |
| **`JudgeError`** | **judge 调用失败——不可用，不是判决** | **`RuleGate.Chain` judge 错误分支** |

新口径：**总拒绝数 = RulesReject + FingerprintReject + IsolatedReject + JudgeReject + JudgeError**。

### 2.1 三处已知重复计数（必须写进注释，禁止相加）

同一次 judge 故障会同时出现在三条互不独立的通道上：

1. `usage.Summary.JudgeFailClosed`（#242，经 `JudgeDecisions[].Verdict=="fail_closed"`）；
2. 本期的 `usage.Summary.L3JudgeError`（聚合计数）；
3. `Summary.JudgeSkipped` 与 `Obs.FingerprintReject` 之间存在同型影子关系
   （`judgeGrey` 的 `case !timed.called: obs.Verdict = JudgeSkipped` 恰在 `Chain`
   于指纹门短路时触发）。

这是 #262 有意的双通道设计（日志流 vs 计数账本），但两个字段的注释都必须写明
**指向同一物理事件、永不相加**。另注：`JudgeFailClosed` 仅在 `OnJudge` 接线时（gateway）
才有值，而 `L3JudgeError` 在 CLI 与 gateway 两条路径都有值。

## 3. 改动清单（按依赖序，锚定符号；行号一律不作数）

> 并行风险：`cmd/semantix/usage.go`、`cmd/semantix/verify.go`、`kernel/cache/l3.go`、
> `gateway/pipeline.go` 当前有另一路 worker 的未提交改动。**每一步开动前重读该符号**。

### 3.1 kernel/judge

1. **`Stats`** — 在 `JudgeReject` 后新增 `JudgeError int // judge call failed/timed out — unavailable, not a verdict`。同时补上 `JudgeApproved` 缺失的注释。
2. **`Stats.add`** — 新增 `s.JudgeError += o.JudgeError`。**这是唯一写入路径**，漏了则计数永远为零且无编译错误。
3. **`RuleGate.Chain`（judge 错误分支）** — `g.count(Stats{RulesReject: 1})` → `g.count(Stats{JudgeError: 1})`。**verdict、reason 字符串、返回的 err 全部逐字不动。**
4. **`RuleGate.Chain`（死代码）** — 删除 `if v != NeedJudge { return }` 与 `g.Judge.Confirm` 之间的整个 `if g.Judge == nil { ... }` 块，把解释性注释上移为一行不变式：
   `// v == NeedJudge implies g.Judge != nil (Check's only NeedJudge arm is guarded by it).`
5. **注释** — 在指纹门两处 `Stats{Fingerprint: 1}` 上方留一行指针，说明同型缺陷已单列 issue（§1.3）。

**测试**：`judge_test.go` 目前对 `Stats` 零断言。
- 改 `TestChainJudgeErrorConservative`：保留 Reject+err 断言，挂 `Stats` 并断言 `JudgeError==1` **且 `RulesReject==0`**（口径回归守卫）。
- `TestChainGreyNoJudgeRejects` 应**保持通过且不改**（它走的是 `Check` 产出的那条路，不是被删的块）；在断言处加注释点明是哪条路径产出该 reason。
- 新增至少一条直接断言 `Chain` 每个分支计数归属的表驱动测试——把语义钉在源头，而不是三层之外。

### 3.2 kernel/cache

6. **`Obs`** — 在 `JudgeReject` 后新增 `JudgeError int`。
7. **`ObsAccum.add`** — 新增 `a.n.JudgeError += o.JudgeError`。**强制**：`gateway/pipeline.go` 的 `withL3Obs` 只经 `ObsAccum.Snapshot` 读数，漏这一步会让整条 gateway→usage→CLI 链全零，而用 `OnDecide` 的 kernel 测试仍然绿——这是全链路最高风险的静默失败点。
8. **`L3Decider.judgeGrey`** — 在四行折叠块中 `o.JudgeReject += gs.JudgeReject` 之后加 `o.JudgeError += gs.JudgeError`。不动 `ok := err == nil && v == judge.Confirm`，不动 `OnJudge` 观测 switch（#242 是独立通道）。扩写 `judgeGrey` 文档注释，记录被**有意丢弃**的两个字段（`Confirmed` 与 `o.Reused` 冗余、`NeedJudge` 与 `o.Grey` 冗余），免得下一个人当 bug 修。

**测试**：`l3_test.go` 的 `wantObs` 用整结构 `got != want` 比较——新字段默认零值意味着**现有测试全绿也证明不了接对了**，必须新增显式断言。
- 新增 `TestL3DeciderObsCounters` 子测试「judge error counts judge error not rules reject」：`mockJudge{err: context.DeadlineExceeded}` → `Obs{Candidates:1, Grey:1, JudgeError:1}`（整结构比较顺带免费断言 `RulesReject==0`）。
- **新增第二条走词法门那条臂**：`zone.Hit` 但 lexical support 为 0 的候选 + 出错的 judge，同样断言 `Obs{JudgeError:1, Grey:1}`。两条臂都喂这个计数，spec 的语义才成立。
- 改 `TestDecideL3GreyZoneJudgeErrorFailsClosed`：挂 `ObsAccum` 并断言同样的计数，把 fail-closed 行为与计数钉在一处。
- 改 `TestL3ObsAccumSnapshotIsThreadSafe`：并发 `Obs` 与期望总数都加上 `JudgeError`。

### 3.3 kernel/usage

9. **`Event`** — 在 `L3JudgeReject` 后新增 `L3JudgeError int \`json:"l3_judge_error,omitempty"\``（保持 `omitempty`，与同组字段一致；additive，旧日志读作零）。注释写明与 #242 `fail_closed` 是同一事件的不同通道。
10. **`Summary`** — 新增 `L3JudgeError int`，注释交叉引用 `JudgeFailClosed` 并写明**禁止相加**。
11. **`Summarize`** — 新增 `s.L3JudgeError += e.L3JudgeError`。

**测试**：改 `TestSummarizeL3NegativeObservability`——加 `L3JudgeError:2` 并断言，**同时断言 `L3RulesReject` 保持原值**，这样把错误重新路由回 rules_reject 的实现会失败。

### 3.4 gateway

12. **`Gateway.withL3Obs`** — 在 `L3JudgeReject` 赋值后加 `e.L3JudgeError = o.JudgeError`。无签名变化。
    注意 `withL3Obs` 有**四个调用点**，其中包含 L3 命中那一条——所以一次 turn 可以同时带
    `l3_reuse=true` 与 `l3_judge_error>0`（候选①的 judge 出错、候选②被复用）。

**测试**：改 `gateway/false_hit_test.go` `TestWithL3Obs`。新增 `gateway/judge_observability_test.go` 用例，断言一次出错的 judge 调用**同时**产出 `l3_judge_error=1` 和一条 `Verdict=="fail_closed"` 的 `JudgeDecision`——把 §2.1 那个有意的双通道冗余钉住。

### 3.5 cmd/semantix

13. **`usageJSON`** — 新增 `L3JudgeError int \`json:"l3_judge_error"\``，**不带 omitempty**（该 envelope 有意始终输出零值，以区分「缓存安静」与「日志缺失」）。
14. **`runUsage`** — (a) JSON 装配体加 `L3JudgeError: s.L3JudgeError`；(b) 人读/TSV 块在 `l3_judge_reject` 行后插 `fmt.Fprintf(stdout, "l3_judge_error\t%d\n", s.L3JudgeError)`，保持 reject → error → approved 的分组。
15. **`feedEvolve`——本期无改动，但记录一条约束。**
    实测（`fac8e4c` 树）：`feedEvolve` 只发 `cache_hit` 一种信号，**不存在污染分子**；
    `inject_pollution` 由 harness 侧的 SliceReject 事件发出（见 `cmd/semantix/reject.go` 注释），
    不经这里。故 #245 在此无事可做。
    **给后续加污染信号的人的约束**：`L3JudgeError` **只可进分母，绝不可进 `inject_pollution` 分子**。
    judge 不可用不是「缓存被污染」的证据；若计入分子，上游 judge 故障会被读成污染从而收紧
    `TauL2`，在 judge 挂掉的时候反而缩小缓存——用第三方故障惩罚用户。
16. **`calibrateRuntime` / `runCalibrateRuntime`** — 新增 `L3JudgeError int \`json:"l3_judge_error"\``（无 omitempty，与同组一致）并在装配体赋值。
17. **`runCalibrate`** — 运行时 TSV 从 9 列扩到 10 列：header 在 `judge_reject` 后插 `judge_error`，格式串同位置加一个 `%d`，参数列表同位置加 `runtime.L3JudgeError`。**这是三条手工维护的平行列表，编译器不检查，列位必须对齐。**
18. **`runVerify`** — `# judge:` 行在 `judge_reject=%d` 后加 `judge_error=%d`；**并且**把 waste 表达式改成
    `jstats.JudgeReject+jstats.Fingerprint+jstats.RulesReject+jstats.JudgeError`。
    不加这一项，一个今天已被计入的公开数字会在「重构」的名义下悄悄变小。
19. **`verifySummary`（线格式决策）** — 不得让 PascalCase 的 `"JudgeError"` 键静默进入 `verify --json`。
    **采用方案 (b)**：在 `cmd/semantix` 内建一个本地 DTO `verifyJudgeJSON`，带 snake_case tag，从 `judge.Stats` 映射；`kernel/judge` 保持无 tag。
    依据是仓库自己的先例——`cmd/semantix/search.go` 写着 `json:"-" keeps search --json output byte-identical (backward compat)`，同类问题同类解法。顺带把今天已在泄漏的 `NeedJudge` 一并收进 DTO 决定（建议不导出，它无人读）。

**测试**：
- 改 `TestRunUsageJSON`（扩 Data 结构并断言 round-trip）、`TestRunUsageSummary`（`want` 加 `"l3_judge_error\t"`）。
- `feedEvolve` 本期无改动，故无新增测试；`TestFeedEvolveAdjustsTauAfterHistory` 须保持通过不变。
- 改 `TestCalibrateRuntimeOnly`：9 列字面量 → 10 列 `"1\t0\t0\t0\t0\t0\t0\t0\t1\t100.0"`，**并新增 header 含 `judge_error` 的断言**，防 header/row 漂移。改 `TestCalibrateJSONEnvelope`。
- 新增 `TestVerifyJudgeStatsLineIncludesJudgeError`：`verify_test.go` 今天对 judge 零断言（`grep -i judge` 无匹配），该行与 waste 总数完全无覆盖。
- 新增 `TestVerifyJSONJudgeBlockKeys`：反序列化为 `map[string]any` 比对精确键集，第一次把该 envelope 形状钉住。

### 3.6 文档同步

20. `docs/specs/issue-262-l3-negative-observability.md`：§2.1 拒绝分类表**新增 `JudgeError` 行**（该表自称「按原因不可合并」，这正是 #245 的全部论据）；`Event` 字段清单加 `l3_judge_error`；运行时 TSV 由 9 列改 10 列。
21. `docs/reports/verify-rubric.md`：waste++ 条目内联列举了 `judge.Stats` 字段，需补。
22. `docs/reports/issue-262-acceptance.md`：枚举 `Obs` 八字段与 `Event` 七字段的两行需补。

## 4. 不变式（实现必须保持）

1. **判决行为零变化**：judge 出错仍然 `Reject`，仍然返回原 err，reason 字符串逐字不变。
2. **零配置字节等价**：本期不引入任何配置键；除新增的一列/一键外，所有既有输出保持不变。
3. **计数互斥**：任何一次拒绝只增加五类计数中的**恰好一个**。
4. **waste 语义不变**：`verify` 打印的 waste 总数在改动前后对同一输入产出**同一个数**。

## 5. 分支与落地

`kernel/judge/judge.go` 与 `kernel/usage/usage.go` 目前干净；`cmd/semantix/usage.go`、
`cmd/semantix/verify.go`、`kernel/cache/l3.go`、`gateway/pipeline.go` 有并行 worker 的
未提交改动。建议顺序：先落 §3.1（kernel/judge，无争用），再落 §3.3，最后在 rebase
之后动 §3.2/3.4/3.5 那四个争用文件。

`gofmt` 说明：`gofmt -l kernel/ cmd/ gateway/` 会报 147 个文件，成因是 CRLF
（`core.autocrlf=true` 且无 `.gitattributes`，`git ls-files --eol` 显示工作区混合）。
`Stats` 结构体确实未对齐（在 LF 规范化副本上验证）。CI 无格式门禁，提交时会规范化，
所以**只重排这一个结构体是安全的；绝不要对全树跑 `gofmt -w`**。

## 6. 验收

### 6.1 复用现有门禁（不发明新的）

```bash
go vet ./...
go test ./... -race                      # CI 的字面 job，注意 -race
go test ./kernel/judge/ ./kernel/cache/ ./kernel/usage/ ./gateway/ -count=1
go test ./cmd/semantix/ -run "Calibrate|Eval|Usage|Verify" -count=1
```

后两条正是 `docs/reports/issue-262-acceptance.md` 为这条链固定下来的命令。

### 6.2 专项证据

1. **死代码已消失**：`go test -covermode=count ./kernel/judge/` 的覆盖率输出中不再有
   `judge.go:131.20,136.3` 这个零执行块。
2. **口径拆分生效**：构造一次 judge 超时，`semantix usage` 输出中
   `l3_judge_error=1` 且 `l3_rules_reject=0`。
3. **waste 不变**：同一输入下 `semantix verify` 打印的 waste 总数与改动前逐字相同。
4. **calibrate 列对齐**：`semantix calibrate --usage <log>` 的 header 与数据行列数一致（10 列）。

### 6.3 验收环境说明

Windows 上 `cmd/semantix` 的测试会**间歇性**出现
`TempDir RemoveAll cleanup: unlinkat ...db.journal: The process cannot access the file`
——slice store journal 的文件锁，在**并发跑多个 go test 进程**时出现，测试体本身通过、
失败发生在 `t.TempDir()` 的清理阶段。

实测口径（改动落地后，单进程串行）：`go test ./...` **退出码 0，零 FAIL**。
所以这不是恒定的基线失败，不能拿来当「已知红」搪塞。

规则：验收必须**单进程**跑；若出现上述 cleanup 失败，先确认没有其他 go test 进程在跑，
再复跑一次；仍然失败才算真失败。有条件时以 Linux/CI 结果为准。
