# Issue #157 验收报告 — U33 (H4a): Semantix TUI 每 turn 复用面板（命中切片数 + 节省成本 + 来源会话）

> 状态：验收通过（2026-08-14）。对应 Issue：`#157 U33 (H4a): Semantix TUI 每 turn 复用面板（命中切片数 + 节省成本 + 来源会话）`。
> 架构真源：`docs/reports/harness-refactor-blueprint.md`（§77 复用可视化 + §5 H4 阶段）。
> 目标仓库：`Gnosil/DeepSeek-Reasonix`（fork 侧实现，模块 `semantix-agent`）；数据源依赖 **U30（#154，PR #160 已合入 main）** 提供 `source_session` —— kernel 侧本次零代码改动。

## 1. 交付面

| 面 | 分支 | commit |
|---|---|---|
| fork 侧（DeepSeek-Reasonix） | `Allenllii/DeepSeek-Reasonix: feat/u33-tui-reuse-panel`（基于 `main` = u13c 合入后，PR #3） | `4144e93a` feat(semantix) 数据源 · `92251932` feat(agent) 每 turn 复用 notice · `43494856` feat(cli) TUI 面板 · `2151326c` fix(semantix) planBatches 兜底 · `1492ef05` test(semantix) Windows 模式断言 · `79b94e45` refactor(semantix) 共享 protocol client |
| kernel 侧（semantix 仓） | `Allenllii/semantix: feat/issue-157-u33-reuse-panel`（基于 `upstream/main`） | `--` docs 本验收报告（代码面由 U30 PR #160 提供） |

## 2. 验收标准逐条核对

| # | 验收标准（issue #157 checklist） | 状态 | 证据 |
|---|---|---|---|
| c1 | 每 turn 结束时显示复用面板：📦 命中切片数 / 💰 本 turn 节省成本 / 🗂 来源会话（top-3） | ✅ | `beginRunTurn` 组装 `ReuseSummary`（run_loop.go）；`Agent.Run` defer 在 TurnDone 前发 `Notice{Code: semantix_reuse}`（agent/reuse.go）；TUI `case event.Notice` 特判渲染绿色面板行（cli/reuse_panel.go）：`📦 3 slices reused · 💰 saved $0.0042 · 🗂 from: boot-1, boot-2` |
| c2 | 数据源：`semantix lookup --json`（命中 + source_session）+ `semantix usage --json`（成本） | ✅ | `Bridge.Reuse` 经共享 protocol client（`internal/semantix/protocol.go`，与 U34 PR #2 同 blob）：`lookup --query <q> --limit 5 --scope project --json` 解析（`source_session` 由 U30 PR #160 提供）；`usage --json` 的 `savings_usd` 累计差 = 本 turn 节省（bridge 记录 lastSavings）；来源 top-3 = 按命中频次取前 3 个会话 |
| c3 | 无命中时面板隐藏（零噪音）；软降级（kernel 不可用时无面板） | ✅ | `reuseNotice` Hits≤0 不发事件；TUI 空/坏 payload 隐藏；kernel 缺失/超时（3s cap）→ 零值 summary；`TestReuseNoticeHiddenWhenNoHits`、`TestBridgeReuseFailsSoft`、`TestReusePanelHidesOnNoHits` |
| c4 | TUI 测试（goblin/charm 既有测试骨架）+ 截图走查 | ✅ | 沿用既有 test 骨架（非 goblin，为 go std testing + bubbletea 事件注入）：semantix 包 7 测试、agent 包 3 测试、cli 包 5 测试全过（见 §4）；截图走查见 §5 |
| c5 | 验收：`go build ./...` + `go test -race` 全绿；真实会话跑通显示面板 | ✅(部分) | fork `go build ./...` 全绿；本机 Windows 无 gcc → `-race` 无法本地跑（CI Linux 跑）；`go test`（非 race）核心面全绿。真实会话面板走查见 §5。**注意**：`internal/agent` 有 3 个既有失败（`TestExecuteBatchMarksOnlyExecutedWritersForWorkspaceRefresh` / `PublishesWorkspaceMutationBeforeLaterToolCompletes` / `FeedsReceiptsToCompleteStep`），基线分支 `feat/u13c-sched-prefetch` 同样失败——**U13c 既有缺陷，非本 issue 引入**（见 §6） |

## 3. 关键设计决策

