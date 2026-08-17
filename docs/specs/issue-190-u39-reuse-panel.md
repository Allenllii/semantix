# Spec：U39 C5 视觉基线——Semantix Design 主题 token + U33 复用面板进程内化（Issue 190）

> 对应 Issue：`#190 U39: C5——Semantix Design 视觉基线（主题 token + U33 复用面板进程内化）`
> 真源约束：`docs/specs/h2h3-resource-orchestration.md` §7（C5 判级 Spec-Exempt，本 spec 为实施级细化）、
> `docs/reports/harness-refactor-blueprint.md` §4（Semantix Design：深色 + 语义绿 #2F967F）与 §77（复用可视化）、
> `kernel/inject/inject.go`（`Injection.Slices`）、`kernel/usage/usage.go`（`Summary.SavingsUSD`）、
> `kernel/lookup/lookup.go`（`Result.SourceSession`，U30 PR #160 已合入）、`kernel/event/event.go`（Notice + 追加式 Kind 纪律）。
> 迁移素材：fork `Allenllii/DeepSeek-Reasonix` `feat/u33-tui-reuse-panel` @ `79b94e45`（U33 验证成果，PR #3）。
>
> **状态（2026-08-17）**：v1 实施 spec，先审后写。**前置：U38（#189）合入 `harness-integration`**（harness/ 代码在仓）——U38 未落地前本 U39 只做素材准备，不写 harness 代码。
>
> **实施记录（2026-08-17）**：U38（#189）与 U39 已串行实施并提交（分支 `feat/issue-189-u38-harness-vendor`，base `harness-integration`），本 spec 的 c1-c6 全部落地，见 §6 验收与 `docs/reports/issue-190-migration-source.md`。实施偏差记录于 §8。

## 1. 目标与范围

**核心目标**：U38 落地后，在 `harness/tui/` 落地 Semantix Design 最小视觉基线（深色 + 语义绿 #2F967F 主题 token），并把 fork 侧已验证的 U33 每 turn 复用面板（📦 命中切片数 / 💰 节省成本 / 🗂 来源会话 top-3）迁入本仓，数据源从 CLI 子进程改为**进程内直读**（inject 返回值 + usage 直读，删 `--json` 子进程调用）。

**范围内**：

- `harness/tui/theme.go`（或等价落点）：Semantix Design 主题 token（深色基调 + 语义绿 #2F967F）
- `ReuseSummary` 结构与渲染逻辑从 fork 迁入（`internal/semantix/reuse.go` → harness 进程内共享层）
- 进程内数据源：命中切片数 + 来源会话取自注入点返回值（`kernel/inject` 的 `Injection.Slices`，含 `Meta.SourceSession`）；节省成本取自 `kernel/usage.Summarize` 的 `SavingsUSD` 累计差
- 每 turn 结束渲染面板（`Agent.Run` defer，TurnDone 前发 `Notice{Code: semantix_reuse}`）
- 无命中隐藏（零噪音）+ kernel 组件不可用时软降级（零值 summary → 面板不出现，不崩）
- 资源仪表挂点：预留侧栏组件接口，本期空实现
- TUI 测试 + 真实会话截图走查

**不在范围内**（后续单元，本 U39 不实现）：

- C0 vendor 本身（U38/#189：`harness/` 目录、模块改写、构建、冒烟）
- C1-C3（U40/#191 资源目录/Decider 直连/SuspendTools；U41 BudgetController 阶梯降级）
- 桌面端复用面板（#158，U34 续作；fork 共享 protocol client **不迁**——那是跨进程时代的产物，其职责由进程内 ReuseSummary 直读替代）
- 资源仪表实现（本期只留挂点，等 C1-C3 数据稳定后单独 issue）
- 复用面板「当前任务完成时间」段（#177，独立 issue，采集/展示另开）
- 原生功能裁剪/回归（#178 关注：沙箱、bot、既有交互照用，只做风格化与复用可视化增强）
- `kernel/` 接口签名变更（冻结；`kernel/lookup.Result` 已含 `source_session`，无需改动）

## 2. 现状与依赖

