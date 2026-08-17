# Harness Attribution

`harness/` 是从 DeepSeek-Reasonix vendor 进本仓的 agent 系统（合体路线：
`docs/specs/h2h3-resource-orchestration.md` §0/§3，2026-08-17 决策）。

## 上游

- 原始项目：[esengine/DeepSeek-Reasonix](https://github.com/esengine/DeepSeek-Reasonix)（MIT，`main-v2` 分支）
- 经由 fork：Gnosil/DeepSeek-Reasonix
- **脱钩基线 commit：`bf0d8594`**（fork `feat/h4-branding` 的干净基线，
  即「fix: restore semantix_lookup builtin registration」）
- 上游许可证原文：[LICENSE.upstream](./LICENSE.upstream)（MIT）

自该基线起本目录与上游**正式脱钩**：上游后续修复不再自动同步，
重大修复按需手工 cherry-pick 并在本文件追加记录。

## 搬运清单（U38，2026-08-17）

**依据**：`cmd/reasonix` 编译闭包分析——`internal/` 90 个顶层包中 85 个在闭包内，
`internal/boot` 单独闭包即 84 个包（依赖图致密，入口级裁剪收益 ~5 包但需改造骨架，
违背蓝图「骨架不动」原则），故采用近全量 vendor。

| 内容 | 处置 | 理由 |
|---|---|---|
| `internal/`（88 包）→ `harness/internal/` | ✅ 搬 | 编译闭包 + 测试辅助（`testenv`/`worktree`/`appidentity` 虽在闭包外但被测试与 Tier B 契约引用，保留避免断链） |
| `internal/desktoplauncher` | ❌ 裁 | 桌面启动器，闭包外，desktop module 本期不搬 |
| `internal/semantix` | ❌ 裁 | 跨进程时代的 kernel 桥（CLI 子进程 sink/inject/sched 移植），由进程内接线替代（spec §4，U40 落地） |
| `docs/`、`release-notes/` → `harness/docs`、`harness/release-notes` | ✅ 搬 | `go:embed` 包，闭包内 |
| `cmd/reasonix` → `harness/entry` + `cmd/semantix-agent` | ✅ 改造 | 顶层命令无法 import `harness/internal`（Go internal 可见性），以公开 `entry` 包作接缝 |
| `desktop/`（独立 go module） | ❌ 不搬 | spec §3：#158 时再取 |
| `sdk/`、`npm/`、`tools/`、`benchmarks/`、`prod_test/` 等 | ❌ 不搬 | 闭包外 |

**Import 改写**：`reasonix/internal/...` → `semantix/harness/internal/...`、
`reasonix/docs|release-notes` → `semantix/harness/docs|release-notes`（纯前缀替换）。
字符串字面量中的 `reasonix/...`（worktree 分支名模板、ACP vendor method 名等）
按 Tier B 不改清单保留（改动 = 独立迁移 spec，见 `docs/specs/h4-branding.md`）。

**品牌与视觉**：本次 vendor 保持上游原样（二进制自报 `reasonix`）；
Semantix 品牌换皮由已验证的 H4 patch 在 U39 套用（`git apply --directory=harness`）。

## 集成层再移植（U38/U39/#177/#191，本 PR，2026-08-17）

本 PR 以 `harness-integration` 为基（保留其增强版 `kernel/sched` + `kernel/event`
ResourceCatalog），将 `harness/` 换成 #206 的 `internal/` 布局 vendor，并把原先叠在
扁平 vendor 之上、#206 未含的 semantix 集成层**再移植**到 internal/ 布局：

| 集成块 | 落点 | 来源（`upstream/harness-integration`） |
|---|---|---|
| semantix↔kernel 桥（**进程内**，非已删的 CLI 子进程版 protocol.go/inject.go/sched.go） | `harness/internal/semantix/{bridge,reuse,sink}.go` | `60f25ab`（in-process 版，含 `aa10a6f`/`f038c2f` 改写） |
| U39 reuse panel + semantix 主题 + resource-gauge 座 | `harness/internal/cli/{reuse_panel,resource_gauge,theme}.go` | `aa10a6f`（Issue #190） |
| U38 task-time（任务计时） | `harness/internal/agent/{reuse,taskstate,turn_phase}.go` | `5102353`（Issue #177） |
| 资源编排（Decider/资源目录/tier） | `harness/internal/agent/{resources,tier,execute_batch}.go` + `kernel/sched` | `f038c2f`（Issue #191） |
| bridge/agent/orchestration 的 boot 接线 | `harness/internal/boot/boot.go` | `60f25ab`/`f038c2f` |

模块路径统一改写 `semantix/harness/<pkg>` → `semantix/harness/internal/<pkg>`。
已删的三文件（protocol.go/inject.go/sched.go）**不复活**——reuse/inject 为进程内
kernel 读取，编排走 `kernel/sched`。本 PR 取代 #206（采纳其 vendor），并保留
U39/#177/#191 的既有功能。
