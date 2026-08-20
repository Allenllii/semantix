# Agile 2 · U42 调度演示报告：kernel 决策 on/off 对照（H3 验收②证据）

> 日期：2026-08-20 · 对应 Issue：#193 · 判级：Spec-Exempt（验收执行 + 报告，不改契约）
> 结论：**验收②「调度演示：kernel 决策改变 harness 行为 + 可量化收益」达成**——
> 同一任务族在决策 on/off 两臂下，挂起执行省 **79.5%** 轮时延、预算阶梯三档
> （halt_prefetch / degrade_tier / hard_stop）全部生效且硬停阻断了 off 臂会照常执行的
> 全部工具调用；每一轮的 RoundPlan 均落盘可解释。

---

## 1. 方法

**两臂**：
- **on** = 生产默认装配：`kernel/sched.NewRuleDecider(sched.Config{})`（U40 进程内直连）+
  `ResourceBudget{LimitUSD: 1.00}`（U41 预算控制器）；
- **off** = pre-U40/U41 行为：返回空 RoundPlan 的决策器（`executeBatch` 回退静态只读分组，
  无挂起执行、无 tier、无预算动作），预算未配置（LimitUSD=0）。

**驱动**：[harness/agent/scheduling_demo_test.go](../../harness/agent/scheduling_demo_test.go) 的
`TestU42SchedulingDemo`（环境变量门控，不进 CI）。它直驱生产代码的三个 seam：
`executeBatch`（工具轮执行）、run_loop.go:647 的硬停守卫、run_loop.go:657 的
`applyScheduledTier`，以及 run_budget.go:158-159 的「花费观察 → 资源目录快照推送」镜像。
决策器被 tee 包装：每次 `DecideRound` 的输入摘要与完整 RoundPlan 落
[roundplans.jsonl](data/agile2-scheduling-demo/roundplans.jsonl)（决策可解释性证据），
行为观察（`Observe`）原样透传使 RuleDecider 的行为门按生产路径学习。

**工具组**：6 个只读工具——4 个健康读取（40ms）、1 个可切换故障的 `flaky_probe`（40ms）、
1 个慢速 `noisy_scan`（200ms）；共享并发计量器记录每轮峰值并发。

**量化口径**（#193 要求）：端到端轮时延（wall）、峰值并发（peak）、工具实际执行计数
（runs）、tier 解析记录、预算动作、RoundPlan 落盘样本。原始数据：
[rounds.json](data/agile2-scheduling-demo/rounds.json)。

## 2. 结果总表（实测，2026-08-20，Apple Silicon 本机）

| 场景 | 臂 | wall (ms) | peak | 实际执行 | tier | 预算动作 | 硬停 |
|---|---|---:|---:|---|---|---|---|
| S1 并行基线 | on | 41.7 | 5 | 全部 5 | **pro** | — | — |
| S1 并行基线 | off | 41.2 | 5 | 全部 5 | — | — | — |
| S2 行为门 | on | 82.5 | 4 | 全部 5（flaky 被隔离串行） | pro | — | — |
| S2 行为门 | off | 41.1 | 5 | 全部 5（flaky 混入并行批） | — | — | — |
| S3 挂起执行 | on | **41.3** | 2 | 2（noisy_scan 被跳过） | **flash** | — | — |
| S3 挂起执行 | off | **201.0** | 3 | 3（noisy_scan 照常执行） | — | — | — |
| S4a 花费 75% | on | 40.5 | 2 | 2 | — | **halt_prefetch** | — |
| S4b 花费 95% | on | 40.4 | 1 | 1 | — | **degrade_tier** | — |
| S4c 花费 105% | on | **0** | 0 | **0（拒绝执行）** | — | **hard_stop** | ✅ |
| S4a-c | off | 40.2-41.2 | 1-2 | 全部照常 | — | 无任何动作 | — |
| S5 组合 | on | 0 | 0 | 0（粘性硬停维持） | — | hard_stop | ✅ |
| S5 组合 | off | 201.3 | 5 | 全部 5（含 noisy+flaky） | — | — | — |

## 3. 四个机制逐项解读

**① 并行分组（S1）——无回归**。决策器对健康只读批的分组与静态分区一致
（peak=5、wall≈1×工具时延），即 kernel 接管调度不损失既有的只读并行红利；
同时 on 臂产生 tier=pro 决策并经 TierResolver 真实解析（off 臂无 tier 概念）。

