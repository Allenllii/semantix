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

| 场景 | 演示的 kernel 决策 | 峰值并发 on/off | 墙钟 ms on/off | 成本 USD on/off | tier 决策 on/off | tier **已应用** | 预算动作 |
|---|---|---|---|---|---|---|---|
| **fanout** | 并行分组基线 | **4 / 4** | 31.4 / 32.0 | 0.002175 / 0.002175 | [pro] / [pro*] | —（已在 pro） | — |
| **behavior-gate** | 行为门（并行分组生效） | **1 / 2** | 123.3 / 92.6 | 0.000700 / 0.002175 | [flash×3] / [pro*×3] | flash | — |
| **tier-switch** | tier flash↔pro | 2 / 2 | 22.2 / 20.1 | 0.001960 / 0.006090 | [flash] / [pro*] | flash | — |
| **suspend** | 工具挂起 | **1 / 2** | 21.2 / 21.6 | 0.001050 / 0.003262 | [flash] / [pro*] | flash | — |
| **budget-downgrade** | 预算阶梯降级 | 1 / 1 | 40.7 / 40.7 | 0.002520 / 0.007830 | [flash] / [pro*] | flash | **[degrade_tier]** |
| **聚合** | | | | **0.008405 / 0.021532** | | | **节省 61.0%** |

> `pro*` = OFF 无 tier 决策，按静态基线的 pro 档计价（见 §4 诚实声明）。behavior-gate 的成本只计**测量轮**（2 个热身轮是训练行为门的脚手架，不计入成本差；tier 列仍显示全部 3 轮以保持透明）。墙钟取自 JSONL 的 `wallClockNs`，逐臂实测；工具耗时是夹具，所以绝对毫秒是夹具，**相对结构真实**（每次运行浮动数毫秒）。
>
> **「tier 已应用」列怎么读**：它来自 JSONL 的 `tierApplied`，记录 `TierResolver` 实际被调用换掉 provider 的次数，与「tier 决策」列是两回事。fanout 显示「—」不是没生效，而是该轮决策就是 pro、而两臂的基线模型本就是 pro，`applyScheduledTier` 的同档短路直接返回；behavior-gate 三轮都决策 flash 却只应用一次，因为第一次换完之后已经在 flash 上了。**这一列存在的意义见 §4 第一段——它是本报告一处已修正错误的直接证据。**

## 3. 逐场景证据（kernel 决策如何改变 harness 行为）

原始逐轮 `RoundPlan` 与 `ToolResult` 见 `docs/reports/data/agile2-scheduling-demo/scheduling-demo.jsonl`（10 行 = 5 场景 × 2 arm，on 在前）。

1. **fanout（并行分组基线）**：一轮 4 个连续只读工具。两 arm 都并行到峰值 4（OFF 静态分组自带并行）。成本相同（4 工具 > 复杂阈值 3 → on 也路由 pro）。作用是**证明 OFF 不是稻草人**——kernel 的收益必须来自真实决策，而非削弱基线。

2. **behavior-gate（并行分组生效）**：flaky 只读工具先失败 2 次热身（`MinSamples=2`），随后与健康只读工具同轮。ON 的行为门把已不可信的 flaky 工具**拉进自己的串行槽**——JSONL 中该轮 `parallelGroups: [["c1"],["c2"]]`（两个单元素组），峰值降到 1；OFF 盲目并行到 2。这是安全决策，代价见 §4。

3. **tier-switch（tier 切换）**：一轮小只读批。RuleDecider 因「无写、轮次小于复杂阈值」路由到便宜的 flash；OFF 无 tier 决策、按 pro 计价。纯成本差（0.00196 vs 0.00609）。

4. **suspend（工具挂起）**：kernel 决定挂起一个高风险工具。ON 由 harness 在执行前拦下（`ToolResult.Err = "suspended by scheduler"`，工具零执行）；OFF 无 plan → 工具照跑。挂起**决策**由 `suspendPolicyDecider` 建模（即 U41 BudgetController 会写入的 `RoundPlan.SuspendTools` 字段，与生产 `scheduler_integration_test` 同一强制路径）；**harness 拦截是真实代码**。

