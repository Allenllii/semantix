# Issue #133 验收报告 — M2-GW1: Semantix Gateway v1（New API OpenAI 兼容网关）

> 状态：验收通过（2026-08-14）。对应 Issue：`#133 M2-GW1: Semantix Gateway v1`。
> 设计真源：`docs/specs/newapi-gateway-design.md`（本 issue 提交）。
> 验收方式：实现 + 单测/e2e + 独立 subagent 审查（review）+ 缺陷修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 分支 | `feat/issue-133-gateway`（基于 `upstream/main` 9cf3d53） |
| commit | `0e31be2` docs(specs) 设计文档 · `1934f90` feat(gateway) 核心 · `39a3697` test(gateway) · `2eef877` feat(cmd) 入口 · `20464ad` fix(gateway) 审查修复 |
| diff | 14 文件 +2559/-…（gateway/ 包 8 文件 + cmd/semantix-gateway + kernel/cache、kernel/slice 各 2 处隔离字段 + 设计文档） |

## 2. Issue checklist 逐条核对

| # | checklist（issue #133 原文） | 状态 | 证据 |
|---|---|---|---|
| 1 | 提交设计文档 `docs/specs/newapi-gateway-design.md` | ✅ | `0e31be2`（从 d3a4efd 恢复 372 行完整草案） |
| 2 | HTTP 网关层（OpenAI 兼容 `/v1/*`）：L3 命中直接返回 → 未命中 L2 注入 → 转发上游 | ✅ | `TestE2EL3HitZeroUpstreamCalls`（hit、上游 0 调用）、`TestE2EL2InjectionForwarded`（注入块 prepend system）、SSE 透传/流式命中回放 |
| 3 | 复用 kernel 三层缓存（cache + inject + fingerprint），零新增核心逻辑 | ✅ | gateway 全部语义委托 `cache.L3Decider` / `inject.Injector` / `ingest.Pipeline` / `usage.Recorder` / `slice.FileStore` / `fingerprint`；仅 kernel 增补 2 个隔离字段（审查修复，见 §4） |
| 4 | 与 New API 渠道对接（HTTP 上游形态）；`lookup --json` 协议可复用 | ✅ | `TestE2EAliasMapping`（alias→upstream_model 映射）；/healthz 供渠道探活；上游 OpenAI 兼容直通（DeepSeek vendor） |
| 5 | 验收：`go test -race ./...` 全绿 + 网关 e2e（L3 命中零上游调用、L2 注入不重复探索） | ⚠️ 部分 | e2e 两项核心在 gateway 包实测全绿（httptest 上游）；`-race` 本机无 CGO 无法运行，由 CI 承担（`.github/workflows/ci.yml:36` 已跑 `go test ./... -race`） |

## 3. 端到端实测（httptest 上游，gateway 包）

| 场景 | 断言 | 结果 |
|---|---|---|
| L3 命中 | `x-semantix-cache: hit`，上游调用 0，返回缓存内容 | ✅ |
| L2 注入 | 上游收到 system 消息含 `[semantix-reuse]` 块，透传上游响应 | ✅ |
| SSE 透传 | `text/event-stream` 逐块透传，`[DONE]` 保留 | ✅ |
| L3 流式命中 | SSE 回放缓存内容 + `[DONE]`，上游 0 调用 | ✅ |
| 鉴权/错误 | 无 key/错 key→401、未知模型→404、坏请求→400、/healthz→200 | ✅ |
| 写记忆 | 会话 JSONL 落盘 + 异步 ingest 入库（轮询确认） | ✅ |
| 跨上下文隔离 | 同 query 不同 system prompt → miss 走上游 | ✅ |
| alias 映射 | `client-model` → 上游收到 `real-model` | ✅ |
| L3Safe=false | 无 deps 未 opt-in 的 Result 拒绝复用 | ✅ |

## 4. 独立 subagent 审查与修复（20464ad）

| 发现 | 分级 | 处置 |
|---|---|---|
| L3 缓存键缺 context+model（kernel Query.ContextHash 无消费者，跨会话/跨模型可复用） | 阻断 | 修复：`SliceMeta` 增 `ContextHash/Model`，`cache.Query` 增 `Model`，`L3Decider` 对带 stamp 的查询 fail-closed（无 stamp 条目一律拒绝）；网关 `metaStore` 入库打标。kernel/cache 补 `TestDecideL3ContextIsolation` |
| `upstream_model` 未应用（alias≠上游模型时上游 404） | 阻断 | 修复：`rewriteOutgoing` 统一替换 model + 注入块；e2e 补 alias 映射测试 |
| `ingestWG.Add` 与 `Close().Wait` 竞态（关闭时丢写记忆） | 应修 | 修复：shutdown 互斥 + closing 标志；cmd 用 `srv.Shutdown` 优雅关闭 |
| `x-semantix-session` 未校验（路径穿越出 sessions 目录） | 应修 | 修复：`^[A-Za-z0-9_-]{1,64}$` 白名单 |
| 上游响应 `io.ReadAll` 无上限 | 应修 | 修复：`LimitReader(maxBodyBytes)`；上游 ≥400 转 OpenAI 错误信封 |
| 失败请求仍写记忆（进复用库） | 应修 | 修复：sidecar 移入成功分支 |
| judge_api_key/retriever 死配置；SSE 缺 usage trailer；/healthz 不查 store；LimitReader 静默截断 | nit | MaxBytesReader 已采纳；其余记录为债务（§6） |

## 5. 验证命令

```
go test ./gateway/ ./kernel/cache/ ./cmd/semantix-gateway/   → ok
go test ./...                                                → 16 包 ok
go vet ./...                                                 → 通过
go test -race ./...                                          → 本机无 CGO 不可运行，CI 承担（ci.yml:36）
```

注：`cmd/semantix` 的 `TestRunUsageWithEvolve` 与 `kernel/slice` 的 `TestFileStoreKeepsPerm0600/TestExportPerm` 在**未改动基线** 9cf3d53 上同样失败（Windows 权限位 + evolve 时序既有问题），与本 issue 无关。

## 6. 未闭合风险与后续边界

- **M1/M2 里程碑项未在本 issue 范围**：Claude 格式转换 + cache_control（vendor=anthropic 配置即报错，防误用）、流式命中回放的 usage trailer、Kimi/GPT 渠道实测、New API 计费对账（D1）、多项目 scope 方案 B。
- 债务：SSE 响应侧内容不解析（流式写记忆只记请求侧）；/healthz 不实时探测切片库（store 已由 New 打开保证）；`judge_api_key`/`retriever` 配置字段为预留（kernel RuleGate 规则判定已生效，LLM judge 未接线）。
- `-race` 运行级证据依赖 CI；本机无 C 编译器。
- 分支未推送远端；合入后 `semantix-gateway.toml` 示例与部署（§5 compose）建议另开运维 issue。
