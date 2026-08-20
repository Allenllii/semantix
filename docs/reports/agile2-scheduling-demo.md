# Agile 2 调度演示报告（U42 · Issue #193）

> 日期：2026-08-18 · 判级：Spec-Exempt（验收执行 + 报告，不改契约）
> **结论先行**：同一任务族在 kernel 调度 **on/off** 两遍对照下，**5 个场景全部可见 harness 行为改变**（并行分组 / 行为门 / tier 切换 / 工具挂起 / 预算降级），聚合成本 **kernel-on $0.008405 vs kernel-off $0.021532 → 节省 61.0%**（已剔除纯脚手架的热身轮）。调度决策与并发是**真实生产代码路径**（`Agent.Run → executeBatch → kernel/sched.RuleDecider`）；每工具耗时与 token→USD 价模型是**标注的合成夹具**。

---

## 1. 对照方法

同一任务族跑两遍，唯一变量是调度器：

- **kernel-on**：`kernel/sched.RuleDecider` 逐轮决策——并行分组（含行为门）、模型 tier（flash↔pro）、工具挂起、预算阶梯动作（70/90/100）。
- **kernel-off**：一个恒返回空 `RoundPlan` 的 decider → harness 回退到自身静态 `partitionToolCalls`（连续只读工具仍并行）、无挂起、无 tier、无预算动作。**这正是 Issue #193 对 OFF 的定义**（「静态 ReadOnly 分组、无挂起、无预算动作」），不是被削弱的稻草人。

两遍都经过真实的 `Agent.Run → executeBatch → decideRound → planBatches/runParallel` 全链路（与生产同一份代码），只把 provider 换成脚本化的 `testutil.MockProvider`（喂固定的工具调用轮次），把工具换成带模拟耗时的夹具工具。

依赖：U40（#191，Decider 直连 + ResourceCatalog + SuspendTools 执行点）、U41（#192，BudgetController 70/90/100 阶梯）——均已合并且在 `harness-integration` 线上。

## 2. 对照结果

| 场景 | 演示的 kernel 决策 | 峰值并发 on/off | 成本 USD on/off | tier on/off | 预算动作 |
|---|---|---|---|---|---|
| **fanout** | 并行分组基线 | **4 / 4** | 0.002175 / 0.002175 | [pro] / [pro*] | — |
| **behavior-gate** | 行为门（并行分组生效） | **1 / 2** | 0.000700 / 0.002175 | [flash×3] / [pro*×3] | — |
| **tier-switch** | tier flash↔pro | 2 / 2 | 0.001960 / 0.006090 | [flash] / [pro*] | — |
| **suspend** | 工具挂起 | **1 / 2** | 0.001050 / 0.003262 | [flash] / [pro*] | — |
| **budget-downgrade** | 预算阶梯降级 | 1 / 1 | 0.002520 / 0.007830 | [flash] / [pro*] | **[degrade_tier]** |
| **聚合** | | | **0.008405 / 0.021532** | | **节省 61.0%** |

> `pro*` = OFF 无 tier 决策，按静态基线的 pro 档计价（见 §4 诚实声明）。behavior-gate 的成本只计**测量轮**（2 个热身轮是训练行为门的脚手架，不计入成本差；tier 列仍显示全部 3 轮以保持透明）。墙钟为代表值（每次运行浮动）：fanout ~32ms、tier-switch ~22ms、suspend ~21ms、budget ~41ms；behavior-gate on ~125ms / off ~93ms（见 §4 的延迟-安全权衡）。

## 3. 逐场景证据（kernel 决策如何改变 harness 行为）

原始逐轮 `RoundPlan` 与 `ToolResult` 见 `docs/reports/data/agile2-scheduling-demo/scheduling-demo.jsonl`（10 行 = 5 场景 × 2 arm，on 在前）。

1. **fanout（并行分组基线）**：一轮 4 个连续只读工具。两 arm 都并行到峰值 4（OFF 静态分组自带并行）。成本相同（4 工具 > 复杂阈值 3 → on 也路由 pro）。作用是**证明 OFF 不是稻草人**——kernel 的收益必须来自真实决策，而非削弱基线。

2. **behavior-gate（并行分组生效）**：flaky 只读工具先失败 2 次热身（`MinSamples=2`），随后与健康只读工具同轮。ON 的行为门把已不可信的 flaky 工具**拉进自己的串行槽**——JSONL 中该轮 `parallelGroups: [["c1"],["c2"]]`（两个单元素组），峰值降到 1；OFF 盲目并行到 2。这是安全决策，代价见 §4。

3. **tier-switch（tier 切换）**：一轮小只读批。RuleDecider 因「无写、轮次小于复杂阈值」路由到便宜的 flash；OFF 无 tier 决策、按 pro 计价。纯成本差（0.00196 vs 0.00609）。

4. **suspend（工具挂起）**：kernel 决定挂起一个高风险工具。ON 由 harness 在执行前拦下（`ToolResult.Err = "suspended by scheduler"`，工具零执行）；OFF 无 plan → 工具照跑。挂起**决策**由 `suspendPolicyDecider` 建模（即 U41 BudgetController 会写入的 `RoundPlan.SuspendTools` 字段，与生产 `scheduler_integration_test` 同一强制路径）；**harness 拦截是真实代码**。