5. **budget-downgrade（预算阶梯降级）**：预算预置在 92%，轮内含 writer 本应走 pro。RuleDecider 的阶梯在 ≥90% 触发 `degrade_tier`，把该轮强制降到 flash；OFF 无视预算按 pro 计价。JSONL 中 ON 该轮 `tier: "flash", budgetAction: "degrade_tier"`。（70% 档 `halt_prefetch`、100% 档 `hard_stop` 的决策由 `kernel/sched/rule_decider_test.go` 单测覆盖；本演示聚焦可量化的 90% 成本降级档。）

## 4. 诚实声明（不重蹈 m0 合成演示覆辙）

> **本报告的一处更正（2026-08-20）**：本节初版把「tier 应用」列为已验证的真实行为。**那是错的。** 初版的演示没有配置 `TierResolver`，于是 `harness/internal/agent/tier.go` 走的是 nil-resolver 分支——发一条 `sched tier=flash` 的 Notice 然后 **return，provider 根本没被换掉**。也就是说：tier 决策是真的，tier 应用没有发生，而成本模型却按决策的 tier 计了价。由于 61.0% 的聚合节省主要由 tier 路由贡献，这个缺口直接动摇头条数字的立论。
>
> 现已修复：演示接上真实的 `TierResolver`，两臂基线模型均为 pro，调度器决策 flash 时 provider 被真正替换；JSONL 新增 `tierApplied` 字段作为「换发生了」的直接证据；新增护栏测试 `TestTierDecisionIsAppliedToProvider`（摘掉 resolver 后该测试立即失败）。**成本数字未变（仍为 61.0%）——变的是它从「给一个未执行的决策计价」变成了名副其实。** 这个缺陷是由一次独立评审发现的，本报告的作者没有自己发现它；记录在此以免同类问题再次以「诚实声明」的形式被掩盖。

**真实的**：所有调度决策（并行分组、行为门、tier、挂起、预算动作）与 harness 的执行行为（并发、拦截、**tier 应用**——见上方更正）都是与生产同一份代码；峰值并发由工具内的探针实测（非时间戳推断），确定性、在 `-race` 下无竞态（探针用 mutex）。

**合成夹具（标注）**：
- **每工具耗时**：20–30ms 的模拟值，用于让墙钟有可测切片。并发**结构**真实，绝对毫秒是夹具。
- **token→USD 价模型**：每轮固定 token 估算 × tier 单价。价表取 DeepSeek V4 公开价（2026-08，老 deepseek-chat/reasoner 名 2026-07 退役）：**V4-Flash** 输入 $0.14/M（cache-miss）、输出 $0.28/M；**V4-Pro** 输入 $0.435/M、输出 $0.87/M（标准价，另有峰谷浮动）。tier**决策**真实，绝对 USD 仅示意。
- **OFF 计价假设**：OFF 无 tier 决策，按 **pro 档**计价——即「没有 kernel 分档时，运维会静态供给强模型以覆盖最难的轮次」这一现实基线。**61.0% 的聚合节省主要由此假设下的 tier 路由贡献**。敏感性：若假设 OFF 静态钉 flash，则 tier/预算带来的成本差归零，收益只剩行为门的安全性与挂起的浪费规避——即节省是「tier 供给策略」的函数，不是凭空的。
- **热身轮不计成本**：behavior-gate 的 2 个热身轮纯为训练行为门（脚手架），已从成本累加中剔除（`assembleResult` 按 `Scenario.warmup` 跳过），否则会凭空给该场景制造成本差并抬高头条数字。**且需澄清**：behavior-gate 剔除热身后仍有的成本差（0.0007 vs 0.002175）本质仍是 tier 路由（小轮走 flash vs pro），**不是行为门自身的经济收益**——该场景的真实收益是并发/安全（峰值 1 vs 2），成本差只是 tier 效应的附带显现。各场景隔离的是不同**决策**（并发/挂起/预算），成本则是贯穿全部的同一个 tier 路由效应。

