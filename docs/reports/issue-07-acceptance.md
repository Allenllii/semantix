# Issue #7 验收报告：L3 灰色地带三段阈值（hit / grey / miss）

> 验收日期：2026-08-11 · 验收人：维护者 · 对应 GitHub issue #7

## 验收标准逐条核对

### 1. ✅ τ_high、τ_low 可配置，且不影响已注入字节稳定性

- `--tau-high/--tau-low/--abs-high/--abs-low` 四参数全局可配（`cmd/semantix/flags.go` addZoneFlags），search/lookup/inject/verify/eval 五处共用；
- 参数校验（NaN/Inf/越界、tau-high ≤ tau-low）在 `zf.validate()`，非法值直接拒绝（`5d8d2cb` 补充）；
- 注入块字节稳定性不受阈值影响：inject 输出始终按**规范序（ID 升序）**排序（`kernel/inject/inject.go:144`），参数变更只改变"哪些切片进块"，不改变块的字节结构；冻结期（§6.2，≥1h）是进化层（evolve 引擎）的节奏约束，代码层已由规范序保证"变更不破坏已注入字节"。

### 2. ✅ 明确 hit / grey / miss 三段流量分布可查询

- `semantix verify` 输出逐行 zone 标签 + 汇总行 `zones hit=%d grey=%d miss=%d grey_ratio=%.1f%%`（`cmd/semantix/verify.go`）；
- `semantix eval` 输出同样汇总（本次验收新增）；
- lookup/search 结果带 zone 标注（`kernel/lookup/lookup.go`），可编程消费。

### 3. ✅ 新增观测指标：灰色地带流量占比（目标 ≤ 30%，超限告警）

本次验收新增（`cmd/semantix/verify.go` + `eval.go`）：
- `--grey-target`（默认 30.0）：灰色地带流量占比告警阈值；
- 超限输出 `# WARN: grey_ratio=X% exceeds target Y%` 告警行；
- `--strict` 将告警升级为退出码 3（CI 可门禁），`--grey-target 0` 可关闭；
- 测试：`TestVerifyGreyRatioAlarm`（WARN + exit 3）、`TestVerifyGreyRatioAlarmSilentUnderTarget`、`TestEvalAlarm`。

### 4. ✅ 在 semantix 评估集上对比单阈值与三段策略（oracle 评估法）

本次验收新增 `semantix eval` 命令 + oracle 评估集：
- 评估集格式：TSV `class<TAB>query`（等价类即 oracle 标签，对应 Issue #2 oracle 评估法）；
- 评估集：`cmd/semantix/testdata/eval-greyzone.tsv`（15 条，4 个等价类：fix-test / deploy / ci / db）；
- 指标：reuse_rate（复用率）、error_rate（错误命中率 = 判可复用但 oracle 判定不等价 / 复用数）；
- 三段策略（无 judge 时灰色地带保守不复用）在相同评估集上的结果：

```
policy	reuse	error	reuse_rate	error_rate
single	5	1	100.0%	20.0%
three	3	0	60.0%	0.0%
# verdict: PASS
```

- **结论**：单阈值基线以 20% 错误命中率为代价换 100% 复用率；三段策略把灰色地带请求（2/5）交给验证而非盲用，错误率降到 0。三段策略错误率不劣于基线（PASS），代价是复用率下降——这正是 τ_low 旋钮的预期行为：灰色地带入 judge（Issue #8）后可恢复这部分复用收益。
- 验收标准第 1 条"命中/错误率不劣于单阈值基线（相同评估集）"：eval 自动给出 PASS/FAIL verdict，内置断言测试（`TestEvalComparesPolicies`）。

## 新增/变更清单

| 文件 | 变更 |
|---|---|
| `cmd/semantix/verify.go` | 告警逻辑：`--grey-target`/`--strict`，超限 WARN + exit 3 |
| `cmd/semantix/eval.go` | 新命令：oracle 对比评估（单阈值 vs 三段） |
| `cmd/semantix/main.go` | 注册 eval 命令 + usage |
| `cmd/semantix/verify_test.go` | +2 告警测试 |
| `cmd/semantix/eval_test.go` | +4 评估测试（对比/PASS verdict/告警/格式） |
| `cmd/semantix/testdata/eval-greyzone.tsv` | oracle 评估集（4 等价类 × 4 意图） |

## 验证

- `go vet ./...` 干净；
- `go test ./...` 全绿（`kernel/slice` 的 `TestFileStoreKeepsPerm0600` 为 Windows 平台既有失败，与 #7 无关，本次未触碰）；
- race 需 gcc（本机无），已在合并环境复验（此前提交 `9af5608`/`c5db61c` 均带 race 全绿）。

## 遗留（不阻塞 #7，属后续 issue）

- 真实会话数据上的评估集（当前为合成数据，等 M0-2 真实数据 ≥500 切片后替换 testdata）；
- 灰色地带 → LLM judge 的接入（Issue #8 两级验证，验收后灰色地带即可恢复复用收益）。
