# Issue #262 验收报告 — L3 负向观测接线(judge 拒绝/误命中统计 + 校准报告)

> 验收日期:2026-08-21 · 实现与验收:Reasonix agent · 对应 spec:`docs/specs/issue-262-l3-negative-observability.md`
> 基线:当前 main(28aec74)。工作区含未跟踪文件(.reasonix/、tmp/、wtbase/、deploy/data/ 等),本轮未触碰。

## 验收标准逐条核对

| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
| c1 | 运行时负向统计可查询:`usage --json` 输出 L3 负向聚合 | ✅ | `usageJSON` 新增 7 字段;`TestSummarizeL3NegativeObservability` + `TestUsageJSONEnvelope` 通过 |
| c2 | 拒绝分原因可见:规则/指纹/隔离/judge 四类互不混淆 | ✅ | `L3Decider.Obs` 五组单测覆盖复用/指纹修改/上下文隔离/无 judge grey/judge 拒绝/批准路径,`wantObs` 全字段断言(零期望有意义) |
| c3 | 误命中反馈回路闭环:同 session 重试 → 绕过 L3 走上游 + `l3_false_hit=true` | ✅ | `TestE2EFalseHitRetryBypassesL3`:`x-semantix-cache=miss`、upstream calls 0→1、`usage.Summarize` L3FalseHits=1;`TestFalseHitRetryDetection` 覆盖不同 session/低相似不触发、记录一次消费;`TestFalseHitDetectionDisabled`(-1 关闭) |
| c4 | 校准报告完整:混淆矩阵 + ε_fa/ε_fr + precision/recall/F1 + Δerr_upper;门禁 exit 3;JSON 信封 | ✅ | `TestCalibrateConfusionMatrix`(tp=3 fp=3 tn=0 fn=0、50.0/50.0/0.0/0.500/1.000/0.667)、`TestCalibrateGateExitThree`、`TestCalibrateJSONEnvelope`(ok/command/data 分栏)、`TestCalibrateJSONGateEnvelope`(error.code=3) |
| c5 | 运行时汇总与离线分栏:误命中率复用数为 0 时 N/A;offline/runtime 不混算 | ✅ | `TestCalibrateRuntimeOnly`(1 复用 1 误命中 → 100.0)、`TestCalibrateUsageMissingIsNA`、JSON 中 `audit`/`runtime` 两对象;手动运行 `calibrate --audit + --usage` 输出分栏正确 |
| c6 | 兼容性:usage.Event 仅 additive;旧日志可读;vet 干净 | ✅ | `TestSummarizeL3NegativeObservability` 含 legacy 行(无新字段)读作零;旧字段未删;`go vet ./kernel/... ./cmd/... ./gateway/...` 干净 |

## 交付清单

| 文件 | 变更 |
|---|---|
| `docs/specs/issue-262-l3-negative-observability.md` | 新增 spec v1:负向观测语义(拒绝四分类 + 误命中双口径)、usage.Event additive 契约、calibrate 契约、c1-c6 |
| `kernel/cache/l3.go` | `Obs`(纯值快照)+ `ObsAccum`(线程安全累计)+ `OnDecide`(per-call 回调);`DecideL3` 全路径计数;Candidates/Grey/RulesReject/FingerprintReject/IsolatedReject/JudgeReject/JudgeError/JudgeApproved/Reused;`judgeGrey` 挂局部 `judge.Stats` 合并 |
| `kernel/usage/usage.go` | `Event` 新增 7 个 additive 字段(L3GreyCandidates/L3JudgeReject/L3JudgeApproved/L3RulesReject/L3FingerprintReject/L3IsolatedReject/L3FalseHit);`Summary` 对应聚合 |
| `gateway/config.go` | `[cache] false_hit_sim`(默认 0.6,-1 关闭,validate 校验 NaN/Inf/越界) |
| `gateway/gateway.go` | `l3Reuses map` + `reuseMu`(有界 1024 LRU);`New()` 初始化 |
| `gateway/pipeline.go` | 误命中检测(同 session 重试 → 绕过 L3 走上游)→ `decideL3` per-request 副本捕获 Obs delta → 3 个下游函数携带负向字段写 usage;`withL3Obs` 映射 |
| `gateway/levenshtein.go` | 归一化编辑距离(rune 级,DP 上界 2048 防 CPU 滥用) |
| `gateway/anthropic.go` | `streamThroughAnthropic` 携带负向字段 |
| `cmd/semantix/calibrate.go` | 新命令:offline(oracle 混淆矩阵/consistency/ε_fa/ε_fr/precision/recall/F1/Δerr_upper)+ runtime(usage 汇总/误命中率)分栏;`--min-consistency` 门禁 exit 3;`--json` 信封;退出码 0/1/2/3 |
| `cmd/semantix/main.go` | 注册 `calibrate` 命令(groupKernelOps) |
| `cmd/semantix/usage.go` | `usageJSON` + 文本输出补负向聚合 |
| 测试 | `kernel/cache/l3_test.go`(+3 组)、`kernel/usage/usage_test.go`(+1)、`gateway/false_hit_test.go`(+6)、`gateway/e2e_test.go`(+1 端到端)、`gateway/config_test.go`(+1)、`cmd/semantix/calibrate_test.go`(+8) |
| `README.zh-CN.md` | 命令枚举补 `calibrate` |

## 验证

- `go vet ./kernel/... ./cmd/... ./gateway/...` 干净;
- `go test ./kernel/cache/ ./kernel/usage/ ./gateway/` 全绿(含新增全部用例);
- `go test ./cmd/semantix/` 中 calibrate/usage/eval/eval-judge/help 相关全绿;
- 手动:`go run . calibrate --audit testdata/judge-audit.tsv --stub yes --usage <log>` 输出 offline/runtime 分栏,`false_hit_rate=100.0`。

### 平台既有失败(与 #262 无关,已用 stash 基线确认)

- `kernel/slice` 与 `cmd/semantix` 的 51 个失败全部为 Windows 上
  `.journal` 文件句柄未释放导致的 `TempDir RemoveAll cleanup` 失败(测试断言
  本身通过);`TestFileStoreKeepsPerm0600` 已在 issue-07 报告记录为 Windows
  平台既有失败。stash 掉本轮改动后 `TestVerifyJSONEnvelope` 同样失败,确认
  与本次变更无关。race 需 gcc,本机无,按项目惯例留合并环境复验。

## 遗留(不阻塞 #262,归后续 issue)

1. harness/Reasonix 桥接侧的用户编辑/回滚反馈信号(误命中的强信号源,SliceReject 发布者仍缺);
2. 真实 300 对 judge 一致性实测(待 #20/#58 真实数据,沿用 #8 遗留);
3. `semantix calibrate` 的真实 usage 日志回归样本(当前测试用合成事件)。
