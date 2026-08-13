# Spec：fork 端到端闭环验证（Issue 64 / M1-U13b）

> 对应 Issue：`#64 M1-U13b: fork 端到端闭环验证（Reasonix fork 挂载后真实会话跑注入）`
> 真源约束：挂载设计以 `docs/reports/h1-mount-design.md` 为准；fork 侧挂载点
> （U7 `internal/semantix/sink.go` HarnessSink、U8a `systemPrompt()` 注入 hook、
> U8b `internal/tool/semantix.go` semantix_lookup）已合入 fork（Gnosil/DeepSeek-Reasonix @ main），
> 守卫测试（字节稳定性 / cachehit_e2e）已 PASS——本 PR **不改 fork 代码**，只做真实会话验证与报告。
>
> **状态（2026-08-13）**：本文档为 Issue 64 验证规格，先审后跑。

## 1. 目标与范围

**核心目标**：用 fork 的 reasonix 二进制跑真实开发会话（配 `[semantix]` 配置段），
验证 H1 挂载的端到端闭环：**会话 → HarnessSink JSONL → `semantix extract` → 库；
新会话 → `semantix lookup` 命中 → 注入块出现在 system prompt 前缀**。

**范围内**：

- 真实会话旁路证据：`.semantix/sessions/boot-1.jsonl`（fork HarnessSink 写的
  turn 级 JSONL，含 user/assistant/tool 行）→ `semantix extract` 入库
- 跨会话检索/注入：`semantix lookup` / `inject` 命中第一会话沉淀的切片，
  验证 `zone=hit` 与注入块输出
- 注入块字节稳定性实测：连续两次 `inject` 输出逐字节一致（前缀缓存吸收前提）
- 报告：`docs/reports/issue-64-acceptance.md`（环境、命中记录、字节稳定性、成本/延迟口径）

**不在范围内**（后续单元，本 PR 不做）：

- fork 代码任何改动（挂载点已合入；如需修复另行开 issue）
- L3 结果复用（`kernel/cache/l3.go`，随 U16）
- 真实 300 对 judge 一致性（#20/#58 数据门槛）
- 前缀缓存命中的厂商侧计量（仅记录 usage 事件观测口径，不改造 fork 计费）

## 2. 环境与前置

| 项 | 要求 | 本机现状 |
|---|---|---|
| fork 仓库 | Gnosil/DeepSeek-Reasonix @ main（含 U7/U8 挂载） | 已挂载（守卫测试 PASS 记录于 `docs/reports/customer-delivery.md` §挂载） |
| semantix 二进制 | `go install ./cmd/semantix`，PATH 可调 | `C:\Users\liwen\go\bin\semantix.exe` ✅ |
| 会话旁路 | fork 运行中写 `.semantix/sessions/` | `boot-1.jsonl`（73 行，真实会话） ✅ |
| 模型 API key | fork 运行所需 key（本会话已在跑） | ✅ |

## 3. 验证流程设计

### 3.1 会话提取（U7 产物入库）

```
fork 真实会话 → HarnessSink → .semantix/sessions/boot-1.jsonl
  → semantix extract --input boot-1.jsonl --scope user --project <域> --user-db ...
```

通过标准：`extracted=N stored=M`，M ≥ 1（user scope 库非空）。

### 3.2 跨会话检索（U8b semantix_lookup 等价验证）

```
semantix lookup --query "<第一会话中的任务描述>" --scope user --limit 5
```

通过标准：返回 top1 的 `zone=hit`，且切片内容与第一会话主题相关（≥1 个跨会话复用案例）。

### 3.3 注入块（U8a 注入等价验证）

```
semantix inject --query "<当前任务描述>" --scope user --budget 4096
```

通过标准：输出 `[semantix-reuse]` 块；连续两次输出 sha256 相同（字节稳定性）。

### 3.4 守卫复跑（fork 侧无回归）

- fork 内 `go build ./...` 通过
- fork 守卫测试复跑：`TestBuildComposesByteStableSystemPrompt` +
  `TestCacheHitPrefixStable/ClimbsWithoutCompaction/SurvivesTooSmallWindow`

> 本 PR 在 semantix 仓库内完成，fork 侧守卫复跑结果记录在报告中
> （fork 仓库独立 clone，本次验证仅记录其已有 PASS 状态，不重复拉取）。

## 4. 验收标准（对应 issue #64 body）

| # | 验收标准 | 验证方式 |
|---|---|---|
| 1 | 至少 2 个跨会话复用案例（第二会话命中第一会话切片） | §3.2 lookup 命中 ≥2 个不同查询词 → zone=hit |
| 2 | 报告含：环境（模型/版本）、命中记录、注入块字节稳定性实测 | `docs/reports/issue-64-acceptance.md` 逐项填写 |
| 3 | fork 侧无回归（go build + 守卫测试复跑） | §3.4 记录 fork 守卫测试状态 |

## 5. 风险与降级

| 风险 | 等级 | 缓解 |
|---|---|---|
| boot-1.jsonl 切片主题与检索词不匹配（BM25 命中不足） | 中 | 用会话内实际关键词做查询；必要时降低 τ 或换查询词；至少保 1 个 hit 案例 |
| 注入块字节不稳定（map 遍历顺序等） | 低 | `kernel/inject` 已确定性排序；连续两次 sha256 断言 |
| fork 守卫测试本机无法复跑（仓库未 clone 到本机） | 低 | 记录已有 PASS 证据来源，标注"未复跑"，不伪造 |
| 网络限制（GitHub 直连不稳） | 低 | 推送/PR 用 gh api 旁路（此前惯例） |

## 6. 交付物

- `docs/specs/issue-64-fork-e2e.md`（本文档）
- `docs/reports/issue-64-acceptance.md`（验证报告：环境/命中/字节稳定性/守卫状态）
- PR 关联 issue #64，含验证命令与输出摘录
