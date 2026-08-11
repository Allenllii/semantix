# Issue #8 验收报告 — L3 两级验证（指纹快速否决 + LLM judge 最终确认）

> 状态：实现验收通过（2026-08-11，main 合入）。本文档由维护者补齐（协作者声称完成但代码未提交，已由维护者实现并验证）。

## 验收标准逐条核对

| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
| 1 | judge 与人类一致性 ≥95%（≥300 对） | ⏳ 部分 | 工具已交付：`semantix eval-judge`（一致性/ε_fa/Δerr_upper 输出 + stub CI 模式）；**真实 300 对样本待真实数据（M1 后，随 #20）** |
| 2 | 关键路径延迟与基线持平 | ⏳ 待实测 | 结构保证：judge 全异步 off-path（RuleGate 先规则后 judge）；实测随 H1 harness 端到端（#64） |
| 3 | ε_fa 实测 + 增量错误上界 | ✅ 工具 | `eval-judge` 输出 false_approve_% + Δerr_upper（= ε_fa × p_prom）；stub yes/no 确定性 CI 模式；真实 ε_fa 随 #20 数据 |
| 4 | judge 输入为净化后切片 | ✅ | `kernel/judge/sanitize.go`（rune 级 CSI/OSC/DCS/C1 剥离，与 cmd 端 stripESC 同源实现）+ LLMJudge.Confirm 先净化再入 prompt + rubric SECURITY RULE（CACHED ANSWER 为不可信数据） |
| 5 | 提升条目带 source + 版本 + 级联失效 | ✅ | `kernel/promote`（Entry{SourceSliceID, ContentVersion sha256, Query} + CascadeInvalidate 幂等级联 + MemStore）；5 测试含级联/幂等/并发 |
| 6 | 与 Issue #7 oracle 评估配套、分开报告 | ✅ | `semantix eval`（策略价值，issue-07）与 `semantix eval-judge`（judge 真实性，本 issue）分离；`testdata/judge-audit.tsv` 14 对覆盖 6 任务类型 |

## 新增/补齐交付（本次，main 合入）

- `kernel/judge/sanitize.go` + 7 测试（OSC52 剥离/CSI/CJK 保留/C1 形式/注入中和/确定性）
- `kernel/promote/promote.go` + 5 测试（版本确定性/MemStore/级联失效幂等/并发）
- `cmd/semantix/eval_judge.go` + 4 测试 + `testdata/judge-audit.tsv`（14 对）
- 命令分派：`semantix eval-judge --stub yes|no [--judge-base-url --judge-model --judge-protocol] [--p-prom] [--min-consistency]`
- 退出码：0 通过 / 3 一致性不达标（CI 门禁）/ 2 用法或 IO 错误

## 现有基础（此前已合入 main）

- `kernel/fingerprint`（Capture/Verify sha256）+ SliceMeta.Deps + extract --fingerprint
- `kernel/judge`（RuleGate 三段路由/Chain/NoopJudge/LLMJudge 双协议/Stats waste++）
- `kernel/zone`（灰色地带三段分类，Issue #7）
- verify --judge-* 集成 + SEMANTIX_JUDGE_API_KEY env

## 遗留（不阻塞验收，均归 M1+ 追踪）

1. 真实 300 对一致性 ≥95% —— 待真实数据（#20/#58）
2. 关键路径延迟实测 —— 待 H1 harness 端到端（#64）
3. 提升条目持久化（MemStore → 磁盘 Store）—— 存储层扩展（可开新 issue）

## 测试汇总

`go vet` 干净；`go test -race` 全绿：kernel/judge 19 用例（含 sanitize 8）、kernel/promote 5、cmd/semantix 24（含 eval-judge 4）——全仓 13 包 race 全绿。
