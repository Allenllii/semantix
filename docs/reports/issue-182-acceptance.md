# Issue #182 验收报告 — GW2: 流式响应侧写记忆（真实流式流量积累 L3 切片）

> 状态：验收通过（2026-08-17）。对应 Issue：`#182 GW2: 流式响应侧写记忆`（Spec-Exempt，实现已批准 spec §3.7/§3.4 既有条目）。
> 设计真源：`docs/specs/newapi-gateway-design.md`（GitHub main 版 §0.3 documented debt / §3.4 流式透传 / §3.7 会话旁路写记忆）；实现规格 = issue #182 上的 spec post（评论 `#issuecomment-5312381566`，用户审后批准）。
> 验收方式：实现 + 单测/e2e + 独立 subagent 审查（review）+ 缺陷修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 范围 | `gateway/sse.go`（新增，SSE 聚合器）+ `gateway/sse_test.go`（新增，11 个单测）+ `gateway/pipeline.go`（`streamThrough` 改造）+ `gateway/e2e_test.go`（新增 5 个 e2e + 测试设施扩展） |
| diff | 4 文件 +430/-9（`gateway/gateway.go`、配置、旁路格式、`turns`/`recordSession`/`ingestSession` 零改动） |
| 未提交 | 工作区未提交（待用户确认后提交/推送） |

## 2. Issue checklist 逐条核对

| # | checklist（issue #182 原文） | 状态 | 证据 |
|---|---|---|---|
| 1 | `streamThrough` 边透传边聚合 SSE `choices[].delta.content`（含 role 首块 / finish_reason 终止） | ✅ | `gateway/sse.go` `sseAggregator`（按行解析、data 负载、`[DONE]`/`finish_reason` 终止、1 MiB 聚合上限 + 64 KiB 行上限）；`TestE2EStreamSidecarAggregatesContent` + `TestSSEAggregatorFullStream/SplitFeeds` |
| 2 | 聚合出的 assistant 消息补进旁路文件（`recordSession` 响应侧），与非流式路径同构 | ✅ | `pipeline.go:222-224` 完整时 `turns(req, agg.Content())`，`turns`/`recordSession` 零改动；`TestE2EStreamSidecarAggregatesContent` 断言旁路末行 `{role:assistant, content:拼接全文}` |
| 3 | tool_calls 流式块的聚合与跳过策略与非流式一致（Result 提取取最后一条无 tool_calls 的 assistant） | ✅ | 聚合仅取 `choices[0].delta.content`，tool_calls delta 块跳过（与非流式 `extractAssistantContent` 只读 `message.content` 同构）；`TestE2EStreamSidecarSkipsToolCalls` 断言旁路无 `tool_calls` 字段 |
| 4 | 上游异常断流：已聚合的部分内容**不入库**（半截回复不可复用，fail-closed） | ✅ | `Complete()` = 终止标志 ∧ 未超限，否则跳过 `recordSession`；`TestE2EStreamAbortFailClosed`（Hijack 中途断连）+ `TestE2EStreamNoTerminatorFailClosed`（EOF 无终止信号）+ `TestSSEAggregatorNoTerminator/DoneWithTrailingSpace/EmptyFinishReason/ContentOverflow/LineOverflow` |
| 5 | e2e：流式请求 → 旁路文件含完整 assistant → ingest 后 `semantix search` 可检索 → 二次同 query L3 命中（假上游） | ✅ | `TestE2EStreamAccumulatesL3`：真 ingest 管线 → store 出现 Result 切片 → 二次同 body 请求 `x-semantix-cache: hit` + 上游调用数保持 1（gateway 无 search 端点，检索经 `L3Decider.DecideL3` 内部 `Index.Search` 触发，与 e2e 同构） |
| 6 | `go test ./gateway/... -race` 全绿 | ⚠️ 部分 | 本机无 CGO（无 gcc）无法运行 `-race`；`go test ./gateway/... -count=1` 本机全绿，`-race` 由 CI 承担（`.github/workflows/ci.yml:36` 已跑 `go test ./... -race`）——与 issue-133 验收报告 §5 的既有记录一致 |

