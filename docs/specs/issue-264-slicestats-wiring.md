# Spec：SliceStats 命中/注入统计回写与事件发射（Issue 264）

> 对应 Issue：#264  
> 实现：PR #303（已合并）  
> 状态：Implemented

## 1. 目标

为项目切片的 `SliceStats` 补上真实运行时生产者，使后续评分、遗忘和淘汰逻辑能够消费实际的
`Hits`、`Injected` 与 `LastUsed`，同时把相同事实写成既有的 `SliceHit` / `SliceInject`
内核事件。

本规格只定义三个有真实切片 ID 的产生点：

1. gateway L3 结果复用；
2. gateway L2 上下文注入；
3. harness L2 上下文注入。

## 2. 统一统计口径

所有统计通过 `slice.ApplyStats` 写入，优先使用 store 的批量能力，并复用 `mergeStats` 的累加和
`LastUsed` 取最大值语义。

| 产生点 | 统计增量 | 事件 |
| --- | --- | --- |
| gateway L3 复用 | `Hits += 1`，`LastUsed = now` | `SliceHit{Layer:"L3", SliceIDs:[id]}` |
| gateway L2 注入 | 每个注入 ID：`Injected += 1`，`LastUsed = now` | `SliceInject{SliceIDs:[...], Bytes:n}` |
| harness L2 注入 | 每个注入 ID：`Injected += 1`，`LastUsed = now` | `SliceInject{SliceIDs:[...], Bytes:n}` |

L2 注入只表示切片进入了发送给模型的上下文，不代表模型实际采用，因此不能增加 `Hits`。
空 ID 和已不存在的切片按 `ApplyStats` 的 best-effort 契约跳过，不阻断请求。

## 3. Gateway 实现

`gateway/pipeline.go` 在两个确定的运行点记录事实：

- L3 候选通过全部复用门后，记录一次命中；
- L2 `injector.Build` 返回的切片实际进入注入块后，记录一次注入。

统计由 `Gateway.recordSliceStats` 放入现有异步生命周期：任务不占用 HTTP 热路径，
`Gateway.Close` 会等待已接受的任务完成。事件由 `recordSliceEvent` 写入同一 session sidecar，
保留 `session_id`、context hash 和 model 元数据，不需要新增第二套事件总线配置。

## 4. Harness 实现

`Bridge.InjectDetailed` 在得到稳定的注入文本和规范化 targets 后调用内部 `recordInjection`。
调用者无需再执行第二个公开方法，因此任何使用 `InjectDetailed` 的路径都遵循同一统计口径。

`recordInjection`：

- 复制并保留本次真实注入的 slice IDs；
- 立即向 Bridge 既有 event bus 发射 `SliceInject`，由 `mirrorKernel` 写入 session JSONL；
- 异步打开项目 store 并批量回写 `Injected` / `LastUsed`；
- 通过 Bridge 的关闭状态和 wait group 管理任务，`Close` 等待已接受的回写完成。

## 5. 可检索事件

Gateway 和 harness 都把事件写入会话 JSONL。`kernel/ingest` 保留这些内核事件，extractor 将
`SliceHit`、`SliceInject` 投影为可索引文本，因此既有 ingest/search 管线可以检索这些运行事实。

## 6. 明确不做

- CLI `verify` 回放不回写生产统计：它使用独立 replay store 和合成 `v-` 切片 ID；
- `usage --evolve-db` 不回写 per-slice 统计：usage 记录不含 slice ID；
- 本增量不生产 `Rejected` / `UserFeedback`；
- 本增量不计算 `Weight`，只提供其未来计算需要的输入；
- 不新增公共 Event Bus 配置或手动 `RecordInject` API。

## 7. 验收

- gateway L3 复用后，对应切片的 `Hits == 1` 且 `LastUsed > 0`；
- gateway L2 注入后，对应切片的 `Injected == 1` 且不增加 `Hits`；
- harness `InjectDetailed` 返回 targets 后，关闭 Bridge 前接受的统计全部落盘；
- gateway 和 harness 的 session JSONL 分别包含对应 `SliceHit` / `SliceInject`；
- 事件可经过 ingest/extractor 进入检索语料；
- 统计或事件记录失败遵循 best-effort，不改变主请求结果。

## 8. 回归覆盖

- `gateway/stats_test.go`：gateway L3/L2 的端到端统计与事件落盘；
- `harness/semantix/reuse_test.go`：harness 注入统计与 `SliceInject` sidecar；
- `kernel/ingest/ingest_test.go`、`kernel/slice/slice_test.go`：事件保留与可检索投影。
