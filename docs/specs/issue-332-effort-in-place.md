# Spec — Issue #332 `/effort` 原地生效，不再重建 controller

## 1. 目标

`/effort <level>` 从"写配置 + Snapshot + 迁移 session lease + 重建整个 controller"改为 `/reasoning-language` / `/preset` 的原地模式：**先持久化用户配置（保留），再调用 `m.ctrl.SetEffort(...)` 应用到活 controller**。前置条件 #330（`control.Settings.SetEffort` + `Controller`/`Agent` 实现，PR #392 已合并）与 #331（anthropic/responses 适配器 honor `Request.EffortOverride`，PR #397）均已就绪。

## 2. 新流程（`harness/cli/effort.go` 重写 `runEffortCommand`）

保留：`currentConfigProvider()` 解析、capability gate、arg 分支（无参显示/超参 usage）、`config.NormalizeEffort` 校验、锁定的配置写入（含 anthropic `SetProviderThinking("adaptive")` backfill）、`m.runtimeSwitchBusy()` 与 `m.modelSwitchPending` guard（setter 契约要求 active-work guards）。

删除：`Snapshot()`、`History()`、`rebindSessionLease()`、`pendingModelSwitch` 构建（原 `effort.go:88-115` 整段）。

新增：配置写入成功后调用 `m.ctrl.SetEffort(level)`，其中 `/effort auto`（归一化后 `""`）映射为 `SetEffort("auto")`（显式选择 provider 配置深度，stand down governor），深度值原样传入；随后更新 `m.effortLevel`、经 `m.effortApplied` hook 通知 cli.go 更新捕获的构造 override，发成功 notice，`return nil`。

## 3. `--effort` flag 优先级边角（cli.go）

`bindRuntimeRebuilder` 按值捕获 `overrides`（`runtime_rebuild.go:220-225`），`/reload` 时 `spec.EffortOverride` 恒为 nil（`runtime_rebuild.go:117-118`），因此启动 flag 会在 reload 后胜过后来的 `/effort`。解决：在 `m.buildController` 旁安装 hook `m.effortApplied(level)`（cli.go 内闭包更新 `overrides.Effort`）：深度值写入指针；`auto` 清为 nil（让 reload 读已清空的配置，回落 provider 默认）。

## 4. i18n

`effort.go` 现有 14 处硬编码英文全部移除，新增 key（三 catalog + `catalog_parity_test.go`）：

- `EffortErrorFmt`（错误前缀，%s = error）
- `EffortNotConfigurableFmt`（%s = provider）
- `EffortCurrentFmt`（%s name、%s current、%s default、%s options）
- `EffortUsageFmt`（%s = levels）
- `EffortNoConfigDir`（user config 目录解析失败）
- `EffortSwitchUnavailable` / `EffortSwitchBusy` / `EffortSwitchPending`
- `EffortSwitchedFmt`（%s = provider、%s = level）

`config.NormalizeEffort` 的原始英文错误按 #330 惯例直接透传（该层自有词汇表）。

## 5. slash registry

`harness/cli/slash_registry.go:53` 的 `/effort` 增加 `showInHelp: true`（与 `/reasoning-language` 对齐；completion dropdown 已存在）。

## 6. 测试计划

- `chat_tui_test.go` `TestEffortCommandWritesCurrentDeepSeekProvider`：cmd 为 nil、`modelSwitchPending` false、`pendingModelSwitch` nil，config 仍写 `effort = "max"`，且真实 `control.New` controller 的 `SessionEffort() == "max"`。
- `TestEffortCommandRejectsUnsupportedProvider`：保持（cmd nil + 不写 config），补 session 未变断言。
- `TestEffortCommandAutoClearsProviderEffort`：cmd 均 nil，config 清空，`SessionEffort() == "auto"`。
- `switch_recovery_test.go` `TestRuntimeSwitchesRejectRunningBackgroundJobs` effort case：强化为 cmd nil + `m.effortLevel` 不变 + busy notice 已发出。
- 删除 `TestEffortSwitchCarriesRecoveryPathAfterSnapshotConflict`（:244）与 `TestEffortSwitchMovesLeaseToRecoveryPathBeforeRebuild`（:571）——snapshot/lease 路径不复存在。
- `session_temp_rebuild_test.go` effort case：改为原地断言（不替换 controller）。
- `statusline_test.go` `TestRefreshEffortStatusUsesCurrentModel`：不变（`refreshEffortStatus` 未改）。
- 新增 `--effort high` 启动后 `/effort max`、再 `/model` 与 `/reload` 的 precedence 测试。

## 7. 兼容性

无 wire / on-disk 格式变更（config schema 与 `effort` key 不动）；`/model`、`/reload`、skill refresh 的 rebuild 路径不变（`controllerBuildSpec.EffortOverride` 保留）；serve 前端 `applyEffortEdit` 与 ACP `thought_level` 不受影响。

## 8. 验收命令

```sh
go build ./harness/... ./cmd/...
go vet ./harness/cli/... ./harness/i18n/... ./harness/control/...
go test ./harness/cli/... ./harness/i18n/... ./harness/control/...
git diff --check
```
