# SWE-bench Verified「等效最省」研究计划

> 状态：提案（draft v1, 2026-08-29）。目标读者：加入本研究的协作者。
> 目标定义：**固定模型 + 固定 harness，在已饱和的 pass-rate 基准（SWE-bench Verified）上做到等效最省** —— pass@1 非劣的前提下，语义缓存命中率更高、总 token 更少、墙钟时间更快。

---

## 1. 架构现状：什么是真的，什么是设计

以下事实基于 2026-08-29 对仓库的代码级调查，用于对齐团队预期，避免拿着 README 的宣传语讨论。

**已经真实落地的链路：**

| 能力 | 状态 | 位置 | 备注 |
|---|---|---|---|
| L3 结果复用（指纹验证 + 灰区 judge，fail-closed） | ✅ 网关热路径 | `gateway/pipeline.go` → `kernel/cache/l3.go` | 流式可回放（GW2 已合入） |
| L2 语义注入（确定性整片注入） | ✅ 网关热路径 | `kernel/inject/inject.go` | Anthropic 路径带 cache_control 断点 |
| 切片抽取（P/C/T/R/M）+ BM25 检索 | ✅ | `kernel/slice`、`kernel/bm25` | turn 级粒度，内容哈希去重 |
| 调度 / 预取 / 进化（sched/prefetch/evolve） | ✅ 仅 harness 内部 | `harness/agent` + `harness/semantix` 桥 | **网关请求路径完全没有这三者** |
| 计量（baseline vs paid、InjectROI） | ✅ | `kernel/usage` | 依赖 provider usage 字段 |

**关键短板（按对目标的影响排序）：**

1. **「语义」目前是 BM25 + FNV 字符 n-gram 哈希嵌入**（256 维，代码注释自认占位），无 ANN 索引（map + 暴力余弦）。`ModelEmbedder`（真实嵌入）只在 CLI 存在，网关未接入。这是检索质量上限的最大瓶颈。
2. **灰区判定过严且 harness 桥直接丢弃 grey**（`harness/semantix/bridge.go:131` 只放行 zone.Hit，无 judge 兜底）。GW4 验收数据显示 10 个重复任务里 8 个落灰区 —— 这是命中率最大损失点。
3. **LLM judge 同步阻塞网关请求**（30s 超时），直接拖慢 TTFT，与设计注释声称的 async 不符。
4. **Prefetch 实质是玩具**：转移矩阵只学到工具名 bigram，warm-up 是本地拼注入块，几乎不省 token；内核 Runner 未被 harness 调用。
5. **调度 tier 全硬编码**（intent / 写工具 / 复杂度三个 if），evolve 信号稀疏（value 恒为 1、仅 3 类事件、冻结 60 epoch），闭环几乎不产生实际调整。
6. **所有量化主张（79.8% 成本、79.5% 时延、自进化曲线）均来自合成回放 + 参数化成本模型**，复用比例是假设参数而非实测。仓库中没有任何真实 agent 基准。

## 2. 研究假设

SWE-bench Verified 的结构对本方案异常友好，这是核心机会：

- **H1（跨实例复用）**：同一 repo 家族的不同实例共享 Project scope 的高价值内容 —— 构建/测试命令、repo 结构、高频文件路径（C-slice）、`定位→改→跑测试` 的工作流模式（T-slice）。这些应当在后续实例上产生真实的 L1/L2 命中。bug 级 R-slice 则不应跨实例复用（负迁移风险）。
- **H2（上下文注入减少探索轮次）**：好的 C/T 注入让 agent 少走弯路，省 token 的主力是「少几轮 grep/readFile」，而非缓存折扣本身。
- **H3（前缀稳定喂饱 L1）**：确定性注入 + 固定排序把语义相似变成字节稳定，把 provider prefix cache 命中率抬上去。
- **H4（L3 跳轮）**：跨实例几乎不会命中（不同 bug），但实例内多轮/重试场景可能命中。预期贡献小，保留但不押注。
- **H5（顺序编排）**：先跑的实例喂库，后续实例受益 —— 课程式（按 repo 聚簇）vs 随机顺序应可测出差异。

