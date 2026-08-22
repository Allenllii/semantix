# Spec：类型化片段确定性上下文淘汰（Beyond Compaction）（Issue 277）

> 对应：`docs/Agent-Infra-架构设计.md` §3.2 流水线 ⑤淘汰器、§8「越用越准而非越用
> 越大」；文献：Beyond Compaction（arXiv:2606.11213）、Context Engineering Survey
> （arXiv:2507.13334）、Useful Memories Become Faulty（arXiv:2605.12978，反面参照）。
>
> 姊妹 spec：[slice-value-eviction.md](slice-value-eviction.md)（#219 评分器/淘汰器，
> 本 spec 的**基线**——已随本分支 merge 落地，见 §2）。
>
> **状态（2026-08-21）**：本文档为实现规格，先审后写。判级：Spec-Required
> （淘汰排序契约变更 + 事件 payload 扩展）。

## 1. 目标与范围

**目标**：长会话/大库上下文淘汰从「摘要式 compaction」路线转向「类型化片段 +
确定性（无 LLM）分级」路线。在 #219 的价值淘汰器（评分 + 按价值升序淘汰）之上
加入**类型维度**：不同切片类型有不同保留优先级——Result/ToolPattern 易过时
优先淘汰，Prompt/Context 项目知识耐淘汰。淘汰顺序**确定、无 LLM、逐字节可复现**。

**范围内**：`kernel/slice` 淘汰排序插入类型优先级键；`GCResult` 增加类型分布
`EvictedByType`；`kernel/event.CompactPayload` 扩展 `evicted_by_type` 字段（wire
向后兼容）；gateway 启动淘汰发射 `Compact` 事件；gc CLI JSON envelope 增加
`evicted_by_type`；相关测试与文档同步。

**不在范围**：摘要式 LLM compaction（harness `compact.go` 一行不动——四大缺陷 +
破坏字节稳定，issue 明确不做）；类型优先级配置化（v1 内置固定表，见 §7 取舍）；
按 scope 淘汰；修改 `Weight` 语义（类型维度独立于评分，见 §7）。

## 2. 现状与依赖

- **#219 已随本分支落地**（merge upstream/feat/slice-value-eviction）：评分器
  `ComputeWeight`（kernel/slice/score.go）、GC 升级为评分/淘汰流水线（上限 cap
  淘汰 + 归档 + 确定性四元组排序）、命中回写四挂点、配置面。本 spec 消费其
  `GCOptions.MaxSlices` 淘汰路径与 `GCResult`。
- **`kernel/event.Compact` 事件**（`event.go:38`，payload `CompactPayload{trigger,
  before_tokens, after_tokens}`）存在但**零消费者**（docs/events.md §4 已注记）。
  本 spec 把它接上类型化淘汰策略：扩展 payload 后由 gateway 启动淘汰发射
  （§6）。kernel/slice 保持纯库函数、不发射事件（与 #219 一致）。
- **harness 侧摘要式 compaction 确认存在**（harness/agent/compact.go）——它是
  issue 的反面对照，不在本 spec 范围。

## 3. 类型优先级表（v1 内置固定表）

淘汰优先级按「先淘汰者在前」定义，数值越小越先淘汰：

| SliceType | 优先级 | 理由 |
|---|---|---|
| `result` | 0 | 工具输出/答案，时效性最强，环境一变即过时 |
| `tool_pattern` | 1 | 行为序列，依赖具体环境与工具集 |
| `memory` | 2 | 用户记忆，中等耐淘汰 |
| `prompt` | 3 | 任务模板，复用价值高 |
| `context` | 4 | 项目知识，最耐淘汰 |
| 未知/未来类型 | 5（最保守） | 不认识的类型永不优先淘汰，防止误删 |

- 接口：`func EvictPriorityOf(t SliceType) int`（kernel/slice，导出供测试与
  未来消费者）；未知类型返回最保守档（= 最后淘汰）。
- 表为**内置常量**，v1 不做配置旋钮（见 §7）。

## 4. 确定性五元组排序（核心变更）

#219 的 cap 淘汰排序（四元组）升级为五元组，类型键插入 grace 之后、Weight 之前：

```
淘汰序（先淘汰者在前）：
  ① graceProtected 升序      // 未受 grace 保护的先淘汰（原规则不变）
  ② EvictPriorityOf(Type) 升序  // ★ 本 spec 新增：类型维度
  ③ Weight 升序               // 原规则不变
  ④ CreatedAt 升序            // 0（unknown）视为最老（原规则不变）
  ⑤ ID 升序                   // 终极平局，逐字节可复现（原规则不变）
```

- **同类型切片之间**排序与 #219 完全一致（类型键相同 → 退化回四元组），
  既有 #219 测试的 fixture（单类型库）不回归。
- **确定性不变量**：同库 + 同 `Now` + 同输入 → 淘汰序与归档逐字节一致。
- 阈值判据（retention / min-weight）与豁免规则**不引入类型维度**——类型只
  影响 cap 相对挤出，不影响绝对阈值判断。

