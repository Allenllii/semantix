# Spec v1 — 计划审批卡：接上三态决策 API 并收集修改说明（Issue #329）

> 判级：Spec-Exempt（CLI 接线 + i18n + 文档；无新顶层包、无判定模型变更、
> 无新的落盘取值——`PlanDecisionExitPlan` 本就是 `ResolvePlanDecision` 会写
> 的既有取值，CLI 只是不再写错的那个）。Spec-Required 的那半（widening
> `Approvals` 端口、改计划门对拒绝的处理）属于 #328，已在
> `docs/specs/issue-328-plan-revision-feedback.md` 定义。
>
> 基线：PR #389（`codex/issue-328-plan-revision-feedback` @ `e4a3f80`），
> **不是 main**——本 issue 编译依赖 #328 落的三参数签名。

## 1. 问题

### 1.1 真源审计（2026-08-23，worktree `fix/issue-329-plan-approval-card`）

| 缺口 | 证据 |
|---|---|
| 三态决策 API 零生产调用方 | `handleApprovalKey` 的 `answer` 闭包把**三行都**走 `m.ctrl.Approve(...)`（`harness/cli/chat_tui.go:3316`）。`grep -rn ResolvePlanDecision --include=*.go .` 只命中定义、端口声明与其自身单测 |
| 「暂不执行」被记成 `revise_plan` | `Approve` 只凭布尔分类计划结果：`outcome = revise_plan; if allow { outcome = start_execution }`（`harness/control/controller.go:2264-2272`）。`exitPlan` 行 `allow=false` → 落盘收据写成 `revise_plan`，`PlanDecisionExitPlan` 从 CLI 不可达 |
| 「修改计划」无处写说明 | `approvalChoice` 五个字段全是布尔（`harness/cli/chat_tui.go:3194-3200`），没有能请求文本的字段；计划分支恰好建三行（`:3218`） |
| recovery 卡片硬编码空 note | `m.ctrl.ResolveRecovery(id, action, "")`（`harness/cli/chat_tui.go:3310`），而该传输**早就是三参数**（`harness/control/recovery.go:18`） |

### 1.2 损害的性质

不是卡死。按 `2` 会清掉 `m.pendingApproval`（`chat_tui.go:3317`），`hideComposer()`
随之变 false、输入框回来，计划模式也还在（计划分支只对 allow/exit 关模式，
`:3313-3315`）。用户完全可以把修改意见当成下一条消息发出去。

真正的损失有两处：

1. **收据是错的**，且不可事后重建——每一次「暂不执行」都在会话里累积成
   `revise_plan`。这是 P0 的那半。
2. **因果链断了**——修改意见作为一条全新的、不挂靠在任何决策上的用户消息
   到达，而 #328 刚刚建好的 feedback 通道没有生产者。

## 2. 设计

### 2.1 第 2 行就地展开输入，不加第 4 行

`PlanApprovalChoices` 的尾行是 `选择 [1/2/3]`，`ChatStatusPlanApproval` 也按
`1/2/3` 宣传；`handleApprovalKey` 还留着 `4 = deny` 的肌肉记忆兜底
（`chat_tui.go:3353`）；`TestPlanApprovalChoicesExposeThreeExplicitActions`
硬断言三语各恰好三个编号行。加第 4 行要同时破坏这四处。

因此：`approvalChoice` 加 `promptsForText bool`，只挂在**计划的 revise 行**
与 **recovery 的 revise 行**上。选中该行时不解析，转入打字态。

### 2.2 打字态的键盘归属：在 Update 的路由处单点拦截

issue 建议在 `handleApprovalKey` 内部把三段 arm（`switch msg.String()`、数字
arm、`y/a/p/n` arm）逐一 gate 住。本 spec 采用更强的做法：在
`chat_tui.go:1435` 的 `if m.pendingApproval != nil { return m.handleApprovalKey(msg) }`
**之前**插入 `approvalTyping` 分支，照抄 chooser 打字块（`:1370-1398`）的形状。

这样 `handleApprovalKey` 在打字期间根本不可达，三段 arm 无需各自 gate——
少三处可能漏掉的分支。同时在 `handleApprovalKey` 顶部保留一句
`if m.approvalTyping { return m, nil }` 作为不变量：它可被直接单测，且防住
未来有人调整路由顺序时把拦截点绕过去。

**`n` / `Esc` / `Ctrl+C` 的行为不变。** 它们调用 `answer(approvalChoice{})`
——零值，`promptsForText` 为 false——所以仍然立即解析为「保持规划」，不会
弹出输入框。只有显式选中第 2 行（数字 `2`，或 Enter 落在选中行 1）才进打字态。

### 2.3 解析路径拆出方法

