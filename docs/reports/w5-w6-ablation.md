# W5–W6 执行报告：调度与进化实化 + 离线消融矩阵

> 日期：2026-08-31。对应计划：[`docs/specs/swebench-efficiency-research-plan.md`](../specs/swebench-efficiency-research-plan.md) §3 W5/W6。
> 前序报告：[`w0-w4-efficiency-experiments.md`](w0-w4-efficiency-experiments.md)。
> 本报告区分**代码落地**（有单测）与**实测数字**（离线、标注数据来源与局限）——沿用仓库纪律，设计预期与实测结果不混写。

---

## 1. W5 代码落地：调度与进化实化

| 子项 | 交付物 | 位置 |
|---|---|---|
| tier 规则学习化 | `ObserveTier`：每轮执行后回报实际 tier / intent / writer 数 / 成败，per-tier 成功率与 token 成本 EWMA。**只有复杂度规则可被学习覆盖**——cheap tier 在 complex-shape 轮上成功率 ≥ floor 且实测成本不劣于 pro 时降级（`learned:complex-cheap-ok`），失败保 pro（`learned:complex-needs-pro`），证据不足（< `TierMinSamples`，默认 10）回退硬规则；intent=mutation 与 writer 两个安全 floor 永不学习 | `kernel/sched/tier_learn.go`、`kernel/sched/rule_decider.go` |
| tier 证据接线 | harness 在每批工具执行收尾回报（冻结的 `Decider` 接口不动，按 `Observe` 同款类型断言注入）；token 成本归因留给 usage 路径（0 = 仅成败证据） | `harness/agent/execute_batch.go` |
| evolve 信号语义化 | `prefetch_hit` 按 `LeadMs` 带幅度：准时（≥0ms）计 1.0，迟到计 0.5（仍被消费但走了冷路径）；新增 **Usage 事件 → `cache_hit` 信号**：provider 前缀缓存命中率以 [0,1] 真幅度进入与 L2/L3 同一个调参闭环（此前 harness 侧所有信号恒为 Value=1，幅度信息被丢弃） | `harness/semantix/evolution.go` |
| 评估节奏拆分 | 冻结窗口按「参数能否破坏注入字节稳定」分治：`TauL2`（改 zone → 改注入集 → 打字节前缀缓存）仍守 `FreezeEpochs`；`PrefetchConf`（只门控投机预热准入，不重写在途会话的注入集）冻结期内继续评估，且 prefetch-only 步进**不再重置 TauL2 的冻结时钟**（原实现每次 real change 都重开 60 epoch 冻结，语义化后两者解耦） | `kernel/evolve/evolve.go` |
| prefetch 真实资源预热 | `EmbeddingWarmer`（预热远程 embeddings 连接/冷启动，向量即弃）与 `FileWarmer`（页缓存预热，root 边界 fail-closed、拒绝穿越）落地 `PrefetchTask` 早已声明但无 executor 的 `embedding`/`file` 两种 Kind；`Runner.WarmKinds`——warm 类任务执行但不持久化垃圾 Result 切片；`KindRouter` 按 Kind 组合多 executor | `kernel/prefetch/runner.go` |

测试：`kernel/sched/tier_learn_test.go`（学习降级/保 pro/安全 floor 不可学习/小轮不触发）、`kernel/evolve/evolve_test.go`（冻结期内 PrefetchConf 可调 + 不重开 TauL2 冻结）、`kernel/prefetch/warm_test.go`（warm 不持久化、egress 红线仍生效、FileWarmer 越界拒绝、KindRouter）。

**尚未落地（留待 W1 管线产生真实流量后）**：tier 学习的 token 成本证据目前来自 0（成败-only），全量成本归因需要 W1 的 per-round usage 关联；`ObserveTier` 的复杂度证据在真实 SWE 会话上积累到 `TierMinSamples` 后学习策略才会生效。

## 2. W6 离线消融矩阵

### 2.1 设置

- 数据：TraceLab `syfi_coding_trace`（真实 Claude Code/Codex trace，CC BY 4.0，内容剥离、结构级），39 个项目 / 4000 个 user turn（与 W0 同一预处理树）。
- 臂（`scripts/experiments/w6_ablation.sh`，探针 `semantix probe`，逐会话有序回放、累计建库）：
  - **A1 vanilla**：bm25、user 模式（基线检索）；
  - **A2 +hybrid(hash)**：bm25+向量融合、确定性 hash embedder；
  - **A3 +真实嵌入**：同 A2，bge-small-zh-v1.5 经本地 OpenAI 兼容服务（`scripts/experiments/embed_server.py`，全程回环、无数据出机）；
  - **A4 顺序消融（H5）**：同一批会话，`scripts/tracelab/probe_w6_order.py` 以种子随机重排文件名前缀（库内容不变，只变累积顺序）；
  - **A5 T-slice 准入**：tools 模式，同项目 vs 跨项目对照池（#268 证据）；
  - **A6 T-slice 粒度**：tools 模式 ± `--t-step-split`。
- 全部为**离线检索覆盖率**指标（hit/grey/miss），非 token/墙钟——后者需要 W1 真实双臂管线。

### 2.2 主矩阵（两种分母口径）