## 3. 端到端实测（httptest 假上游，gateway 包）

| 场景 | 断言 | 结果 |
|---|---|---|
| 流式完整聚合（role 首块 + 3 段 content + finish_reason + [DONE]） | 旁路末行 `{role:assistant, content:"hello streaming world"}`；SSE 逐字节透传不变 | ✅ |
| 流式 tool_calls delta 跳过 | 旁路 assistant 行仅含纯文本拼接、无 `tool_calls` 键 | ✅ |
| 上游中途断连（Hijack 关连接） | 旁路文件不存在（半截内容不入库） | ✅ |
| 上游 EOF 但无终止信号 | 旁路文件不存在（fail-closed） | ✅ |
| 流式积累 → L3 二次命中 | 真 ingest 后二次同 query：`x-semantix-cache: hit`、上游调用数 1（不增长）、流式回放含完整内容 | ✅ |
| 既有回归 | `TestE2EStreamPassthrough` / `TestE2ESidecarWrittenAndIngested` / `TestE2EL3*` / kernel 16 包 | ✅ |

## 4. 独立 subagent 审查与修复

审查结论：核心逻辑正确，无阻断问题；2 项应修 + 1 项 nit 已全部处理。

| 发现 | 分级 | 处置 |
|---|---|---|
| `sseAggregator` 零单测，跨块行缓冲是核心新逻辑 | 应修 | 新增 `gateway/sse_test.go` 11 个单测（跨块切分 / CRLF / 多行 data / 坏 JSON / 多 choice / [DONE] 尾随空格 / 空 finish_reason / content 超限 / 行超限） |
| `lineBuf` 无上限，与"内存有界"承诺矛盾（恶意上游无限不换行数据 → 无界增长） | 应修 | 新增 `maxSSELineBytes = 64 KiB` 行上限，超限置 overflow fail-closed（`sse.go` `Feed`） |
| `finish_reason` 空串也触发终止判定 | nit | 改为 `*first.FinishReason != ""` 才置 done（`sse.go` `flushEvent`） |
| overflow fail-closed 分支无测试 | 应修 | `TestSSEAggregatorContentOverflow` / `TestSSEAggregatorLineOverflow` 覆盖 |

复验：修复后 `go test ./gateway/... -count=1` + `go vet ./gateway/...` 全绿。

## 5. 验证命令

```bash
go build ./...                    # ✅
go vet ./gateway/...              # ✅ 无警告
go test ./gateway/... -count=1    # ✅ 全部通过（含 11 个新单测 + 5 个新 e2e）
go test ./kernel/... -count=1     # ✅ 16 包全绿
go test ./gateway/... -race       # ⚠️ 本机无 CGO（gcc 不存在）不可运行，CI 承担
```

## 6. 未闭合风险与后续边界

| 项 | 说明 |
|---|---|
| L3 命中受 zone 绝对地板约束（实测发现） | 真 ingest 会同时产生 Prompt 切片（user 消息副本，占据 BM25 top1）。短 query（≤2 词）下 BM25 绝对分数 < `AbsHigh(0.7)` → 该检索整体 fail-closed（Grey），L3 不命中。这是 kernel zone 的既有 fail-closed 设计（U16 验收），非本 issue 引入；e2e 用多词 query 覆盖了可命中路径。真实流量下短 query 的 L3 命中率影响由 GW4 M1 门实测评估 |
| `-race` 未在本机验证 | 无 CGO 环境限制（项目既有记录），CI `go test ./... -race` 承担 |
| 请求侧 tool_calls 旁路保留（既有债务） | `turns` 不写请求侧 tool_calls 字段（非流式同构的既有行为），网关链路 ToolPattern 切片提取仍失效——非本 issue 范围，另开 issue |
| usage 计量 | 流式 usage 补块 / token 口径属 GW7 #187（依赖本条，先后合入），本条未动 usage |
| GW4 #184 | M1 验收门（真实流量二次命中 + 成本节省 ≥30%）依赖本条落地后的真实链路验证 |