`answer` 是 `handleApprovalKey` 里的闭包，而打字提交发生在 `Update`。把解析
逻辑提成方法 `func (m chatTUI) resolveApproval(choice approvalChoice, note string) (tea.Model, tea.Cmd)`，
两条路径共用。进入打字态时把当前行存进 `m.approvalTypingChoice`，提交时取回。

### 2.4 计划三行改走 `ResolvePlanDecision`

```
allow            → PlanDecisionStartExecution
choice.exitPlan  → PlanDecisionExitPlan
其余（含 n/Esc） → PlanDecisionRevisePlan
```

非计划工具仍走 `Approve`，签名不动——HTTP / ACP / bot 绑定不受影响。

`Ctrl+C` 先 `Cancel()`（会 `clearAll()`），再 `answer(approvalChoice{})`：此时
`ResolvePlanDecision` 返回「不再 pending」的错误，与旧路径下 `Approve` 直接
no-op 等价，照旧丢弃。

### 2.5 i18n

新增两个键，措辞对齐既有的 `ChatStatusQuestion` 风格：

| 键 | en | zh | zh-TW |
|---|---|---|---|
| `ApprovalNoteHint` | `Describe what to revise (Enter submits, Esc goes back)` | `说明要改什么（Enter 提交，Esc 返回）` | `說明要改什麼（Enter 提交，Esc 返回）` |
| `ChatStatusApprovalNote` | `Enter submits the note · Esc goes back · empty keeps planning` | `Enter 提交说明 · Esc 返回选项 · 留空即继续规划` | `Enter 提交說明 · Esc 返回選項 · 留空即繼續規劃` |

`PlanApprovalChoices` 第 2 行改措辞，注明说明可选：

- en：`2. Revise plan (keep planning; note optional)`
- zh：`2. 修改计划（继续规划；可留说明）`
- zh-TW：`2. 修改計畫（繼續規劃；可留說明）`

**约束核对**（`harness/i18n/catalog_parity_test.go:78-93`）：`extractCodeTokens`
只抽反引号跨度、`/slash` 命令、`PgUp|PgDn|Home|End|Esc|Shift+Tab|Ctrl[-+]X`
以及箭头。**`Enter` 不是 code token**，`Esc` 是——所以三语必须都逐字带 `Esc`
（上表已满足），`Enter` 可自由本地化但这里也保持原样。新串无反引号、无
`/slash`、无箭头。

`i18n_test.go:51` 钉住的三个子串（`Revise plan` / `修改计划` / `修改計畫`）与
`2.` 前缀（解析器 `chat_tui.go:3272-3278` 只收首两字节为数字加点的行）在新措辞
中均保留。`ChatStatusPlanApproval` 不动——它的 token 集已被钉死，且改它不产生
新信息。

### 2.6 横幅与页脚

- **横幅**（`renderApprovalBanner`，`chat_tui.go:3867`）：打字态下追加一行显示
  已输入内容（复用 `rowLine`，`chooser.go:308`），并把末行提示换成
  `ApprovalNoteHint`。

  注意现有末行 `"↑/↓ navigate · Enter select · y/a/p/n shortcuts"` 是**硬编码
  英文**、未走 i18n。本 spec 不改它（超出范围），但也不在它旁边并排一句翻译过
  的提示——那会让面板一半英文一半中文。做法是**整行替换**：行选态显示原硬编码
  串，打字态显示 `ApprovalNoteHint`，任何时刻只有一种来源。

- **页脚**（`status_footer.go:125`）：在两个 `pendingApproval` arm **之前**插入
  打字态 arm，`switch` 从上往下匹配，顺序不能反。

### 2.7 `hideComposer`

`pendingApproval != nil` 目前无条件返回 true。改为 `(m.pendingApproval != nil && !m.approvalTyping)`
——与 chooser 打字态的例外（`chat_tui.go:2333`）同构：面板是 input-owned 时
输入框必须留着。该函数的文档注释（`:2317-2327`）要求「新增审批式面板时同时
更新本函数与模态布局测试」，本 spec 照办。

### 2.8 `refreshInputPlaceholder`

现在只认 `chooserTyping()`，其余一律置空（`chat_tui.go:745-751`）。加
`approvalTyping` 分支置为 `ApprovalNoteHint`；不加就会静默变成 `""`，说好的
占位符提示不会出现。

## 3. 非目标

- **不碰 `harness/control/`**。#328 拥有那里的每一行；本 issue 的 diff 不应出现
  该目录下任何文件。
- **不碰 `harness/cli/chooser.go`**。第 2.2 / 2.3 / 2.6 节从它抄形状，不改它。
- ACP 与 HTTP 客户端无法表达 feedback（`harness/acp/dispatch.go`、
  `harness/serve/serve.go:775-788`），继续走布尔 `Approve`；空 feedback 路径对
  它们必须行为等价。

