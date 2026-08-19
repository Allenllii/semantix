# Attribution — Reasonix agent system vendor (harness/)

## 上游声明（MIT）

本目录（`harness/`）是 [Reasonix](https://github.com/Gnosil/DeepSeek-Reasonix) agent 系统的
vendor 副本，上游以 **MIT License** 授权（Copyright (c) 2026 Reasonix Contributors，
见上游 `LICENSE` 全文，MIT 许可正文随附于本目录 `LICENSE.reasonix.md`）。

- 来源仓库：`Gnosil/DeepSeek-Reasonix`（上游）+ `Allenllii/DeepSeek-Reasonix`（集成 fork）
- **vendor 基线 commit**：`Allenllii/DeepSeek-Reasonix` `feat/u33-tui-reuse-panel` @ `79b94e45`
  （含 U13c 合入后的 agent 主循环 / TUI / provider / control 骨架）
- 基线日期：2026-08-17
- 搬运范围：agent 系统运行必需闭包（`agent` / `tool` / `control` / `cli`(TUI) / `provider` /
  `event` / `config` / `memory` / `skill` / `permission` / `sandbox` / `store` 等 112 个包，
  详见 `docs/reports/issue-190-migration-source.md` 与 U38 实施记录）
- 模块路径改写：`reasonix/...` → `semantix/harness/...`（单 module 合并，不留 `reasonix` module）

## 与上游脱钩记录

| 日期 | 动作 | 说明 |
|---|---|---|
| 2026-08-17 | 合体路线决策（spec PR #181） | 停止在 DeepSeek-Reasonix fork 上改动，vendor 进本仓进程内结合 kernel |
| 2026-08-17 | U38 vendor（本目录） | 112 包闭包搬入 + 模块改写 + `cmd/semantix-agent` 入口；`patches/` 模式废弃（保留作历史） |

**脱钩后**：上游 Reasonix 的后续修复不再自动获得；重大上游修复按需手工 cherry-pick
（记录于上表）。本目录代码以本仓为准，不反向同步。

## 裁剪原则（spec `h2h3-resource-orchestration.md` §3）

- 只搬 agent 系统运行必需；desktop/、ACP、serve 等前端面在闭包内的按依赖保留
  （后续按需裁剪回补，见 U39/U40 边界）
- fork 的跨进程 semantix 桥（`internal/semantix/`）**不搬**——其职责由进程内接线替代
  （U39 复用面板数据源进程内化、U40 Decider 直连）；U38 为保持可编译冒烟临时纳入的
  桥代码在 U39 按 `docs/reports/issue-190-migration-source.md` 改写删除
