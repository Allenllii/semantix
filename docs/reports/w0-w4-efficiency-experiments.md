# W0–W4 效率研究计划执行报告

> 日期：2026-08-29。对应计划：[`docs/specs/swebench-efficiency-research-plan.md`](../specs/swebench-efficiency-research-plan.md)。
> 本报告区分两类内容：**代码落地**（已合入工作区、有单测）与**实测数字**（标注数据来源与局限）。

---

## 1. 代码落地总览（W2–W4 + 工具链）

| 工作流 | 交付物 | 位置 |
|---|---|---|
| W2 检索升级 | 无依赖 HNSW ANN 索引（确定性构建、tombstone 删除、recall@10 ≥ 0.9 @2k 向量） | `kernel/embed/hnsw.go` |
| W2 检索升级 | 网关 `embed_backend=model`：接入 `ModelEmbedder`（OpenAI 兼容 embeddings API，无 key 时 fail-open 降级 hash），模型后端自动启用 HNSW | `gateway/retriever.go` |
| W2 检索升级 | 启动批量嵌入（`BatchInserter`，64 条/请求），网关启动从 N 次 API 调用降为 ⌈N/64⌉ 次 | `gateway/retriever.go`、`gateway/gateway.go` |
| W2 检索升级 | 判定缓存：`CachedJudge`（verdict 缓存 24h + 超时后台预热） | `kernel/judge/cache.go` |
| W3 灰区修复 | `grey_mode=audit`：灰区切片以 `(grey, unverified)` 独立标记进入注入块（网关与 harness 桥都可配），不再静默丢弃；`GreyIncluded` 计数可观测 | `kernel/inject/inject.go`、`gateway/config.go`、`harness/semantix/bridge.go` |
| W3 灰区修复 | judge 超时可配（`judge_timeout_ms`，此前固定 30s 阻塞 TTFT）；超时请求 fail-closed 同时后台预热下一次 | `gateway/config.go`、`kernel/judge/cache.go` |
| W4 scope 准入 | #268 准入矩阵落地：T/R 切片在 `FileStore.Put` 强制降级，User scope 不可能持久化低层轨迹（写入口唯一收口点） | `kernel/slice/admission.go`、`kernel/slice/file_store.go` |
| W4 库内整理 | Context 切片近重复合并（token-set Jaccard，确定性聚类，计数求和重建「repo 概览卡」）；挂到 `gc --consolidate-context`，只在维护窗口运行（注入字节稳定不受影响） | `kernel/slice/consolidate.go`、`cmd/semantix/gc.go` |
| W4 抽取粒度 | T-Slice 步级拆分（`--t-step-split`）：在验证边界（test 命令）切分工具序列为子任务级切片 | `kernel/slice/extractor.go` |
| W0 工具 | `semantix probe` 跨会话命中率探针：有序回放、累计库、逐 turn zone/来源/分数，支持 user/tools 查询模式与 bm25/hybrid/模型嵌入臂 | `cmd/semantix/probe.go` |
| 工具链 | TraceLab 数据准备脚本（#263：按项目分组、按时间排序、对照集生成）；本地 OpenAI 兼容嵌入服务（fastembed，bge-small-zh，仅回环） | `scripts/tracelab/probe_w0.py`、`scripts/experiments/` |
| 工具链 | TraceLab 资产 sha256 校验和回填（`11ce51ec…`），`fetch.py` 从 placeholder 变为可验证 | `scripts/tracelab/fetch.py` |

已知问题（留给 RAG 负责人）：`semantix search --retriever hybrid` 走 RRF 融合（分数 ~1/60 尺度），zone 阈值却是 BM25/cosine 尺度——hybrid 臂的 zone 输出全部 miss，尺度不一致需要统一到网关的归一化融合口径。

## 2. 实测数字

### 2.1 W0：跨会话命中率（TraceLab 真实负载，290 会话 / 40 项目）

数据：TraceLab `syfi_coding_trace`（真实 Claude Code/Codex trace，CC BY 4.0，内容剥离、仅结构）。方法：每项目内按时间排序，会话 i 的检索库 = 会话 0..i-1 的切片；查询 = 每轮工具名序列（T-slice 结构级）。对照：每项目 1 个会话组成的跨项目池。

| 臂 | turns | hit | grey | miss | 说明 |
|---|---|---|---|---|---|
| 同项目（38 项目聚合） | 1723 | 918（53.3%） | 0 | 805 | 项目内中位数 67.3%，均值 58.6%，min 3.2%，max 99.1%；26/38 项目 ≥50% |
| 跨项目对照 | 754 | 704（93.4%） | 0 | 50 | **危险信号** |

两个关键结论：