## 4. 任务拆解（TDD，逐任务一次 commit）

**T1 — 收据 bug：三行改走 `ResolvePlanDecision`**
1. 建记录型 stub（嵌 `control.SessionAPI`，照 `extension_surface_test.go:18-19`
   的既有模式），写测例断言按 `3` 走 `PlanDecisionExitPlan` 且从不调用
   `Approve`。跑，预期 RED（今天无条件调 `Approve`）。
2. `answer` 里对 `planApprovalTool` 分流到 `ResolvePlanDecision`。
3. 跑，预期 GREEN；`TestPlanApprovalActionsSynchronizeTUIAndControllerMode`
   的 1/3/n/Esc 分支不变。commit。

**T2 — `promptsForText` + 打字态状态位**
1. `approvalChoice` 加字段并挂到计划 revise 行与 recovery revise 行；
   `chatTUI` 加 `approvalTyping bool` 与 `approvalTypingChoice approvalChoice`。
2. 更新 `TestApprovalChoicesPreserveDecisionSemantics` 的 `want`（该测例按结构体
   相等比较，新字段必须体现）。
3. `resolveApproval` 方法提取，`answer` 改为其薄封装。跑全包，预期绿。commit。

**T3 — 打字循环**
1. 写测例：选 `2` 后 `pendingApproval` 仍在、`approvalTyping` 为真、
   `hideComposer()` 为 false；打字期间 `1`/`y`/`a`/`n` 进输入框而非解析；
   Enter 提交裁剪后的文本；Esc 回到行选且不解析；空说明按
   `PlanDecisionRevisePlan` + 空串解析且计划模式保留。跑，预期 RED。
2. 在 `chat_tui.go:1435` 前插入 `approvalTyping` 路由块；`handleApprovalKey`
   顶部加不变量早返回；`hideComposer` 与 `refreshInputPlaceholder` 加分支。
3. 跑，预期 GREEN。commit。

**T4 — recovery note 透传 + 横幅/页脚 + i18n + 文档**
1. recovery revise 行的 typed text 传给 `ResolveRecovery`，测例断言不再是 `""`。
2. 横幅打字行与提示整行替换；页脚新 arm。
3. i18n 两个新键 × 三语 + 第 2 行改措辞；跑 `go test ./harness/i18n/...`。
4. `harness/docs/GUIDE.md:600`、`harness/docs/TOOL_APPROVAL_MODES.md:33` 更新。
   commit。

**T5 — 验收**：`go build ./...`、`go vet ./...`、
`go test ./harness/cli/... -race`（本机无 cgo 时降级为无 -race，并如实标注）、
`go test ./harness/i18n/... ./harness/control/...`。spec commit，开 PR。

## 5. 验收标准

- 按 `3` 经 `ResolvePlanDecision` 落 `PlanDecisionExitPlan`，且全程未调用
  `Approve`（记录型 stub 断言）。今天 RED。
- `TestPlanApprovalActionsSynchronizeTUIAndControllerMode` 中 `2` 不再命中
  `t.Fatal("plan approval was not resolved")`：新预期是 `pendingApproval` 仍在、
  `approvalTyping` 为真、`hideComposer()` 为 false；`1`/`3`/`n`/`Esc` 的计划模式
  结果不变。
- 打字循环三条：Enter 提交恰好是裁剪后的文本；Esc 返回行选且不解析；打字期间
  `1`/`y`/`a`/`n` 进输入框。空说明解析为 `PlanDecisionRevisePlan` + 空串，计划
  模式保留，与今天 `n`/`Esc` 一致。
- recovery revise 行把文本送进 `ResolveRecovery`，`chat_tui.go:3310` 不再硬编码 `""`。
- 未变的模态契约仍成立：非计划的 pending approval 仍隐藏输入框
  （`chat_tui_test.go:1960-1963` 原样通过），底部行数账仍平（`:1290`）。
- `TestApprovalChoicesPreserveDecisionSemantics`（`:1003`）与
  `TestPlanApprovalBannerShowsThreeExplicitActions`（`:1116`）按新措辞更新并通过。
- i18n 完整：`TestCatalogsAgreeOnCodeTokens` 与
  `TestPlanApprovalChoicesExposeThreeExplicitActions` 通过——三语仍各恰好三个编号
  行，钉住的子串仍在。
- `go build ./...` 与 `go vet ./...` 干净。

### 5.1 已知残余

- 本 PR 的 base 是 PR #389 的分支而非 main。#389 若在评审中改动签名，本分支需
  跟随 rebase。#389 合入 main 后，本 PR 的 base 应切回 main。
- 本机无 C 编译器（`go test -race` 报 `-race requires cgo`），`-race` 由 CI
  ubuntu-latest 承担。
