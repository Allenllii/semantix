# U27 serve/watch 常驻模式：触发条件评估报告

> 日期：2026-08-14 · 状态：**已关闭（评估未触发 → 2026-08-14 关闭 #121，stateReason: not planned）** · 对应：M2 / U27（Issue #121，重复登记 #132）
> 结论先行：**三项触发信号均未满足 → 按 #121 验收标准第 4 条「关闭本 issue 并记录理由（不实现）」处理；serve/watch 不实施，保持子进程协议。**

---

## 1. 背景

H1 子进程协议（`docs/reports/h1-mount-design.md`）已满足 harness 闭环需求：每次 `semantix lookup --json` / `semantix inject` 独立子进程调用，3s 超时 + 失败静默降级，kernel 故障不影响 harness 主流程。

`docs/reports/cli-v2-architecture.md` §6 明确 serve/watch 为 **P2 可选**，且写明「**默认不做，保持简单**」，仅在以下信号出现后启动：

| # | 触发信号 | 结论 |
|---|---|---|
| ① | harness 单会话内 lookup/inject 调用 > 5 次（启动开销可测） | 未满足 |
| ② | 需要跨进程共享内存索引（embedding 缓存） | 未满足 |
| ③ | 团队需要实时事件流（usage/evolution）做仪表盘 | 未满足 |

以下逐项给出证据。

---

## 2. 触发信号 ①：单会话 lookup/inject 调用 > 5 次 —— 未满足

### 2.1 inject：每会话恰好 1 次（挂在 systemPrompt 一次性组装点）

- H1 挂载设计（`docs/reports/h1-mount-design.md` U8a）：inject 挂在 fork 的 `internal/agent/agent.go:2641 systemPrompt()` 末尾，config 开关 `semantix.inject` 开启时注入。
- Semantix 的 system prompt **在 boot 一次性组装且不再变**（`docs/reports/semantix-kvcache-mechanisms.md` 机制①：`internal/boot/boot.go:547-640`；`boot_test.go:137-139` 断言 base 保持缓存前缀稳定）→ 整个会话只触发 **1 次** inject，无 per-turn 重复。
- 会话中新增内容（记忆更新/任务完成/hook/召回）全部注入**当前用户消息尾部**，绝不改 system prompt（`internal/control/input.go:174-214`）→ 不会因会话增长而增加 inject 次数。

### 2.2 lookup：模型按需调用，无强制频次

- H1 挂载设计（U8b）：`semantix_lookup` 工具注册在 `internal/tool/tool.go:250 RegisterBuiltin`，模型自主决定是否调用；文档定位为「新任务开始时，判断是否做过类似事」（`agent-skill/SKILL.md` 工具调用规范）。
- 仓库中**没有任何实测数据、日志或文档证据**显示单会话 lookup 调用 > 5 次；无 telemetry 表明子进程调用成为可测痛点。

### 2.3 即使达到 5 次，开销也可忽略

- H1 风险节：「子进程调用 kernel 有 ~50ms 启动开销（Go 二进制）——注入/检索都在 turn 边界，可接受」。
- 5 次 × ~50ms ≈ **250ms/会话**，且分散在 turn 边界（模型思考/推理期间），不阻塞用户可感知路径 → 不构成「启动开销可测」的触发证据。

---

## 3. 触发信号 ②：跨进程共享内存索引（embedding 缓存）—— 未满足

- 当前 kernel 检索用确定性 `HashEmbedder`（`semantix.example.toml` `[retrieval] vector_dim = 256`，注释「HashEmbedder 维度」），无模型级 embedding 缓存需要跨进程共享；哈希 embedding 可廉价按需重建。
- `lookup` 每次从持久 store 重建会话本地索引：`cmd/semantix/lookup.go:51-53`（`indexFromStore`）→ 当前库规模下代价可接受，无共享索引需求方。
- 仓库无任何 issue / 文档 / 需求方提出需要跨进程共享索引。

---

## 4. 触发信号 ③：实时事件流（usage/evolution 仪表盘）—— 未满足

- `usage` 是批处理命令（读 `usage.jsonl` 输出汇总，`cmd/semantix/usage.go`），`evolve` 为参数快照（`kernel/evolve`），均为按需查询，无实时订阅消费者。
- 仓库无仪表盘需求记录：无 issue、无文档、无客户需求方提出实时事件流/仪表盘要求。

---

## 5. 依赖说明：U22 评估时未落地，后已合入（不改变触发结论）

- #121 声明依赖 **U22（--json 结构化输出，统一信封 §4.2）**；**评估时**（2026-08-14）U22 对应 #116 / #126 均 open、未实现，`lookup` / `search` 输出为裸 JSON，无 §4.2 的 `{"ok","command","data","error","version"}` 信封 → serve 的 JSON-RPC 子集没有稳定契约可对齐。
- **评估后**：U22 已随 PR #144（`ec9634f`，M2-U20/U22）合入 main。依赖只影响「能否实施」，不影响「是否应触发」——三项触发信号（§2–§4）仍未出现，故结论不变：**不实施，关闭 #121**。

---

## 6. 结论与后续动作

**结论**：三项触发信号均未满足 → 按 #121 验收标准第 4 条，**关闭本 issue 并记录理由（不实现）**；与 `cli-v2-architecture.md` §6「默认不做，保持简单」一致。U22 依赖后已落地（§5），仅解除实施阻塞，不改变触发结论。

**后续动作**：
1. ✅ **2026-08-14 已关闭 #121**（stateReason: not planned，关闭评论引用本报告）。
2. **#132**（同一 U27 的重复登记，含完整 checklist「触发信号出现后再开工」）保留为后续跟踪项，工作不丢失。
3. 若未来出现以下任一信号，在 #132 重新开工：
   - 实测 harness 单会话 lookup/inject 调用 > 5 次（先补 telemetry/日志统计）；
   - 引入需跨进程共享的模型级 embedding 缓存；
   - 出现 usage/evolution 实时仪表盘需求。