| 臂 | 全回放 turns | hit% | grey% | 库非空 turns* | hit% | grey% |
|---|---|---|---|---|---|---|
| A1 vanilla bm25 | 4000 | 26.5 | 15.3 | 2254 | 46.9 | 27.2 |
| A2 +hybrid(hash) | 4000 | 54.5 | 1.8 | 2254 | 96.7 | 3.3 |
| A3 +hybrid+真实嵌入 | 4000 | **56.2** | **0.1** | 2254 | **99.8** | **0.2** |
| A4 bm25（随机序） | 4000 | 26.8 | 49.6 | 3553 | 30.2 | 55.9 |
| A4 hybrid（随机序） | 4000 | 85.4 | 3.5 | 3553 | 96.1 | 3.9 |
| A5 T-slice tools 同项目 | 1723 | 53.3 | 0 | 970 | 94.6 | 0 |
| A5 T-slice tools 跨项目对照 | 754 | 93.4 | 0 | — | — | — |
| A6 A5 + t-step-split | 1723 | 53.3 | 0 | 970 | 94.6 | 0 |

\* 库非空口径排除每项目第一个回放会话（空库必然全 miss）；两种口径下结论一致，报告正文引用全回放口径。

### 2.3 发现

1. **检索升级是主要收益来源（复现并强化 W2/W3 结论）**：cross-session 命中率 bm25 26.5% → hybrid(hash) 54.5% → hybrid+真实嵌入 56.2%，同时灰区占比 15.3% → 1.8% → 0.1%。hybrid 尺度下真实嵌入把灰区几乎清零——`grey_mode=audit` 要接管的流量带在模型后端下大幅收窄，与 W2 难集「最差配对分 0.864→0.909」的方向一致。
2. **H5（顺序编排）部分被证伪、且暴露一个冷启动伪影**：随机序在 hybrid 臂上看似命中率更高（85.4% vs 54.5%），拆开发现是**分母伪影**——课程式（时间序）先把 207 轮的大 bootstrap 会话排在空库位（必然全 miss），随机序排在其位的是 4 轮小会话。排除空库首会话后 hybrid 臂顺序效应≈0（96.7% vs 96.1%）。BM25 臂的「课程式优势」（46.9% vs 30.2%）实为**小库阶段绝对尺度的 grey 膨胀**（随机序 55.9% grey）：库 <10 片时相对置信近似饱和，BM25 绝对下限则系统性落入灰区。**生产含义：冷启动（小库）阶段 hit/grey 会被高估，注入污染风险集中在早期——W1 的污染率指标在库冷启动期最关键；hybrid 有界尺度对库规模不敏感，生产默认优于纯 BM25。**
3. **#268 准入矩阵再次被验证**：T-slice（工具名结构级）同项目 53.3% vs 跨项目对照池 93.4%——低抽象层级切片无项目区分度，跨项目 scope 准入禁令是正确设计；同时 93.4% 的「跨项目命中」再次警示结构级命中不可作为复用证据（W0 报告结论的复现）。
4. **T-slice 步级拆分（W4 遗留）在剥离内容的数据上无差异**（A5 vs A6 完全相同）：验证边界检测退化为工具名匹配，其真实收益需内容级数据（真实 SWE 会话）重测——维持 W0-W4 报告的判断。
5. **诚实声明**：本矩阵所有指标是「检索到候选」的覆盖率，不是复用正确性——TraceLab 无内容、无标签，A3 的 99.8% hit 里必然混有伪相关（W0 已实测工具名级 hit 无项目区分度）。token/墙钟收益、pass@1 非劣、污染率 ≤5% 三项判据**仍未测量**，它们需要 W1 管线在真实 SWE-bench 会话上跑双臂（vanilla vs +L2 vs +L2+L3），这是计划剩余的最大缺口。

## 3. 对计划的更新建议

1. **W5 标记为「代码落地、证据待积累」**：学习策略已可运行，但其生效需要 W1 管线的 per-round usage 关联喂 `ObserveTier` 的 token 成本；真实 SWE 流量上 `TierMinSamples` 之前行为与硬规则一致（安全回退）。
2. **W6 离线部分完成**；全量消融（pass@1 非劣 / token / 墙钟 / 污染率）依赖 W1 预算（500 实例 × 双臂），建议按 §5 先跑 100–200 实例分层子集。
3. **新增设计约束（来自发现 2）**：zone 阈值在库冷启动阶段应有小库保护（绝对下限抬升或禁用相对饱和区），用 TraceLab 离线标定即可，不需要烧 API。

## 4. 复现路径

```bash
go build -o /tmp/semantix-w6 ./cmd/semantix
# 数据（一次性，sha256 已回填）
python3.12 scripts/tracelab/fetch.py --out /tmp/tracelab
python3.12 scripts/tracelab/probe_w0.py --trace /tmp/tracelab/syfi_coding_trace.jsonl.gz --out /tmp/tracelab/w0
# 顺序消融臂
python3.12 scripts/tracelab/probe_w6_order.py --src /tmp/tracelab/w0 --out /tmp/tracelab/w6-random
# 本地嵌入服务（bge-small-zh-v1.5，回环）
python3.12 -m venv /tmp/w6venv && /tmp/w6venv/bin/pip install fastembed
/tmp/w6venv/bin/python scripts/experiments/embed_server.py --addr 127.0.0.1:8688 &
# 消融矩阵（A1–A6，含模型嵌入臂）
bash scripts/experiments/w6_ablation.sh --w0 /tmp/tracelab/w0 --random /tmp/tracelab/w6-random \
  --semantix /tmp/semantix-w6 --out /tmp/tracelab/w6 \
  --embed-base-url http://127.0.0.1:8688 --embed-model bge-small-zh
```
