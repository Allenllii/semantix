# Issue #262 验收报告 — L3 负向观测接线(judge 拒绝/误命中统计 + 校准报告)

> 验收日期:2026-08-21 · 实现与验收:Reasonix agent · 对应 spec:`docs/specs/issue-262-l3-negative-observability.md`
> 基线:当前 main(55240b1)。分支 `feat/issue-262-l3-negative-observability` 经 rebase 同步至 55240b1(含 #260 词法门/#267 负向反馈/#269 intent-tier/#270 注入经济学/#292 usage 面板等 53 个上游提交),7 个冲突文件三方合并(双方面共存),冲突解决后 `go build ./...` + `go vet ./kernel/... ./cmd/... ./gateway/...` 干净。

## 验收标准逐条核对

| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
| c1 | 运行时负向统计可查询:`usage --json` 输出 L3 负向聚合 | ✅ | `usageJSON` 新增 7 字段;`TestSummarizeL3NegativeObservability` + `TestUsageJSONEnvelope` 通过 |
| c2 | 拒绝分原因可见:规则/指纹/隔离/judge 四类互不混淆 | ✅ | `L3Decider.Obs` 五组单测覆盖复用/指纹修改/上下文隔离/无 judge grey/judge 拒绝/批准路径,`wantObs` 全字段断言(零期望有意义) |
| c3 | 误命中反馈回路闭环:同 session 重试 → 绕过 L3 走上游 + `l3_false_hit=true` | ✅ | `TestE2EFalseHitRetryBypassesL3`:`x-semantix-cache=miss`、upstream calls 0→1、`usage.Summarize` L3FalseHits=1;`TestFalseHitRetryDetection` 覆盖不同 session/低相似不触发、记录一次消费;`TestFalseHitDetectionDisabled`(-1 关闭) |
| c4 | 校准报告完整:混淆矩阵 + ε_fa/ε_fr + precision/recall/F1 + Δerr_upper;门禁 exit 3;JSON 信封 | ✅ | `TestCalibrateConfusionMatrix`(tp=3 fp=3 tn=0 fn=0、50.0/50.0/0.0/0.500/1.000/0.667)、`TestCalibrateGateExitThree`、`TestCalibrateJSONEnvelope`(ok/command/data 分栏)、`TestCalibrateJSONGateEnvelope`(error.code=3) |
| c5 | 运行时汇总与离线分栏:误命中率复用数为 0 时 N/A;offline/runtime 不混算 | ✅ | `TestCalibrateRuntimeOnly`(1 复用 1 误命中 → 100.0)、`TestCalibrateUsageMissingIsNA`、JSON 中 `audit`/`runtime` 两对象;手动运行 `calibrate --audit + --usage` 输出分栏正确 |
| c6 | 兼容性:usage.Event 仅 additive;旧日志可读;vet 干净 | ✅ | `TestSummarizeL3NegativeObservability` 含 legacy 行(无新字段)读作零;旧字段未删;`go vet ./kernel/... ./cmd/... ./gateway/...` 干净 |
| c7 | 分桶校准与阈值失配可见:`verify --calibrate` 输出分桶命中分布 + 三段占比漂移;调坏阈值后漂移非零;`--labels` 输出每桶 fp/precision | ✅ | `TestVerifyCalibrateReportStructure`、`TestVerifyCalibrateDetectsMistunedThreshold`(`--abs-high 7` → `grey +50.0pt / hit -50.0pt`)、`TestVerifyCalibrateLabelsPrecision`(fp=1 precision=0.0%)、`TestVerifyCalibrateJSONEnvelope`(bins/current/default/labeled) |

## 交付清单

| 文件 | 变更 |
|---|---|
| `docs/specs/issue-262-l3-negative-observability.md` | 新增 spec v1:负向观测语义(拒绝四分类 + 误命中双口径)、usage.Event additive 契约、calibrate 契约、verify 分桶校准 §3.4、evolve 负向信号 §3.5、c1-c8 |
| `kernel/cache/l3.go` | `Obs`(纯值快照)+ `ObsAccum`(线程安全累计)+ `OnDecide`(per-call 回调);`DecideL3` 全路径计数;Candidates/Grey/RulesReject/FingerprintReject/IsolatedReject/JudgeReject/JudgeError/JudgeApproved/Reused;`judgeGrey` 挂局部 `judge.Stats` 合并 |
| `kernel/usage/usage.go` | `Event` 新增 7 个 additive 字段(L3GreyCandidates/L3JudgeReject/L3JudgeApproved/L3RulesReject/L3FingerprintReject/L3IsolatedReject/L3FalseHit);`Summary` 对应聚合 |
| `gateway/config.go` | `[cache] false_hit_sim`(默认 0.6,-1 关闭,validate 校验 NaN/Inf/越界) |
| `gateway/gateway.go` | `l3Reuses map` + `reuseMu`(有界 1024 LRU);`New()` 初始化 |
| `gateway/pipeline.go` | 误命中检测(同 session 重试 → 绕过 L3 走上游)→ `decideL3` per-request 副本捕获 Obs delta → 3 个下游函数携带负向字段写 usage;`withL3Obs` 映射 |
| `gateway/levenshtein.go` | 归一化编辑距离(rune 级,DP 上界 2048 防 CPU 滥用) |
| `gateway/anthropic.go` | `streamThroughAnthropic` 携带负向字段 |
| `cmd/semantix/calibrate.go` | 新命令:offline(oracle 混淆矩阵/consistency/ε_fa/ε_fr/precision/recall/F1/Δerr_upper)+ runtime(usage 汇总/误命中率)分栏;`--min-consistency` 门禁 exit 3;`--json` 信封;退出码 0/1/2/3 |
| `cmd/semantix/main.go` | 注册 `calibrate` 命令(groupKernelOps);verify completionFlags 补 `--calibrate/--labels` |
| `cmd/semantix/verify.go` | `--calibrate` 分桶校准报告(相对置信度 r=top2/top1 十桶分布 + 当前 vs 默认三段占比漂移)+ `--labels` oracle 标记(fp/precision,P-CHR 简化版);runVerify 关闭 store 句柄(修复 verify 测试 Windows journal 泄漏) |
| `cmd/semantix/usage.go` | `feedEvolve` 补 `inject_pollution` 负向信号(judge 拒绝/误命中 → polEWMA,与 #267 同口径),TauL2 EWMA 双向证据 |
| `cmd/semantix/usage.go` | `usageJSON` + 文本输出补负向聚合 |
| 测试 | `kernel/cache/l3_test.go`(+3 组)、`kernel/usage/usage_test.go`(+1)、`gateway/false_hit_test.go`(+6)、`gateway/e2e_test.go`(+1 端到端)、`gateway/config_test.go`(+1)、`cmd/semantix/calibrate_test.go`(+8)、`cmd/semantix/verify_test.go`(+5 校准)、`cmd/semantix/usage_test.go`(+2 evolve 负向) |
| `README.zh-CN.md` | 命令枚举补 `calibrate` |

## 验证

- `go vet ./kernel/... ./cmd/... ./gateway/...` 干净;
- `go test ./kernel/cache/ ./kernel/usage/ ./gateway/ ./kernel/evolve/ ./kernel/judge/` 全绿(含新增全部用例);
- `go test ./cmd/semantix/ -run "Calibrate|Eval|Usage|Verify"` 全绿(calibrate 8 用例、verify 校准 5 用例、evolve 负向 2 用例、既有 usage/eval 全数);
- 手动:`verify --calibrate` 输出分桶分布 + 漂移块(默认阈值 delta=0;`--abs-high 7` → `grey +50.0pt / hit -50.0pt`);`verify --labels` 输出 fp=1 precision=0.0%;`usage --evolve-db` 污染日志收紧 TauL2(0.600)、干净日志放松(0.500);
- rebase 冲突解决后基线对照:55240b1 上 `TestVerifyJSONEnvelope` 同样因 `.journal` cleanup 失败,确认平台问题与 #262 无关。

### 平台既有失败(与 #262 无关,已用基线 55240b1 对照确认)

- `kernel/slice` 与 `cmd/semantix` 的 `.journal` 文件句柄未释放导致的
  `TempDir RemoveAll cleanup` 失败(测试断言本身通过);`TestFileStoreKeepsPerm0600`
  已在 issue-07 报告记录为 Windows 平台既有失败。本轮顺带修复了 `runVerify`
  的 store 句柄泄漏(verify 系列测试已全绿);其余命令(search/lookup/dashboard/
  extract 等 16 个)与 `kernel/slice`(TestJournalForwardCompat 等)仍为平台
  既有失败,基线 55240b1 同样失败。race 需 gcc,本机无,按项目惯例留合并环境复验。

## 遗留(不阻塞 #262,归后续 issue)

1. harness/Reasonix 桥接侧的用户编辑/回滚反馈信号(误命中的强信号源,SliceReject 发布者仍缺);
2. 真实 300 对 judge 一致性实测(待 #20/#58 真实数据,沿用 #8 遗留);
3. `semantix calibrate` 的真实 usage 日志回归样本(当前测试用合成事件)。