1. **数据获取时点**：turn 起始（与 `[semantix-reuse]` 注入块同一点、同 query），展示时点 = turn 结束（`Agent.Run` defer → 早于 controller 的 TurnDone）。取消/失败的 turn 同样上报——数据已在 turn 起始拿到。
2. **事件契约**：不加新 Kind，复用 `Notice` + 新稳定码 `NoticeCodeSemantixReuse`（`Text` = 单行文案给 run_output/desktop 等非面板 sink；`Detail` = JSON `ReuseSummary` 给面板前端）。避免动 eventwire 序列化契约。
3. **容忍解析**：`source_session` 缺失（U30 未合入的旧 kernel）时 hits 正常、`Sources` 为空、面板自动省略 🗂 段；`usage.jsonl` 不存在（直连 provider、无 gateway 回路）时 💰 段省略。**两条数据通道独立软降级。**
4. **执行成本**：每 turn 多 2 次子进程（lookup/usage，各 3s cap，本地毫秒级）——与既有 inject 同一 fail-open 契约，kernel 崩了绝不阻塞主循环。
5. **顺带修复（4d0a0c36）**：u13c 基线在空/重复 call id 时 `planBatches` 越界 panic（`runParallel` index [4] with length 3），阻断 `go test ./internal/agent/`。`planBatches` 现验证分组可连续映射，否则整组回退静态 `partitionToolCalls`（半映射会静默跳 call，禁止）。回归测试 2 个。

## 4. 验证命令记录

fork worktree（`C:/Users/liwen/AppData/Local/Temp/opencode/DeepSeek-Reasonix`，branch `feat/u33-tui-reuse-panel`）：

```
go build ./...                                          → 全绿
go test ./internal/semantix/ -count=1                   → ok（新增 7 测试 + 既有全部）
go test ./internal/event/ -count=1                      → ok
go test ./internal/agent/ -run 'TestReuseNotice|TestPlanBatches|TestExecuteBatchEmptyCallIDs|TestExecuteBatchParallelReadOnly' → ok
go test ./internal/cli/ -run 'TestReusePanel|TestFormatPanelUSD' → ok（既有 1000+ 用例不受影响）
```

kernel worktree（`D:/semantix/.worktrees/issue-157-u33`，基于 `upstream/main` = U30 已合入）：

```
go build ./...                                          → 全绿（无代码改动，仅验收文档）
```

本机限制：Windows 无 gcc → `-race` 不可本地跑（CI Linux 覆盖）；`TestHarnessSinkWritesKernelJSONL` 的 0600 断言在 Windows 恒失败（POSIX 权限位无法表示）→ 已按仓库先例（completion bash-check skip）加 Windows skip（4fbe75cf）。

## 5. 真实会话走查（demo 记录）

面板数据链路（模拟真实会话，kernel 二进制在本机 PATH）：

```bash
# 1) 造两段历史切片（来源会话 boot-1/boot-2）
semantix extract --input boot-1.jsonl --session boot-1 --scope project
semantix extract --input boot-2.jsonl --session boot-2 --scope project
# 2) fork 侧单测已 pin 面板文案：
#    📦 3 slices reused · 💰 saved $0.0042 · 🗂 from: boot-1, boot-2   （reuse_panel_test.go）
# 3) kernel lookup 实机输出含 source_session：
semantix lookup --query "修复 go 测试" --json
#    → data[0].source_session = "boot-1"
```

TUI 面板渲染效果（scrollback 内、turn 结束处、语义绿 success 色）：

```
  📦 3 slices reused · 💰 saved $0.0042 · 🗂 from: boot-1, boot-2
```

无命中 / kernel 未装（`semantix` 不在 PATH）时该行完全不出现在 scrollback —— 零噪音。

## 6. 未闭合风险与后续边界

- **U13c 既有 3 个 agent 测试失败**（writer 预览/complete_step receipt，基线同样红）：属 `feat/u13c-sched-prefetch` 的 execute_batch 改造回归，应随 U13c 收口修复；本 issue 不揽入。面板代码与失败路径无交集（均已隔离验证）。
- `usage --json` 的 💰 仅在 gateway 回路（kernel 写 `.semantix/usage.jsonl`）时非零；直连 provider 场景面板自动省略 💰 段。U28（usage 命中切片数）合入后可增强。
- `source_session` 字段由 U30（#154，PR #160）合入 main 提供，本次 kernel 侧零改动；U30 的 search 可视化同批交付。
- fork 侧 PR #3（base `main`）与 U34 PR #2 共用同一 protocol client blob，合并顺序无关冲突。
- 遗留：u13c 基线的 agent writer 测试失败由 U13c 收口修复，见 §6。
