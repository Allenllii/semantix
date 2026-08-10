# 贡献指南（Contributing）

感谢你参与 Semantix！本文档描述贡献流程与验收标准。

## 项目结构

```
kernel/          内核包（事件/切片/BM25/缓存/调度/预取/进化）
cmd/semantix     CLI 入口（extract / search）
docs/            架构与契约文档（events.md = 事件契约权威来源）
```

## 工作流（M0 阶段）

1. **认领单元**：在对应 Issue（如 `M0-2`）下评论认领（如 `U5 BM25`），避免重复劳动。
2. **建分支**：从最新 `feat/kernel-skeleton`（或 `main`）切出 `feat/uN-<主题>`。
3. **实现 + 测试**：每个单元必须带单元测试；接口变更先对齐 `kernel/` 已冻结契约。
4. **本地验证**（必过）：
   ```bash
   go vet ./...
   go test ./... -race
   git diff --check
   ```
5. **开 PR**：target `feat/kernel-skeleton`（合并入 main 前先汇主线）；标题 `U<N>: <描述>`；描述里勾选对应 Issue checklist 并写 `Fixes #N`（收尾单元）。
6. **合并**：至少 1 人 review；fork 提交者注意——PR head 在自己 fork 时，修改后需推回 fork 分支（主仓库同名分支不会更新 PR）。

## 契约纪律

- `kernel/event`：Kind 序号与 payload 字段名是 **wire 契约**，只增不改；改事件必须同步 `docs/events.md`。
- 依赖：保持零第三方依赖（MVP 原则）；确需引入时在 PR 描述说明理由。
- 敏感数据：store 文件权限 `0o600`；不得提交凭据/本地绝对路径。

## 验收标准（M0-Gate）

- `go vet ./... && go test ./... -race` 全绿
- 真实会话提取 ≥500 切片；search 相关率 ≥70%（人工抽 10 条 query）
- 事件契约与 docs/events.md 一致；安全 review 无 blocking 项