**必须先证伪的风险**：同 repo 不同 bug 的 agent 轨迹差异可能远大于预期，导致跨实例命中率接近 0。因此 W1 之前先做一个 10–20 实例的 pilot，只测「实例 B 开始时库里有哪些来自实例 A 的切片被 zone.Hit」，这个数字决定整个方案的预期收益。

## 3. 工作流拆分（可分配）

### W0 · Pilot：跨实例命中率探针（1 人，~1 周，最优先）
- 选 2–3 个 repo 家族各 5–10 个 Verified 实例，顺序跑，把实例 N 的会话 JSONL 灌库后检索实例 N+1。
- 产出：跨实例 zone 分布（hit/grey/miss）、可复用切片类型分布（预期 C/T 高、R 低）、灰区占比。
- 这个数字决定 W2/W4 的优先级和全量实验的预期。

### W1 · 评测基座（1–2 人，~2 周，与 W0 并行）
- 接入 SWE-bench Verified 评测（官方 harness 或 mini-swe-agent 类轻量 runner），走 semantix 网关或 harness 桥。
- 按 repo 分层的固定子集（建议 300–500 实例）+ 固定 seed + 双臂 A/B（semantix on/off，同模型同 harness）。
- 指标采集落地到 `kernel/usage` 既有口径：pass@1、input/output/cached token、墙钟、轮次数、分层命中率（L1/L2/L3）、InjectROI、污染率（注入导致的回退/失败）。
- 非劣判据：pass@1 差异落在 ±2% 置信区间内；主指标是「每成功任务的 token 与墙钟」。
- CI 增加轻量回归门禁（小子集，防止优化回退）。

### W2 · 检索质量升级（1 人，~2 周）
- 网关接入真实 embedding（本地 bge-m3 / Ollama，或 API，fail-soft 到 BM25），替换 hash embedder。
- 引入 ANN（HNSW 类）替换暴力余弦，支撑万级切片。
- hybrid 权重与 zone 阈值标定：**用 TraceLab（#263，4265 条真实 Claude Code/Codex trace）离线标定，不烧基准 API 预算**。TraceLab 落地本身是本工作流的子任务。

### W3 · 灰区与 L3 命中率修复（1 人，~1–2 周）
- harness 桥给 grey 加兜底：要么接 judge（异步），要么放宽阈值 + 注入审计标记，用 W0/W1 数据选。
- judge 移出网关热路径（异步化或硬超时降级），保住 TTFT。
- 对齐 GW4 验收门（真实全链路 + 二次命中 + 成本节省 ≥30%）。

### W4 · 切片语义升级（1 人，~2 周，依赖 W0 结论）
- 落地 #268 的 T/R scope 规则（当前纯文档）：低层 T/R 禁跨项目，防负迁移。
- extractor 粒度从 turn 级升级到子任务级（#58 止损方案同款）。
- C-slice 升级为「repo 概览卡」：构建命令、测试命令、目录结构、高频路径，作为注入主力。
- R-slice 负迁移守卫：跨实例默认不 promote。

### W5 · 调度与进化实化（1 人，~2–3 周，可后置到 M2）
- tier 规则学习化：用实测 token 成本 + 质量反馈替代三个硬编码 if。
- evolve 信号语义化：value 恒为 1 改为带幅度信号，扩事件类型，缩短冻结窗口的评估。
- prefetch 从「拼注入块」升级为真实资源预热（embedding 查询、repo 索引、文件 mmap），沿用 hit/waste 反馈框架。

### W6 · 结果与消融（全员，M3）
- 消融矩阵：vanilla / +L2 / +L2+L3 / +L2+L3+prefetch / 课程式 vs 随机顺序。
- 报告发布到 `docs/reports/`，区分设计目标与实测结果（沿用仓库现有纪律）。

## 4. 里程碑