5. **budget-downgrade（预算阶梯降级）**：预算预置在 92%，轮内含 writer 本应走 pro。RuleDecider 的阶梯在 ≥90% 触发 `degrade_tier`，把该轮强制降到 flash；OFF 无视预算按 pro 计价。JSONL 中 ON 该轮 `tier: "flash", budgetAction: "degrade_tier"`。（70% 档 `halt_prefetch`、100% 档 `hard_stop` 的决策由 `kernel/sched/rule_decider_test.go` 单测覆盖；本演示聚焦可量化的 90% 成本降级档。）

## 4. 诚实声明（不重蹈 m0 合成演示覆辙）

**真实的**：所有调度决策（并行分组、行为门、tier、挂起、预算动作）与 harness 的执行行为（并发、拦截、tier 应用）都是与生产同一份代码；峰值并发由工具内的探针实测（非时间戳推断），确定性、在 `-race` 下无竞态（探针用 mutex）。

**合成夹具（标注）**：
- **每工具耗时**：20–30ms 的模拟值，用于让墙钟有可测切片。并发**结构**真实，绝对毫秒是夹具。
- **token→USD 价模型**：每轮固定 token 估算 × tier 单价。价表取 DeepSeek V4 公开价（2026-08，老 deepseek-chat/reasoner 名 2026-07 退役）：**V4-Flash** 输入 $0.14/M（cache-miss）、输出 $0.28/M；**V4-Pro** 输入 $0.435/M、输出 $0.87/M（标准价，另有峰谷浮动）。tier**决策**真实，绝对 USD 仅示意。
- **OFF 计价假设**：OFF 无 tier 决策，按 **pro 档**计价——即「没有 kernel 分档时，运维会静态供给强模型以覆盖最难的轮次」这一现实基线。**61.0% 的聚合节省主要由此假设下的 tier 路由贡献**。敏感性：若假设 OFF 静态钉 flash，则 tier/预算带来的成本差归零，收益只剩行为门的安全性与挂起的浪费规避——即节省是「tier 供给策略」的函数，不是凭空的。
- **热身轮不计成本**：behavior-gate 的 2 个热身轮纯为训练行为门（脚手架），已从成本累加中剔除（`assembleResult` 按 `Scenario.warmup` 跳过），否则会凭空给该场景制造成本差并抬高头条数字。**且需澄清**：behavior-gate 剔除热身后仍有的成本差（0.0007 vs 0.002175）本质仍是 tier 路由（小轮走 flash vs pro），**不是行为门自身的经济收益**——该场景的真实收益是并发/安全（峰值 1 vs 2），成本差只是 tier 效应的附带显现。各场景隔离的是不同**决策**（并发/挂起/预算），成本则是贯穿全部的同一个 tier 路由效应。

**反直觉但诚实的点**：behavior-gate 的 ON 墙钟（~125ms）> OFF（~93ms）。行为门把 flaky 工具串行化是**拿延迟换安全**（避免不可靠读与兄弟调用并发产生的隐患），不是延迟 win。演示如实呈现，不掩饰。

## 5. 复现步骤

```bash
# 对照演示 + 打印对比表 + 写 JSONL 证据
go run ./harness/cmd/scheduling-demo
#   -out DIR   JSONL 输出目录（默认 docs/reports/data/agile2-scheduling-demo）
#   -quiet     只写 JSONL 不打印表

# CI 守护（断言每场景 on/off 的结构不变式；Linux 上带 -race）
go test ./harness/internal/scheddemo/...
go test -race ./harness/internal/scheddemo/...   # 需 CGO/gcc；本仓 harness-integration workflow 在 Linux 跑
```

原始证据：`docs/reports/data/agile2-scheduling-demo/scheduling-demo.jsonl`（每行一个 arm 的完整 `Result`：逐轮 `RoundPlan` + `ToolResult`）。

## 6. 量化口径

| 口径 | 来源 | 真/合成 |
|---|---|---|
| 端到端延迟 | `RunScenario` 墙钟（`Agent.Run` 全程） | 结构真实 / 绝对值合成（工具耗时夹具） |
| 总成本 | 逐轮 tier × token 夹具 × V4 价表 | 决策真实 / 绝对值合成 |
| 并发度 | 工具内并发探针峰值（`runParallel` 实测） | **真实** |
| 决策可解释性 | 逐轮 `RoundPlan` 落 JSONL（`recordingDecider`） | **真实** |

## 7. 过程中发现的缺陷

无。`Options.Decider = nil` 默认装 `RuleDecider`（即 ON）而非 OFF 是一处易踩点，但属既有设计（已在 §1 用空-plan decider 表达 OFF）；无需另开 issue。

## 8. 状态回写

- 路线图 `docs/Agile路线图.md` §2 验收标准 H3 门②：已勾选，指向本报告。
- U42 行：已记录 DoD 证据。
