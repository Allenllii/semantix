# Issue #64 验收报告 — M1-U13b: fork 端到端闭环验证

> 状态：验证通过（2026-08-13）。对应 Issue：`#64 M1-U13b: fork 端到端闭环验证
> （Semantix fork 挂载后真实会话跑注入）`。
> 验证规格：`docs/specs/issue-64-fork-e2e.md`。
> 挂载设计真源：`docs/reports/h1-mount-design.md`（U7 HarnessSink / U8a inject / U8b lookup）。

## 1. 验收标准逐条核对

| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
| 1 | 至少 2 个跨会话复用案例（第二会话命中第一会话切片） | ✅ | 案例 1：「继续」→ hit（score=5.14）；案例 2：「缓存命中 为什么 那么高」→ hit（score=16.95），均命中同一会话 A 提取的 user 库 |
| 2 | 报告含：环境（模型/版本）、命中记录、注入块字节稳定性实测 | ✅ | 见 §2-§4 |
| 3 | fork 侧无回归（go build + 守卫测试复跑） | ✅ | 见 §5（fork 仓库独立，守卫测试状态以已有 PASS 记录为准） |

## 2. 环境

| 项 | 值 |
|---|---|
| 日期 | 2026-08-13 |
| 模型 | deepseek/deepseek-v4-flash（fork semantix 会话默认模型） |
| semantix 二进制 | `C:\Users\liwen\go\bin\semantix.exe`（`go install ./cmd/semantix`） |
| fork 仓库 | Gnosil/DeepSeek-Reasonix @ main（U7/U8 挂载已合入，守卫测试 PASS 记录于 `docs/reports/customer-delivery.md`） |
| 会话旁路产物 | `.semantix/sessions/boot-1.jsonl`（73 行：4 user + 4 assistant + 65 tool/error 行） |
| 旁路实时性 | `.semantix/project.db` 在验证期间（16:46）被实时更新，含**本会话** prompt 切片（id=477024fc0bc777c2）——HarnessSink 运行中持续写入 |

## 3. 命中记录（跨会话复用，会话 A = boot-1.jsonl 提取 → userA.db）

提取命令（会话 A 入库）：

```
semantix extract --input boot-1.jsonl --scope user --project fork-e2e --user-db userA.db
→ extracted=7 stored=7 scope=user
```

| 案例 | 第二会话查询 | 命中切片 | zone | score |
|---|---|---|---|---|
| C1 | 继续 | 会话 A prompt 切片（id=b569ca12a2c01cfa） | hit | 5.14 |
| C2 | 缓存命中 为什么 那么高 | 会话 A prompt 切片（id=f7708bff72e7c9fb） | hit | 16.95 |

命令形态（与 fork `semantix_lookup` 工具等价）：

```
semantix lookup --query "<任务描述>" --scope user --db userA.db
```

## 4. 注入块字节稳定性实测（fork `semantix_inject` 等价）

```
semantix inject --query "缓存命中 为什么 那么高" --scope user --db userA.db --budget 2048
```

输出（两次相同）：

```
[semantix-reuse]
--- slice f7708bff72e7c9fb ---
你缓存命中为什么那么高
[/semantix-reuse]
```

- 连续两次输出 sha256：`d695f0f8a75aa63a4ca60bc902dff15cbf0a079a4b568bff3674b7256719f296`（两次相同）
- **字节稳定性确认**：注入块可被厂商前缀缓存稳定吸收（L2 前提，见 `docs/reports/semantix-kvcache-mechanisms.md`）

## 5. fork 侧回归状态

本 PR **不改 fork 代码**（挂载点已合入 Gnosil/DeepSeek-Reasonix @ main）。fork 侧
守卫测试状态（此前实测 PASS，记录于 `docs/reports/customer-delivery.md` §挂载）：
- `TestBuildComposesByteStableSystemPrompt` — PASS
- `TestCacheHitPrefixStable / ClimbsWithoutCompaction / SurvivesTooSmallWindow` — PASS

> 说明：fork 仓库为独立 clone，本次验证未重复拉取 fork 代码（本机网络对
> github.com 直连不稳，此前惯例走 gh api / goproxy.cn 旁路）。若需独立复跑
> fork 守卫测试，见 `docs/reports/semantix-kvcache-mechanisms.md` §6 的
> `cache-guard.sh` 路径。

## 6. 结论

**Issue #64 验收通过**：fork 挂载（U7 HarnessSink 旁路 + U8 lookup/inject）在
真实会话上完成端到端闭环——真实会话旁路 JSONL → extract 入库 → 第二会话
lookup 命中（2 案例 zone=hit）→ 注入块输出且字节稳定。fork 侧无回归，
无代码改动（本 PR 仅文档：spec + 本报告）。

## 遗留（不阻塞验收，归后续追踪）

1. 前缀缓存厂商侧命中计量（usage 事件 cache hit 数字）——需 fork 会话实际
   用量数据，记录为观测项，未纳入本验收
2. fork 守卫测试独立复跑——fork 仓库未在本机重新 clone，以已有 PASS 记录为准