| 里程碑 | 内容 | 退出判据 |
|---|---|---|
| **M-R0**（~1 周） | W0 pilot + W1 管线跑通 | 跨实例命中率数字落地；双臂管线在 20 实例上出全指标 |
| **M-R1**（~3 周） | W1 全量基线 + W2 + W3 | 固定子集双臂基线数字；灰区损失显著收窄；检索升级 A/B |
| **M-R2**（~6 周） | W4 + W5 + W6 | 全量双臂结果：pass@1 非劣，token 与墙钟显著下降，报告发布 |

## 5. 预算与风险

- **API 预算**：500 实例 × 双臂 ≈ 1000 次完整 agent run。管线调试期用更便宜模型 + 50 实例子集；全量只在 M-R1 后跑一次。建议先跑 100–200 实例分层子集，效应量明确再扩。
- **负迁移风险**：注入污染导致 pass 下降 —— 污染率是 W1 的一级指标，任何消融臂污染率 >5% 即回退该机制（对齐 README 目标 ≤5%）。
- **饱和集的统计功效**：Verified 上强模型 pass@1 已很高，提升空间集中在难实例；分层子集应保留足量难例，报告按难度分层呈现。
- **对比基线**：最低要求 vanilla harness vs semantix；对外主张「等效最省」时建议加「仅 prefix cache（无 semantix）」臂，把 L1 归因讲清楚。

## 6. RAG 负责人工作包（新成员上手指南）

职责范围：W2 全部 + W4 的抽取粒度与去重部分。核心判断先行：**切片库比会话级记忆更原子，但原子化是杠杆不是目标**——注入需要整片字节稳定（喂 L1 前缀缓存），检索需要上下文区分碎片，原子粒度必须由 W0 pilot + TraceLab 数据按切片类型实测决定。

任务清单（按顺序）：

1. **检索升级（W2 主体）**：网关检索当前是 FNV 字符 n-gram 哈希嵌入（`kernel/embed/hash.go`，256 维，注释自认占位）+ map 暴力余弦（`kernel/embed/vecindex.go`），无 ANN。第一步把 `ModelEmbedder`（`kernel/embed/model.go`，已实现但只在 CLI 使用）接入网关，fail-soft 降级 BM25 的路径已存在；第二步引入 HNSW 类 ANN 支撑万级切片。
2. **离线标定**：用 TraceLab（#263，4265 条真实 trace，结构级）标定 hybrid 权重与 zone 阈值，不烧基准 API 预算。**注意：任何打分逻辑改动都会改变 zone（hit/grey/miss）分布，必须重跑标定**——这是最容易踩的坑。
3. **近似去重与合并**：当前 dedup 只靠 sha256 内容哈希（`kernel/slice`），近似重复切片会无限堆积。用 embedding 聚类找近重复并合并——但合并只发生在库内整理（GC/maintenance 时机），**不发生在注入时**：合并会改变注入内容 → 破坏字节稳定 → 打掉 L1 命中。
4. **元数据过滤检索**：scope（session/project/user）、slice type（P/C/T/R/M）、zone 都是索引字段，配合 #268 规则（低层 T/R 禁跨项目）做 per-type 过滤与 per-type 阈值。
5. **记忆管线特有贡献**：语义聚类合并、陈旧切片归档、C-slice 升级为层级化「repo 概览卡」——记忆隐喻里最值钱的是遗忘与合并，不是存储。

架构红线（设计原则，非建议）：注入确定性（整片、排序冻结，动态重切分只许在库内）；fail-open（嵌入服务不可用降级 BM25，不许 fail-closed）；scope 隔离不许被 ANN 索引扁平化打破。

## 7. 与现有路线图的关系

- 本计划是对 #58（真实数据命中率 ≥70%）、#263（TraceLab 真实负载）、GW4（网关验收门）三条未闭合门禁的统一落地方案：一个基准管线同时服务三者。
- W4 直接实现 #268 的落地化；W2 消化 P1 阶段遗留的「local embeddings + ANN pending」。
- 不改变现有 Agile 里程碑的顺序承诺，只为其提供第一个真实数据来源。
