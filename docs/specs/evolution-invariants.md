# Spec：保守进化不变量（Evolution Invariants）——Issue #266

> 对应 Issue：#266（2026-08 文献综述，域 3 经验学习与自进化，负结果文献）。
> 判级：**Spec-Exempt（E2 文档）**——本身不产生行为变更，但为未来
> Spec-Required 工作（C 切片 LLM 抽取、promote 固化、切片压缩等）定验收标准。
> 源码现状基线：`kernel/evolve/evolve.go`（仅 import errors/math/sync）、
> `kernel/judge/judge.go`（`Confirm` 二元判定）、`kernel/slice`（`Slice.Content`
> 一次写入即冻结，全仓无改写路径）。
> 状态：先审后写（本文件为纯文档，无代码）。

## 0. 一句话

把「保守进化」从**现状巧合**固化为**书面不变量**：原始 episode 是一等证据、
切片内容禁止 LLM 批量改写、进化只做增量、切片质量评估必须锚定外部信号——
给未来的 evolve/promote 扩展划红线，防止系统滑向「LLM 持续改写经验」的反模式。

## 1. 文献依据（负结果三连）

### 1.1 Useful Memories Become Faulty When Continuously Updated by LLMs（arXiv:2605.12978）

持续用 LLM 改写/固化经验会让记忆变差：GPT-5.4 在 **54%** 曾零记忆解出的
问题上，经验被固化后反而失败；而仅做「原始情景保留/删除」即可匹配所有
固化式方案。**结论：原始 episode 当一等证据，固化必须显式门控。**

### 1.2 Agentic Context Engineering（arXiv:2510.04618）

迭代整体重写导致 brevity bias 与 context collapse；结构化增量更新（+10.6%）
才是安全形态。**结论：进化只做 delta，禁止「整体重写式」进化。**

### 1.3 LLMs Cannot Self-Correct Reasoning Yet（arXiv:2310.01798，ICLR'24）

无外部反馈的模型自评不可靠。**结论：切片质量评估必须锚定外部信号
（命中率、用户纠正等），不能让模型自评成为唯一依据。**

## 2. 代码现状（当前恰好满足，但无护栏）

| 约束 | 现状 | 性质 |
|---|---|---|
| evolve 无 LLM 依赖 | `kernel/evolve/evolve.go` 仅 import errors/math/sync，不改切片内容，只调全局 TauL2 | ✅ 未实现（巧合） |
| 切片内容一次写入即冻结 | 全仓无改写 `Slice.Content` 的路径（仅提取构造/存储克隆/读取） | ✅ 未实现（巧合） |
| judge 不评切片质量 | `kernel/judge/judge.go` 只做 L3 复用性二元判定 | ✅ 未实现（巧合） |
| Weight 不参与检索 | `kernel/bm25` / `kernel/embed` 检索打分不读 `Slice.Weight`（#219 已定） | ✅ 约定（未入文档正文） |

**风险**：C 切片 LLM 抽取、promote 固化、切片压缩等 RFC 一旦排期，没有
书面红线就可能自然滑向「LLM 持续改写」反模式——文献证明那会让系统比
无记忆还差。

## 3. 不变量（本文档正文，未来 RFC 必须逐条对照）

### I-1 原始层不可变

会话 JSONL 与规则式提取切片是**一等证据**。任何抽象产物（LLM 抽取、
压缩、固化）必须：

- 保留到原始来源的链接（`Slice.Meta.SourceSession` 已有，扩展时不得移除）；
- **不得覆盖原始层**：抽象产物只能新增，原始切片/会话记录在任何抽象
  流程中保持字节不变。

### I-2 固化高门槛

LLM 参与的抽象/压缩/固化必须同时满足：

- **(a) 外部信号达标才触发**：命中率/复用率等外部指标超过阈值才允许
  固化，禁止「先固化后验证」；
- **(b) 产物走独立类型/命名空间**：固化产物不得混入原始切片空间，
  必须可辨识来源与抽象层级；
- **(c) 可一键回退到原始层**：任何固化产物的消费方都能追溯到原始层，
  且回退路径无需重建（原始层仍在）。

### I-3 增量进化

evolve 对参数/权重只做 **delta 更新**（EWMA 等增量形态），禁止
「整体重写式」进化（ACE 教训）。参数存储变更必须保持注入集冻结期
（架构文档 §6.2），避免进化参数抖动摧毁它自己喂养的字节缓存。

### I-4 外部信号锚定

任何切片质量评估（现在的 judge、未来的评分器）**不得以模型自评为
唯一依据**。评估必须锚定外部信号：命中/未命中、用户纠正、显式
approve/reject、预取 hit/waste 等。模型输出只可作为候选证据之一，
不可单独决定切片存废或价值。

### I-5 Weight 永不参与检索（#219 提升）

`Slice.Weight` 是价值/维护信号（gc 清理、进化调权），**永不参与检索
打分**（bm25 相关性、embedding 相似度、融合分数）。检索只依据内容
相似性；Weight 进检索会形成「高权重切片自我强化」的反馈环，破坏
检索中立性。本不变量从 #219 的 PR 描述提升为架构文档正文，未来任何
检索路径（含新检索器）都不得读取 Weight。

## 4. 验收与引用要求

1. 本文档合入后，本批相关 RFC（C 切片提取、切片压缩、promote 共识）
   的 spec 必须逐条对照 §3 的五条不变量，注明「满足/豁免/例外及理由」；
2. 任何新增检索路径的代码评审必须包含「不读取 `Slice.Weight`」检查；
3. 任何新增 LLM 参与的切片内容生产流程，评审必须回答 I-2 的 (a)(b)(c)
   三条是否全部成立。

## 5. 参考

- Useful Memories Become Faulty When Continuously Updated by LLMs: https://arxiv.org/abs/2605.12978
- Agentic Context Engineering: https://arxiv.org/abs/2510.04618
- LLMs Cannot Self-Correct Reasoning Yet: https://arxiv.org/abs/2310.01798
- 架构文档 §6 进化引擎：`docs/Agent-Infra-架构设计.md`
- Issue #219：Weight 永不参与检索（原为 PR 描述约定）