1. **项目内跨会话 T-slice 命中真实存在且差异大**——一半以上项目里，后续会话过半的轮次能检索到早前会话的切片。C-slice（真实内容级）大概率高于此数字（TraceLab 无内容测不了）。
2. **工具名级 T-slice 不具项目区分度**：跨项目池命中率 93.4% 反而更高。工具名序列（Read/Grep/Bash…）是通用词汇，BM25 上一匹配就命中——这类「命中」是负迁移风险而非复用机会。这**实测验证了 #268 准入矩阵的正确性**（T/R 低抽象层级不进跨项目 scope），并给出新的设计约束：T-slice 检索特征需要带上参数/路径级别信号（内容级），否则 zone=hit 不可作为复用证据。
3. 灰区在 BM25 尺度上是双峰的（要么 ≥0.8 相对置信要么 <0.55），grey=0——GW4 观察到的灰区问题主要出现在 hybrid/cosine 有界尺度上。

### 2.2 重复任务场景（gateway-m1 真实 query）

10 条真实技术问答 query × 4 个会话：从第 2 个会话起 **30/30（100%）cross-session hit**（prompt 切片）。验证了 L2 重复复用路径（GW4 的前提场景）。

### 2.3 W2 标定：改写任务（难集：低词汇重叠同义改写，n=10）

| 臂 | 命中 | top-1 均分 | 备注 |
|---|---|---|---|
| bm25（probe，top-1 相对） | 10/10 | 9.54 | 困难集 BM25 绝对分较简单集（21.85）跌 56% |
| hybrid+hash | 10/10 | 0.962 | min 0.864 |
| hybrid+bge-small-zh（真实嵌入，本地服务） | 10/10 | **0.984** | **min 0.909**，最差情况最稳 |

n=10 且任务间主题距离大，区分度有限；但趋势一致：真实嵌入在低词汇重叠下把最差配对分数从 0.864 抬到 0.909（离 AbsHigh=0.7 的安全边际更大）。**候选级**测量（`semantix verify`，BM25）：10 条困难改写 100% 落灰区（真值配对却被相对置信压下去）——GW4 灰区现象在真实数据上复现，`grey_mode=audit` 与 judge 预热正是为这类流量设计的。

### 2.4 诚实声明

- 以上全部为**离线**数字：无真实 SWE-bench agent 运行、无 LLM 计费对比。token/墙钟收益仍是设计预期，不是实测。
- TraceLab 内容剥离，C/P/R 切片的内容级跨会话命中率仍需真实 SWE 会话（v28 单实例管线已验证可跑通官方评测）。
- 改写集为手写 n=10，方向性证据，不构成统计结论。

## 3. 对研究计划的修订建议

1. **W0 假设 H1 部分成立、部分被证伪**：跨会话复用真实存在（53%/中位 67%），但载体必须是内容级切片（C/P）；T-slice 在工具名粒度无区分度，**不应当作跨会话命中的证据来源**。probe 的 tools 模式应视为「负迁移风险探测器」而非收益探测器。
2. **W3 优先级提前**：hybrid/cosine 尺度下灰区丢失是真实瓶颈（复现 100% grey on hard set），`grey_mode=audit` + `judge_timeout_ms` + verdict 预热已落地，下一步在真实流量上测 audit 模式的污染率。
3. **W2 阈值需按尺度分开标定**：BM25 尺度灰区双峰、cosine 尺度灰区是主损失带——两套 zone 阈值（per-scale）比全局阈值更合理，用 TraceLab + 本地嵌入服务离线标定即可，不烧 API。
4. **W4 的 T 步级拆分在 TraceLab 上无差异**（清洗数据无工具参数，验证边界检测退化为工具名匹配），其真实收益需在内容级数据上重测；Context 合并与准入矩阵已就位。

## 4. 复现路径

```bash
# TraceLab 数据（一次性）
python3.12 scripts/tracelab/fetch.py --out /tmp/tracelab          # 校验和已回填
python3.12 scripts/tracelab/probe_w0.py --trace /tmp/tracelab/syfi_coding_trace.jsonl.gz --out /tmp/tracelab/w0
# W0 探针（同项目臂 / 对照臂）
semantix probe --dir /tmp/tracelab/w0/project_xxx --query-mode tools --json
semantix probe --dir /tmp/tracelab/w0/control --query-mode tools --json
# W2 标定（本地嵌入，无外部依赖）
/tmp/femb/bin/python scripts/experiments/embed_server.py &        # 127.0.0.1:8688
python3.12 scripts/experiments/w2_calibration_set.py && python3.12 scripts/experiments/w2_hard_set.py
bash scripts/experiments/w2_run_arms.sh && bash scripts/experiments/w2_search_arms.sh
```

署名：TraceLab / SyFI Lab, University of Washington（https://tracelab.cs.washington.edu ，CC BY 4.0，仅使用结构级清洗数据）。
