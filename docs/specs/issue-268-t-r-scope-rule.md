# Spec：跨项目复用按抽象层级防负迁移（T/R 不进 User scope）（Issue 268）

> 对应 Issue：#268 跨项目复用按抽象层级防负迁移（T/R 不进 User scope）
> 判级预估：**Spec-Required**（准入矩阵改变提取/导入行为；但经核实当前无 User scope 写入通路，
> 本轮为「架构原则 + 未来落点」文档交付，不写码）。
> 关联文献：Memory Transfer Learning（arXiv:2604.14004）、When CL Moves to Memory
> （arXiv:2604.27003）、Can Past Experience Accelerate LLM Reasoning?（arXiv:2505.20643）。
>
> **状态（2026-08-21）**：本文档为 Issue 268 实现规格，先审后写；依团队决策本轮仅文档交付。

## 1. 目标与范围

**核心目标**：确立「类型即抽象层级代理（P/C 高、T/R 低）」为架构原则，并规定**跨项目（User
scope）复用仅允许高层抽象**、**低层原始轨迹（T 工具序列 / R 原始结果）不入 User scope**——防
负迁移（跨项目场景相似度先天不足，低层过度特化的原始轨迹复用易伤害新任务）。

**范围内**：

- 在架构文档（`docs/Agent-Infra-架构设计.md`）写入「类型即抽象层级」原则与「T/R 不入 User
  scope」规则。
- 本 spec 给出原则依据、现状核实、未来落点与验收思路。

**不在范围内**（后续单元，本 PR 不实现代码）：

- **准入矩阵实现**：经核实当前**无任何非测试代码把 T/R 写入 User scope**（`kernel/slice/
  extractor.go` 只产 Project scope 的切片；`lookup`/gateway 是只读；`ingest` 用调用方 scope），
  MTD 防线无处可施——不写运行时准入过滤，待「export/import / Agent KB」跨项目通路出现时再落地。
- **注入侧差异化**（跨 scope T/R 只能进 Grey）：依赖 per-type 阈值 RFC，不随本 PR。
- **评分侧 scope-mismatch 折扣**：依赖 #219 评分器落地 main（当前 `score.go` 缺席 main），不随本 PR。

## 2. 现状核实（2026-08-21）

- `Scope`（Session/Project/User，`kernel/slice/slice.go:21-31`）只做**检索隔离分桶**
  （`kernel/bm25` 每 scope 独立倒排、`lookup` 按调用方 scope 过滤），**无跨域折扣、无按类型的
  scope 准入差异**。
- `extract` 产出 P/T/R 均写 **Project scope**（`extractor.go` newSlice(…, Project, …)），
  **从不产出 User scope 切片**；`lookup` 只读检索；`ingest` 用调用方 scope。→ 当前无 T/R
  User-scope 泄漏路径，规则属前瞻防御。
- 抽象层级概念在仓库中不存在（无 `abstraction` 字段）；`SliceType`（P/C/T/R/M）是直接可用的
  「类型即层级」代理。

## 3. 设计（架构原则）

### 3.1 类型即抽象层级代理

| 层级 | 类型 | 跨项目（User scope）复用 |
|---|---|---|
| 高 | P-Slice 提示词 / M-Slice 记忆 | **允许**（可跨用户库复用，无限制） |
| 高 | C-Slice 上下文 | **允许**（项目结构摘要等元知识跨项目安全、有用） |
| 低 | T-Slice 工具序列（行为模式） | **禁入** User scope（过度特化，跨项目易负迁移） |
| 低 | R-Slice 结果/回复 | **禁入** User scope（原始轨迹，越项目错配） |

「类型」是当前唯一可用的抽象层级代理；未来若引入显式 `abstraction` 字段再升级为按该字段判。

### 3.2 准入规则（未来落点）

当「写 User scope」的真实通路出现（export/import / Agent KB 跨项目共享）时，按以下规则落地：

1. **写入准入门禁**：T/R 类型切片在进入 User scope 前强制降为 Project scope（或拒绝）；
   P/C/M 不受限。零新字段——仅一条「类型 × scope」准入矩阵。
2. **注入侧差异化**（配合 per-type 阈值 RFC）：跨 scope（User→Project 检索）命中的 T/R 只能进
   Grey（judge 确认）不可直通 Hit。
3. **评分侧折扣**（#219 评分器落地后）：评分器加 scope-mismatch 折扣项，仅作用于 T/R。

### 3.3 与现有行为的一致性

- extract 当前已天然满足（只产 Project scope），规则不改变现有 extract 输出。
- bm25/lookup 的 scope 隔离保持不变；准入矩阵仅作用于「写入路径」，不动检索路径。

## 4. 验收思路（未来落地时）

- [ ] 引入 User scope 写入通路后：单测断言 T/R 无法进入 User scope（被降级/拒绝）。
- [ ] 跨项目回放对照：开/关准入矩阵，跨项目误命中（复用了别的项目结果）计数下降。
- [ ] 架构文档与 spec 已写明「类型即抽象层级 + T/R 不入 User scope」原则。

## 5. 后续候选（不在本轮）

- export/import 命令（将打开 User scope 写入通路，届时需本 spec §3.2 的准入落地）。
- Agent KB / 跨项目共享方向。
- 显式 `abstraction` 字段（若未来要更细粒度层级，从类型代理升级）。
