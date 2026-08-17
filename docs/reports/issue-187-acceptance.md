# Issue #187 验收报告 — GW7: 网关计量口径收尾——流式 usage 补块 + token 估算口径

> 状态：验收通过（2026-08-17）。对应 Issue：`#187 GW7: 网关计量口径收尾`（Spec-Exempt：① spec §3.4 既有条目；② 口径文档化/实现选择，不新增契约）。
> 设计真源：`docs/specs/newapi-gateway-design.md` §3.4 / §4.3 / §0.3；实现规格 = issue #187 上的 spec post（评论 `#issuecomment-5312963213`）。
> 验收方式：实现 + 单测/e2e + 独立 subagent 审查（review）+ 缺陷修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 分支 | `feat/issue-187-metrics`（基于 GW2 分支 `feat/issue-182-streaming-sidecar`，stacked；GW2 PR #203 先合入后本 PR diff 自动只剩增量） |
| 范围 | `gateway/pipeline.go`（`streamThrough` 补块 + `writeUsageChunk` + `replayStream` + `replyFromCache` + `usagePayload.Estimator`）+ `gateway/sse.go`（`SawUsage`/`Overflowed`/`EventID`）+ `gateway/sse_test.go` + `gateway/e2e_test.go`（6 新用例）+ `docs/specs/newapi-gateway-design.md`（§0.3 两行）+ 验收报告 |
| 未提交 | 工作区未提交（待用户确认后提交/推送/开 PR） |

## 2. Issue checklist 逐条核对

| # | checklist（issue #187 原文） | 状态 | 证据 |
|---|---|---|---|
| 1 | 未命中流式：上游末块无 usage 时，在 `[DONE]` 前补一块含 `prompt_tokens_details.cached_tokens`（注入统计）的 usage 事件 | ✅ | `streamThrough` 在 `[DONE]` 行透传前补发 OpenAI 格式 usage 事件（`choices:[]` + `usage.prompt_tokens_details.cached_tokens=注入量`，id 复用流内首个 chunk id）；结构检测 `agg.SawUsage()` 防重复；`TestE2EStreamUsageChunkSynthesized/NoFinishReason/NotDuplicated` |
| 2 | token 口径：评估引入真 tokenizer 的依赖成本；若不引入，合成 usage 附 `"estimator":"bytes/4"` 字段并写入 spec §0 注记 | ✅ | 评估结论：**不引入**（tiktoken 兼容实现需 BPE 词表依赖面，与零第三方运行依赖铁律冲突；bytes/4 对英文 ±10% 内、对中文偏大，口径差由 estimator 承载）；`usagePayload.Estimator` + 三处合成 usage（非流式命中/流式补块/流式命中回放）全部附 `"estimator":"bytes/4"`；spec §0.3 注记已回写 |
| 3 | e2e：流式无 usage 上游 → 末块补发断言 | ✅ | `TestE2EStreamUsageChunkSynthesized`（注入发生 cached_tokens>0、estimator、顺序）+ `TestE2EStreamUsageChunkNoFinishReason`（无 finish_reason 直接 [DONE] 仍补块） |
| 4 | `go test ./gateway/... -race` 全绿 | ⚠️ 部分 | 本机无 CGO 无法运行 `-race`（项目既有记录），CI 承担；本机 `go test ./gateway/... -count=1` 全绿 |

## 3. 端到端实测（httptest 假上游，gateway 包）

| 场景 | 断言 | 结果 |
|---|---|---|
| 流式无 usage + L2 注入发生 | 补块在 `[DONE]` 前、`estimator:"bytes/4"`、`cached_tokens>0`（注入统计传导） | ✅ |
| 无 finish_reason 直接 `[DONE]`（合法流） | 仍补块（信任 `[DONE]` 即终止，review 修复点）、id 复用 `chatcmpl-abc` | ✅ |
| 上游自带 usage | 原样透传、无 estimator、`"usage"` 计数==1（不重复补） | ✅ |
| 断流（Hijack） | 补 `[DONE]` 但无 usage 块（半截流不计量，fail-closed） | ✅ |
| L3 流式命中回放 | 回放流含合成 usage（estimator + cached_tokens，在 `[DONE]` 前） | ✅ |
| L3 非流式命中 | 合成 usage 含 `estimator:"bytes/4"` | ✅ |
| 聚合器 SawUsage 单测 | 结构检测（带 usage/无 usage/null usage/content 含 "usage" 字样不误判） | ✅ |
| 既有回归 | gateway 全量 + 全仓 18 包全绿；`go vet` 无警告 | ✅ |

## 4. 独立 subagent 审查与修复

审查结论：核心正确性（顺序/透传/estimator 隔离/fail-closed/口径一致）核验通过；1 应修 + 2 nit，应修已处理。

| 发现 | 分级 | 处置 |
|---|---|---|
| `agg.Complete()` 在 `[DONE]` 行时依赖 done 标志，但 done 要到下一空行才置位——上游只发内容块 + `[DONE]`（无 finish_reason，合法）时合成 usage 静默缺失 | 应修 | 补块条件改为 `!agg.SawUsage() && !agg.Overflowed() && sawDone`（信任 `[DONE]` 即完整终止）；补 `TestE2EStreamUsageChunkNoFinishReason` 防回归 |
| 合成 chunk 每次 `randomID()` 生成新 id，与流内 chunk id 不一致，按 id 归并的客户端可能误判新流 | nit | 聚合器捕获首个事件 id（`EventID()`），补块复用；replayStream 用 `base.ID`；无 id 时回退随机 |
| `TestE2EStreamUsageChunkSynthesized` 的 cached_tokens>0 依赖注入命中 | nit | 与既有 `TestE2EL2InjectionForwarded` 同模式，可接受（记录） |

复验：修复后 `go vet ./gateway/...` + 全仓 `go test ./... -count=1`（18 包）全绿。

## 5. 验证命令

```bash
go build ./...                    # ✅
go vet ./gateway/...              # ✅ 无警告
go test ./gateway/... -count=1    # ✅ 全部通过（含 6 个新 GW7 用例）
go test ./... -count=1            # ✅ 18 包全绿
git diff --check                  # ✅
go test ./gateway/... -race       # ⚠️ 本机无 CGO（gcc 不存在）不可运行，CI 承担
```

## 6. 未闭合风险与后续边界

| 项 | 说明 |
|---|---|
| stacked 分支依赖 GW2 合入 | 本分支基于 `feat/issue-182-streaming-sidecar`；PR #203 合入 main 后本 PR diff 自动只剩 GW7 增量；rebase 已处理（两分支均已基于最新 main 5a4a680，mergeable ✅） |
| Windows 文件锁（main 既有问题，非本条引入） | 干净的 upstream/main 上 `cmd/semantix` 22 个测试因 `#204` journal 文件句柄未释放而失败（Windows 专属）；CI（ubuntu）无此问题；与本条改动无关，记录待另开 issue |
| `-race` 未在本机验证 | 无 CGO 环境限制（项目既有记录），CI 承担 |
| token 口径为估算 | 合成 usage 的 token 数是 `len(bytes)/4`，经 `"estimator":"bytes/4"` 显式标注；GW4 对账时按字段识别，不冒充真 tokenizer 计数 |
| 流式补块对客户端的兼容性 | OpenAI 协议标准形态（choices 空数组 + usage），主流客户端忽略或消费；e2e 断言事件顺序与唯一性 |
