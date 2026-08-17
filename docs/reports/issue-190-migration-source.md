# Issue 190 (U39) 迁移素材对照表——fork U33 复用面板 → 本仓进程内化

> 状态：2026-08-17 · 用途：U39 实施（spec `docs/specs/issue-190-u39-reuse-panel.md` §4）的逐文件迁移对照。
> 素材源：`Allenllii/DeepSeek-Reasonix` 分支 `feat/u33-tui-reuse-panel`（U33 验证成果，PR #3，验收报告 `docs/reports/issue-157-acceptance.md`）。
> 本表不执行迁移；U38（#189）vendor 合入 `harness-integration` 后按此表落点实施。

## 1. fork 侧 U33 相关 commit（6 个）

| commit | 内容 | 本 U39 是否迁入 |
|---|---|---|
| `4144e93a` | feat(semantix) 数据源：ReuseSummary + Bridge.Reuse（lookup/usage 子进程） | 迁入（数据源**改写为进程内**，子进程部分删除） |
| `92251932` | feat(agent) 每 turn 复用 notice：reuse.go + run_loop/agent.go 接线 | 迁入（接线保留） |
| `43494856` | feat(cli) TUI 面板：reuse_panel.go + chat_tui.go Notice 特判 | 迁入（换主题 token） |
| `2151326c` | fix(semantix) planBatches 空/重复 call id 越界兜底 | **不迁**（u13c 基线修复，随 U38 vendor 素材走，非 U33 面） |
| `1492ef05` | test(semantix) Windows 模式断言 | 参考（本仓测试纪律，非面板面） |
| `79b94e45` | refactor(semantix) 共享 usage/lookup protocol client（对齐 U34 PR #2） | **不迁**（跨进程 CLI 协议，进程内化后删除） |

## 2. 逐文件对照

| fork 文件（`feat/u33-tui-reuse-panel`） | 动作 | 本仓落点（U38 后） | 迁移要点 |
|---|---|---|---|
| `internal/semantix/reuse.go`（122 行） | 迁入 | `harness/agent/reuse.go`（或 U38 后共享小包） | `ReuseSummary{Hits,SavingsUSD,Sources}` + `Line()` + `topSources()` + `formatUSD()`/`usdFixed()`/`itoa64()` 原样；包名改 |
| `internal/semantix/reuse_test.go` | 迁入 | 同落点 `reuse_test.go` | 断言原样（文案 pin：`📦 3 slices reused · 💰 saved $0.0042 · 🗂 from: boot-1, boot-2`） |
| `internal/semantix/bridge.go`（`Bridge.Reuse`，约 116-133 行） | **改写** | 同落点（构造函数替代） | 删 CLI 调用；`Hits`/`Sources` ← 注入点 `kernel/inject.Injection.Slices`（`Meta.SourceSession` 频次 top-3）；`SavingsUSD` ← `kernel/usage.Summarize` 累计差（进程内直读，无 3s cap） |
| `internal/semantix/protocol.go`（206 行） | **删除** | — | envelope/runCLI/Usage()/Lookup()/Version() 全部不迁（跨进程产物，spec §0）；U34 桌面端（#158）后续消费进程内 ReuseSummary |
| `internal/semantix/protocol_test.go` | 删除 | — | 同上 |
| `internal/agent/reuse.go`（42 行） | 迁入 | `harness/agent/reuse.go` | `reuseNotice()`（Notice + `NoticeCodeSemantixReuse`，Text=Line、Detail=JSON）+ `emitReuse()`（Run defer，TurnDone 前）；`Hits<=0` 不发 |
| `internal/agent/reuse_test.go` | 迁入 | `harness/agent/reuse_test.go` | 原样 |
| `internal/agent/run_loop.go`（L258-269） | 改写接线 | `harness/agent/run_loop.go` | turn 起始与注入同点：`state.reuse = a.semantix.Reuse(ctx, input)` → 进程内构造（注入点返回值） |
| `internal/agent/agent.go`（L1295-1298） | 保留接线 | `harness/agent/agent.go` | `defer a.emitReuse(state)` 位置不变（成功/错误/取消全路径） |
| `internal/cli/reuse_panel.go`（48 行） | 迁入 | `harness/tui/reuse_panel.go` | `reusePanelLines(detail, width)` 原样；`activeCLITheme.success` → `Theme.Success`（#2F967F 语义绿） |
| `internal/cli/reuse_panel_test.go` | 迁入 | `harness/tui/reuse_panel_test.go` | 原样（换 token 后文案不变） |
| `internal/cli/chat_tui.go`（L4598-4606） | 改写接线 | `harness/tui/`（chatREPL） | `case event.Notice` 特判 `NoticeCodeSemantixReuse` → 渲染面板行；坏/空 payload 隐藏 |
| `internal/event/event.go`（L556-559） | 迁入 | `kernel/event/event.go` | `NoticeCodeSemantixReuse = "semantix_reuse"`（若 U38 vendor 未带入，U39 补；不新增 Kind） |

## 3. 删除面（不迁入本仓）

- `internal/semantix/protocol.go` / `protocol_test.go`：CLI 信封 + 子进程执行（3s cap fail-open 路径整体消失，进程内同生共死）
- `Bridge` 的 `lastSavings` 累计差逻辑改写为进程内 usage 快照差（保留语义：delta = 本次 SavingsUSD − 上次快照，`delta > 0` 才计）

## 4. 素材核对记录

- fork 侧 3 个测试文件存在：`internal/semantix/reuse_test.go`、`internal/agent/reuse_test.go`、`internal/cli/reuse_panel_test.go` ✅
- kernel 进程内数据源已在 upstream/main：`kernel/lookup.Result.SourceSession`（U30 PR #160）✅；`kernel/inject.Injection.Slices`（含 `Meta.SourceSession`）✅；`kernel/usage.Summarize → Summary.SavingsUSD` ✅
- fork 侧 `activeCLITheme` 主题依赖（`wrapForViewport`/`activeCLITheme.success`）为渲染层细节，迁入时以 U38 后 TUI 主题结构为准

## 5. 未闭合项

- U38（#189）未开工：`harness-integration` 无 `harness/` 目录，本表落点（`harness/agent`、`harness/tui`）为 spec §3 的规划路径，最终以 U38 vendor 实际结构为准
- fork `planBatches` 兜底修复（`2151326c`）属 u13c 基线，U38 vendor 时若带入 fork 的 run_loop 实现需核对（不属本 U39 范围）