## 5. 可观测性

- `GCResult` 增加 `EvictedByType map[string]int`：按类型 wire 名（`result`、
  `tool_pattern`、`memory`、`prompt`、`context`）统计本次 pass 淘汰的切片数；
  无淘汰时为 nil（沿用空数组非 nil 惯例于 JSON 面处理）。
- gc CLI `--json` envelope 增加 `evicted_by_type`（空 map 序列化为 `{}`，
  与 `evicted` 列表并存）；文本输出追加 `by_type=result:2,prompt:1` 摘要。
- 统计口径：**实际淘汰（含 dry-run 计划）的 OverCap 切片**按类型计数——
  与 `evicted` 列表同源，不把 retention/low-score 混入。

## 6. Compact 事件联动

- **payload 扩展**（wire 向后兼容，只加字段）：

```go
type CompactPayload struct {
	Trigger string `json:"trigger"` // 新增合法值 "evict"（库级淘汰）
	Before  int    `json:"before_tokens"`
	After   int    `json:"after_tokens"`
	// EvictedByType 按类型 wire 名统计本次淘汰的切片数；仅 trigger="evict" 时设置。
	// 对 "evict"，Before/After 是切片条目数而非 token（字段名保持 wire 稳定）。
	EvictedByType map[string]int `json:"evicted_by_type,omitempty"`
}
```

- **发射点**：gateway 启动淘汰（gateway.go 的 `slice.GC` 调用处）非 dry-run 且
  `Removed > 0` 时，通过 `recordKernelEvent` 发射 `event.Compact`，sessionID 用
  固定 `"maintenance"`（库级事件载体，注释说明非会话事件；不匹配任何真实会话）。
  best-effort：发射失败仅 log，不影响启动。
- **CLI gc 不发射事件**：cmd 层无 kernel/event 基础设施，JSON envelope 已承载
  同等信息（§5），不为此引入新写路径。
- **零消费者现状不变**：docs/events.md 维持「无消费者」注记，事件为未来
  观测/审计消费者就绪。

## 7. 设计取舍

- **类型维度放排序键、不放评分器**（有意偏离 issue 建议的「评分器加类型维度
  权重」）：`Weight` 是持久化价值字段且被 `--min-weight` 消费，混入类型偏好会
  让「价值」语义失真；排序键方案调整类型策略**无需重评分全库**（排序时读
  `Type` 字段），且反自我强化不变量（检索/注入永不读 Weight）不受影响。
- **优先级 v1 不可配置**：固定表 + 单测锁定 = 确定性的最强保证；配置旋钮等
  有实测数据支撑的调优需求出现时再加（避免选项剧场）。
- **未知类型最保守**：淘汰是数据销毁（归档可还原但仍需谨慎），不认识的类型
  最后淘汰。
- **事件发射在 gateway 而不在 kernel/slice**：与 #219 一致——kernel/slice 是
  纯库，事件是调用层职责；gateway 已有 `recordKernelEvent` 写路径。

## 8. 测试计划

- `kernel/slice`：类型优先级映射单测（含未知类型最保守）；五元组排序单测
  （混合类型同 Weight → Result/ToolPattern 先于 Prompt/Context 淘汰）；确定性
  单测（同库同 Now 两次 GC → 淘汰序与归档逐字节一致）；EvictedByType 计数
  正确性（仅 OverCap、dry-run 也统计）；同类型库行为与 #219 逐字节一致
  （回归锚点）。
- `kernel/event`：CompactPayload 新字段 round-trip（ToJSON/FromJSON）。
- `gateway`：启动淘汰 fixture（混合类型超上限库）→ 淘汰后 `maintenance.jsonl`
  出现 trigger="evict" 的 Compact 事件且 EvictedByType 正确；best-effort 失败
  不影响启动。
- `cmd/semantix`：gc --json envelope 含 evicted_by_type；文本输出 by_type 摘要。
- 既有 #219 evict 测试全部保持绿（同类型 fixture 排序不变）。

## 9. 验收标准

- 混合类型库 cap 淘汰：同 Weight 下 Result/ToolPattern 先于 Prompt/Context；
- 确定性：同库同 Now 两次 gc 产出逐字节相同的淘汰序与归档；
- 未知类型切片最后淘汰；
- `gc --json` 报 `evicted_by_type`；gateway 启动淘汰发射 `Compact(trigger=evict)`
  事件且 payload 类型分布正确；
- 全量测试绿（kernel/gateway/cmd），#219 既有测试不回归；
- 文档同步：README 状态行、架构 §3.2、docs/events.md（Compact 契约）、
  QUICKSTART gc 输出说明。

## 10. 后续入口（非本轮）

- 长会话任务精度对照（TraceLab 负载：类型化淘汰 vs 均一淘汰）——评测型验收，
  依赖评测设施，单列。
- 类型优先级调参数据（若淘汰分布异常可考虑配置化）。