**② 行为门（S2）——用一档时延换可靠性隔离**。6 轮故障预热后（MinSamples=5），
RuleDecider 把成功率低于 SuccessFloor 的 `flaky_probe` 拉出并行组
（RoundPlan 显示 `[[read_a..read_d], [flaky_probe]]`，peak 5→4，wall +41ms）。
这是设计意图（N04：低可靠工具不得污染并行批），代价与收益都如实呈现。

**③ 挂起执行（S3）——kernel 决策改变 harness 行为的最直接证据**。资源层标记
`noisy_scan` 挂起后：on 臂决策器在 RoundPlan 中回显挂起集，`executeBatch`
跳过该工具（结果为 "suspended by scheduler"，执行计数 0），轮时延 **201.0→41.3ms
（-79.5%）**；off 臂无 plan 承载挂起指令，同一挂起意图**不被执行**，200ms 照烧。

**④ 预算阶梯（S4，U41 的 70/90/100 三阈值）——三档全部按设计触发**。
花费跨过 75% → plan 级 `halt_prefetch`（预取清空）；95% → `degrade_tier`
（tier 强制 flash——按 GLM 价表 pro=$1.4/M vs flash=$0.07/M，即后续轮输入单价 **1/20**）；
105% → run_loop 守卫拒绝整轮工具调用并给出明确文案
（`budget exhausted: spent $1.0500 of the $1.0000 session limit; no further tool calls will be issued`）。
off 臂（预算未配置）三档均无反应、照常烧钱。S5 进一步证明硬停在窗口内**粘性**：
后续组合轮维持拒绝，而 off 臂把含慢工具与故障史工具的 5 调用全部执行。

## 4. 决策可解释性样本（RoundPlan 落盘）

`roundplans.jsonl` 每行一条决策记录。S3 on 臂样本（节选）：

```json
{"scenario":"S3-suspend","arm":"on","round":1,
 "tools":["read_a","read_b","noisy_scan"],"suspended_input":["noisy_scan"],
 "plan":{"ParallelGroups":[["c1","c2"]],"Tier":"flash",
         "SuspendTools":["noisy_scan"],"MaxParallel":4}}
```

输入（工具集、挂起集、预算快照）与输出（分组、tier、挂起回显、预算动作）逐轮对应，
决策可追溯。

## 5. 复现步骤

```bash
SEMANTIX_SCHED_DEMO=1 SEMANTIX_SCHED_DEMO_OUT=docs/reports/data/agile2-scheduling-demo \
  go test ./harness/agent/ -run TestU42SchedulingDemo -v
```

产物：`rounds.json`（逐轮指标）+ `roundplans.jsonl`（逐轮决策）。驱动默认跳过
（`SEMANTIX_SCHED_DEMO` 未设置时），不影响 CI；睡眠型工具的 wall 数值随机器负载
有 ±数 ms 波动，结构性结论（执行计数、动作、分组）确定性复现。

## 6. 边界与局限（如实）

- 本报告是**机制级受控实验**：工具为睡眠模拟、决策输入由驱动构造（镜像 run_loop /
  run_budget 的生产 seam），不含真实 LLM 流量。会话级真实收益由 U43 自进化曲线
  （#194 / PR #228）与 #58 verify 回放另行度量。
- S4 的成本节省来自「拒绝执行/降档」这一结构性事实；美元金额是驱动注入的模拟花费。
  tier 降档的单价差引用 GLM 价表作参照（见 `docs/specs/semantix-glm-optimization.md` §3.1）。
- 复现驱动时注意 run_budget.go:158-159 的双步 seam：仅 `budgetCtrl.Observe` 不足以让
  决策器看到花费，必须随后 `SetResourceBudget(Snapshot())` 推送资源目录——生产路径
  两步都做，驱动如实镜像。

## 7. 验收判定

- [x] 对照任务集 ≥5 任务 × on/off（S1-S5 × 2 臂，含预热共 26 轮）
- [x] 并行分组生效（S1）/ 工具挂起（S3）/ tier 切换（S1 pro、S3 flash、S4b 降档）/ 预算降级（S4 三档）各至少一例
- [x] 量化口径四项齐备（wall / 并发度 / 执行计数与成本动作 / RoundPlan 落盘）
- [x] 本报告 + 原始数据 + 复现步骤入库
- [x] 路线图 §2 验收②勾选（随本 PR 回写）

**H3 验收②：通过。**