- **decider 配置偏离生产默认**：behavior-gate 场景用 `sched.Config{MinSamples: 2}`，生产默认是 `MinSamples=5`（`kernel/sched/rule_decider.go`）。降低样本门槛只是为了让行为门在 2 个热身轮后就触发、把演示压短；行为门的**判定逻辑**未改。其余场景用 `sched.Config{}` 生产默认。

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
| tier 是否真被应用 | `TierResolver` 调用序列落 JSONL 的 `tierApplied` | **真实**（见 §4 更正） |

## 7. 过程中发现的缺陷

### 7.1 kernel 发出的 `hard_stop` 在 harness 里没有执行点（已另开 issue）

U41 预算阶梯的 100% 档 `hard_stop` 可以出现在 `RoundPlan.BudgetAction` 里，但**没有任何代码读它**。
全仓非测试代码里 `sched.BudgetActionHardStop` 只出现三处：

```
kernel/sched/sched.go                        常量定义
harness/internal/agent/budget_controller.go  控制器给自己的花费分档 + severity 排序
harness/internal/agent/run_loop.go:652        if a.budgetCtrl != nil && a.budgetCtrl.Action() == hard_stop
```

那个守卫读的是 **agent 自己的 `budgetCtrl`**，不是 plan。而 `BudgetController` 的 `spentUSD` 从 0 起算，
只能由 `Observe(cost)` 累加——初始 `ResourceBudget.SpentUSD` 只进资源目录给**决策器**看，不进控制器。
净效果：**调度器判定该硬停时，harness 会照常执行整轮工具**；只有 agent 本地账算到超额才会拒绝。

`run_loop.go:648-651` 的注释写着「本地控制器是兜底；kernel 调度器可能已经通过 DecideRound 发出同样的
动作（谁先触发谁生效）」——实测不成立，kernel 那一路根本不参与。

对照之下 90% 档 `degrade_tier` 是**真接线的**（`tier.go:19` 读 `plan.BudgetAction` 并强制 flash），
所以本报告 budget-downgrade 场景成立。70% 档 `halt_prefetch` 本次未验证（演示未接 prefetch planner）。

**这也意味着**：任何声称「kernel 决策触发了硬停」的演示，实际验证的是本地控制器的兜底路径，不是
kernel 决策改变 harness 行为。本报告因此**不收录 hard_stop 场景**，而是把它作为缺陷上报。

### 7.2 演示自身的 tier 应用缺口（本 PR 已修）

见 §4 的更正块：初版未接 `TierResolver`，tier 决策未被应用却被计价。已修复并补护栏测试。

### 7.3 执行面地图（复现时会踩的 seam）

给后续复现者留档，这几处是驱动必须如实镜像生产的接缝：

| 接缝 | 位置 | 说明 |
|---|---|---|
| 工具轮执行 | `harness/internal/agent/execute_batch.go` | 决策器分组在此生效 |
| 挂起拦截 | `execute_batch.go:128` | `plan.SuspendTools` 的执行点 |
| tier 应用 | `harness/internal/agent/run_loop.go:661-662` → `tier.go` | **未配 `TierResolver` 时静默不换 provider**（§4 更正的根因） |
| 硬停守卫 | `run_loop.go:652-653` | 读本地控制器，**不读 plan**（§7.1） |
| 花费推送 | `run_budget.go:158-159` | 两步：`budgetCtrl.Observe(cost)` **之后必须** `SetResourceBudget(Snapshot())`，否则决策器看不到花费 |

## 8. 状态回写

- 路线图 `docs/Agile路线图.md` §2 验收标准 H3 门②：已勾选，指向本报告。
- U42 行：已记录 DoD 证据。
