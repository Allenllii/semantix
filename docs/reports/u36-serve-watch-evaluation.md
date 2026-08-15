# U36 serve/watch 常驻模式：触发条件评估报告（U27 落地复审）

> 日期：2026-08-14 · 状态：**已评估（未触发 → 关闭 #167，stateReason: not planned）** · 对应：M2 / U36（Issue #167，U27 落地复审）
> 结论先行：**三项触发信号均未满足 → 按 #167 验收标准第 1 条「评估不通过则关闭并记录理由」处理；serve/watch 不实施，保持子进程协议。** 本报告即记录理由。

---

## 1. 背景

- #167 是对 U27（#121，重复登记 #132）的**落地复审**登记：`docs/reports/cli-v2-architecture.md` §6 将 serve/watch 标为 **P2 可选**，且写明「**默认不做，保持简单**」，仅在以下信号出现后启动：

| # | 触发信号 | 结论 |
|---|---|---|
| ① | harness 单会话内 lookup/inject 调用 > 5 次（启动开销可测） | 未满足 |
| ② | 需要跨进程共享内存索引（embedding 缓存） | 未满足 |
| ③ | 团队需要实时事件流（usage/evolution）做仪表盘 | 未满足 |

- 前次评估（`docs/reports/u27-serve-watch-evaluation.md`）已判**未触发**：#121 于 2026-08-14 以 `not planned` 关闭，#132 以 `duplicate` 关闭。
- 自前次评估以来，U22（--json 统一信封，#167 声明依赖）已随 **PR #144** 合入 main（`cmd/semantix/envelope.go`）——依赖只影响「能否实施」，不影响「是否应触发」；本报告按**当前 main**（`fcccc27`）逐项复核，结论不变。

以下逐项给出证据。

---

## 2. 触发信号 ①：单会话 lookup/inject 调用 > 5 次 —— 未满足

- **inject 每会话恰好 1 次**：H1 挂载设计（`docs/reports/h1-mount-design.md` U8a）inject 挂在 fork 的 `systemPrompt()` 末尾、config 开关 `semantix.inject` 开启时注入；system prompt 在 boot 一次性组装且不再变 → 整个会话只触发 1 次 inject，无 per-turn 重复。会话新增内容注入当前用户消息尾部，不改 system prompt。
- **lookup 模型按需调用，无强制频次**：`semantix_lookup` 工具注册在 `kernel/lookup/lookup.go:16`（`ToolName`），CLI 入口 `cmd/semantix/lookup.go:14-29`（H1 harness 协议入口，`--json` 信封）；模型自主决定是否调用，文档定位为「新任务开始时判断是否做过类似事」。
- **无 >5 次/会话的实测证据**：仓库无 telemetry/日志/文档显示单会话 lookup 调用超过 5 次；无需求方报告子进程启动成为可测痛点。
- **新近命令未改变调用模型**：U35 eval（`cmd/semantix/eval.go` / `eval_judge.go`）为进程内 kernel 调用（无 `os/exec`/`exec.Command` 子进程），不产生额外的 H1 子进程调用。
- 即使达到 5 次，开销也可忽略：子进程启动 ~50ms，5 次 ≈ 250ms/会话，且分散在 turn 边界（模型思考/推理期间），不阻塞用户可感知路径。

---

## 3. 触发信号 ②：跨进程共享内存索引（embedding 缓存）—— 未满足

- **默认 embedding 仍为零依赖确定性 HashEmbedder**：`kernel/embed/hash.go`、`cmd/semantix/embedder.go:49-51`（`--embedder hash` 为默认）、`semantix.example.toml:15`（`vector_dim = 256`，注释「HashEmbedder 维度」）→ 哈希向量可廉价按需重建，无跨进程缓存需求。
- **模型级 Embedder 为 opt-in 且 fail-soft**：`kernel/embed/model.go` 需 `SEMANTIX_EMBED_*` 环境变量显式开启，远端失败自动回退 HashEmbedder（fail-soft）——未引入「必须跨进程共享」的模型 embedding 缓存。
- **lookup 每次从持久 store 重建会话本地索引**：`cmd/semantix/lookup.go:52-54`、`159-161`（`indexFromStore`）→ 当前库规模下代价可接受，无共享索引需求方。
- 仓库无任何 issue / 文档 / 需求方提出需要跨进程共享索引。

---

## 4. 触发信号 ③：实时事件流（usage/evolution 仪表盘）—— 未满足

- **U31 dashboard 是批处理 ANSI 快照，非实时订阅**：`cmd/semantix/dashboard.go` 头注释明确「one-screen ANSI snapshot dashboard」，数据源只读（`kernel/usage` / `kernel/zone` / `kernel/slice`），命令内无 `Subscribe` / stream / `fsnotify` / watch 逻辑。
- **`usage` / `evolve` 均为按需批处理查询**：读 `usage.jsonl` 输出汇总 / 参数快照（`kernel/evolve`），无实时订阅消费者。
- **kernel/event 总线为进程内同步总线**：`kernel/event/bus.go:5`（「handlers are invoked synchronously in-process」）——用于 kernel 内部事件接线，不是跨进程订阅流。
- 仓库无仪表盘需求记录：无 issue、无文档、无客户需求方提出实时事件流/仪表盘要求。

---

## 5. 依赖说明：U22 已合入（仅解除实施阻塞，不改变触发结论）

- #167 声明依赖 U22（--json 统一信封 §4.2）；**前次评估时** U22（#116/#126）未实现，无 `{"ok","command","data","error","version"}` 信封。
- **当前**：U22 已随 **PR #144** 合入 main（`cmd/semantix/envelope.go`、`cmd/semantix/json_envelope_test.go`，#116 `completed` 关闭）→ serve 的 JSON-RPC 子集已具备稳定契约可对齐。
- 但依赖只决定「能否实施」：三项触发信号（§2–§4）仍未出现，故触发结论不变：**不实施**。

---

## 6. 结论与后续动作

**结论**：三项触发信号均未满足（U22 已合入仅解除实施阻塞）→ 按 #167 验收标准第 1 条，**关闭 #167 并记录理由（不实现）**；与 `cli-v2-architecture.md` §6「默认不做，保持简单」一致。

**后续动作**：
1. ✅ **2026-08-14 关闭 #167**（stateReason: not planned，关闭评论引用本报告）。
2. 前次评估的跟踪项 #132 已按 `duplicate` 关闭（无 open 跟踪 issue）→ 未来若出现任一触发信号，**新开 issue** 重新评估：
   - 实测 harness 单会话 lookup/inject 调用 > 5 次（先补 telemetry/日志统计）；
   - 引入需跨进程共享的模型级 embedding 缓存；
   - 出现 usage/evolution 实时仪表盘需求。
3. 本报告与 `u27-serve-watch-evaluation.md` 共同构成 serve/watch 的完整评估链（U27 → U36 复审），结论一致。