| 项 | 状态 | 说明 |
|---|---|---|
| U38 (#189) | ❌ 未开工 | `harness-integration` 分支无 `harness/` 目录；本 U39 的实施物理依赖 U38 的最小 vendor（agent+tool+control+tui+provider） |
| fork U33 实现 | ✅ 已验证 | `feat/u33-tui-reuse-panel` @ `79b94e45`：`internal/semantix/reuse.go`（ReuseSummary/topSources/formatUSD）、`bridge.go`（`Bridge.Reuse`：lookup + usage 累计差）、`internal/agent/reuse.go`（reuseNotice/emitReuse）、`run_loop.go`（turn 起始采集点）、`agent.go`（defer emitReuse）、`internal/cli/reuse_panel.go`（面板渲染）、`chat_tui.go`（Notice 特判）、`internal/event/event.go`（`NoticeCodeSemantixReuse`） |
| kernel 进程内数据源 | ✅ 可用（upstream/main） | `kernel/inject.Injection.Slices []*slice.Slice`（含 `Meta.SourceSession`，命中数 + 来源）；`kernel/usage.Summarize(path, costMiss, costHit)` → `Summary.SavingsUSD`；`kernel/lookup.Result.SourceSession`（U30 已合入） |
| 事件契约 | ✅ 冻结 | 复用 `Notice` + 新稳定码 `NoticeCodeSemantixReuse`（fork 已验证），不新增 Kind、不动序列化 |

## 3. 设计

### 3.1 Semantix Design 主题 token（`harness/tui/theme.go`）

最小 token 集（蓝图 §4 与 landing page 一致，深色基调 + 语义绿 `#2F967F`）：

```go
// Theme 是 Semantix Design 最小 token 集（U39）。TUI 组件只消费 token，
// 不私造颜色。
type Theme struct {
    Background lipgloss.Color // 深色基调（如 #1E1E24，以 U38 vendor 后的实际底色为准）
    Surface    lipgloss.Color // 面板/侧栏底
    Foreground lipgloss.Color // 主前景
    Dim        lipgloss.Color // 次要信息
    Accent     lipgloss.Color // 语义绿 #2F967F（品牌强调）
    Success    lipgloss.Color // 成功/命中（= Accent 派生或同值）
    Warn       lipgloss.Color // 警告（保留 reasonix 语义，仅风格化）
    Error      lipgloss.Color // 错误
}
```

规则：现有组件配色迁移到 token；原生功能（沙箱/bot/交互）只换色不换行为；配色值以走查截图验收，不额外引入设计稿依赖。

### 3.2 ReuseSummary 迁入（harness 进程内共享层）

fork `internal/semantix/reuse.go` 的 `ReuseSummary{Hits, SavingsUSD, Sources top-3}` + `Line()`/`topSources()`/`formatUSD()` 原样迁入（语义不变，测试同步迁移）。落点建议：`harness/agent/`（构造方）之上的共享小包，U34 桌面端（#158）与 TUI 同源消费；**以 U38 vendor 后的实际包结构为准**，但必须是 harness 内共享层，禁止落到 `harness/tui/`（渲染层不拥有业务语义）。

### 3.3 进程内数据源（替代 CLI 子进程）

fork 侧 `Bridge.Reuse` 的两次子进程调用（`lookup --json` / `usage --json`，各 3s cap、fail-open）**整体删除**，改为：

- **turn 起始（与注入同点同 query）**：构造 `ReuseSummary`——
  - `Hits` = 注入点返回的 `Injection.Slices` 数量（inject 返回值，零额外查询）
  - `Sources` = `Injection.Slices` 按 `Meta.SourceSession` 频次 top-3（空会话跳过）
  - `SavingsUSD` = `kernel/usage.Summarize(<项目>/.semantix/usage.jsonl)` 的 `SavingsUSD` 与上次快照之差（进程内直读；`usage.jsonl` 不存在/直连 provider → 0，💰 段省略，与 fork 验证一致）
- **turn 结束**：`emitReuse` 在 `Agent.Run` defer、TurnDone 前发 `Notice{Kind: Notice, Level: LevelInfo, Code: NoticeCodeSemantixReuse, Text: reuse.Line(), Detail: JSON(ReuseSummary)}`；`Hits <= 0` 不发（零噪音）
- 删除 fork `protocol.go` 的 envelope/runCLI/Usage()/Lookup()/Version() 及其 3s 超时软降级逻辑——进程内同生共死，kernel 不可用 = harness 启动失败（fail-fast，不再有"子进程超时"路径）；**保留** fail-open 语义于数据层：store/usage 日志缺失 → 零值 summary，面板隐藏

### 3.4 TUI 渲染

- `harness/tui/`（U38 后的 chatREPL/runAgent）在 `case event.Notice` 特判 `NoticeCodeSemantixReuse`：渲染 `📦 N slices reused · 💰 saved $X · 🗂 from: s1, s2, s3`（复用面板行，`Theme.Success` 语义绿）；detail 空/坏/`Hits<=0` → 不渲染
- 无命中/数据缺失 → 面板完全不出现在 scrollback（零噪音，fork 已验证）
- 资源仪表挂点：预留侧栏组件接口（如 `tui.ResourceGauge` interface + 空实现 `nilGauge`），本期不接数据

## 4. 文件面（U38 落地后的形态）

| 面 | 落点 | 内容 |
|---|---|---|
| 主题 | `harness/tui/theme.go`（新） | §3.1 token |
| 数据 | `harness/agent/`（或共享小包） | `reuse.go`：ReuseSummary + topSources + formatUSD（迁入）；turn 起始采集 + `emitReuse`（run_loop/agent.go 接线，对照 fork 改动点） |
| 渲染 | `harness/tui/` | `reuse_panel.go`（迁入，换 theme token）；chat REPL Notice 特判（对照 fork `chat_tui.go`） |
| 事件 | `kernel/event/event.go` | `NoticeCodeSemantixReuse`（若 U38 vendor 未带入，U39 补） |
| 测试 | 对应包 | reuse 构造/格式化/top-3/隐藏/软降级 + 面板渲染（迁移 fork 测试） |
| 素材对照 | `docs/reports/issue-190-migration-source.md` | fork commit → 本仓落点的迁移对照表（U38 前先行产出） |

删除面（不迁）：fork `internal/semantix/protocol.go`（envelope/runCLI/Usage/Lookup/Version）、`bridge.go` 的 CLI 调用路径、`reuse_panel.go` 中 `activeCLITheme` 依赖（换 token）。

## 5. 测试计划（对应 #190 checklist）

- `harness/agent` reuse 测试：命中数/sources top-3/空会话跳过/SavingsUSD 累计差/零值不发事件（迁移 fork `reuse_test.go`）
- `harness/tui` 面板测试：detail 渲染文案 pin（fork 已验证文案 `📦 3 slices reused · 💰 saved $0.0042 · 🗂 from: boot-1, boot-2`）、坏 payload 隐藏、无命中隐藏（迁移 fork `reuse_panel_test.go`）
- 主题 token 测试：token 存在性 + 渲染使用（不测具体色值）
- 走查：真实会话截图（注入 + 面板 + 无命中隐藏 + kernel 数据缺失软降级）
- 门禁：`go build ./...` + `go test ./...`（`-race` 由 CI Linux 覆盖，本机 Windows 无 gcc 时说明）

## 6. 验收标准（对应 #190 checklist 逐条）

| # | 验收 | 证据 |
|---|---|---|
| c1 | `harness/tui/theme.go`：Semantix Design token（深色 + #2F967F） | 文件存在 + TUI 渲染走查截图 |
| c2 | U33 复用面板迁入 + 进程内数据源（inject 返回值 + usage 直读，删 `--json` 子进程调用） | 代码中无 harness→semantix CLI 子进程调用；`go test` 全绿 |
| c3 | 无命中隐藏 + 软降级 | 测试 + 走查（kernel 数据缺失不崩、无面板） |
| c4 | 资源仪表挂点（接口预留，空实现） | 接口 + 空实现存在，不接数据 |
| c5 | TUI 测试 + 截图走查（真实会话） | 测试通过 + 截图入验收报告 |
| c6 | base 分支 `harness-integration` | PR base 指向 harness-integration |

## 7. 风险与边界

- ~~**U38 未开工是本 U39 的物理阻塞**~~：已消除——U38（#189）最小 vendor（112 包闭包）已合入本分支，本 U39 在其上实施。后续 U40/U41 以同一 `harness-integration` base 推进。
- **fork protocol.go 与 U34 共享**：U39 只迁 U33 面；U34（#158）桌面端后续直接从进程内 ReuseSummary 消费，不迁 CLI 协议。
- **usage.jsonl 缺失**（直连 provider、无 gateway 回路）：💰 段自动省略（fork 已验证，两条数据通道独立软降级）。
- **主题色值一致性**：以走查截图为准，TUI 与桌面端（#158/#176）共用同一 token 定义，不双轨。
- **禁止回归**：#178 关注原生功能保留；本 U39 只改渲染层 + 新增数据面，不改 agent 主循环语义。

## 8. 实施偏差（2026-08-17）

| spec 原文 | 实施 | 原因 |
|---|---|---|
| `harness/tui/theme.go`（或等价落点） | 等价落点 `harness/cli/theme.go`：新增 `semantix` 主题风格（dark + #2F967F accent/success），`REASONIX_THEME=semantix` 切换 | vendor 后 TUI 主题层在 `harness/cli`（`cliPalette`/`cliThemeStyle` 已是完整 token 集，复用既有模式，不另起并行体系） |
| 删 `--json` 子进程调用 | `Bridge.Inject`/`Bridge.Reuse` 已进程内化（kernel/slice + bm25 + inject + usage + zone 直读），删除 `protocol.go`/`inject.go`（envelope/runCLI/3s cap 全部移除）；`semantix_lookup` 工具保留自身 CLI exec（U8 工具契约，U40 处理） | #190 checklist "inject 返回值 + usage 直读" |
| 资源仪表挂点 | `harness/cli/resource_gauge.go`：`resourceGauge` 接口 + `nilGauge` 空实现，挂入 `chatTUI.gauge` 字段 | 本期空实现 |
| 成本价格来源 | `SemantixConfig` 新增 `cost_input_price_usd`/`cost_cache_price_usd`（镜像 semantix.toml `[cost]` 键），默认 kernel 价格 | 进程内无法让子进程 CLI 读 semantix.toml |
