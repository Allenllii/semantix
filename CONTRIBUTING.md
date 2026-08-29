# 贡献指南（Contributing）

感谢你参与 Semantix！本文档描述贡献流程与验收标准。English speakers are very welcome — issues / PRs / reviews in English are fine, and we will reply in kind.

- 🗺️ 路线图：[`docs/Agile路线图.md`](docs/Agile路线图.md)（唯一事实来源）
- 🐛 报 bug / 提需求：[Issue 模板](https://github.com/Gnosil/semantix/issues/new/choose)
- 💬 讨论：[GitHub Discussions](https://github.com/Gnosil/semantix/discussions)
- 🔒 安全漏洞：不要开公开 issue，见 [SECURITY.md](SECURITY.md)

## 项目结构

```
kernel/          内核包（切片/BM25/embed/缓存/调度/预取/进化/事件/审计）
harness/         随仓的 agent runtime（provider/工具/权限/会话/UI），harness/semantix 为事件桥
gateway/         OpenAI 兼容网关（检索注入、L3 复用、fail-open 上游）
cmd/             三个入口：semantix（CLI）/ semantix-agent（agent）/ semantix-gateway
docs/            架构与契约文档（events.md = 事件契约权威来源；specs/ = 行为契约；reports/ = 验收报告）
site/            官网（semantix.ensureok.ai，Next.js 静态导出）
```

## 工作流

1. **认领**：在目标 issue 下评论认领，避免重复劳动。没有 issue 的新想法请先开 issue（或到 Discussions 提案）对齐方向。标了 `good first issue` 的是给新人的入门任务，动手前随时在 issue 里提问。
2. **建分支**：从最新 `main` 切出，命名 `feat/issue-<N>-<主题>` / `fix/issue-<N>-<主题>` / `docs/<主题>`。
3. **Spec 先行**：跨包行为变更（kernel↔harness 契约、事件、配置面）先在 `docs/specs/` 写清契约与验收，评审通过再写实现；纯 bug 修复与文档可省略。
4. **实现 + 测试**：每个行为变更必须带单元测试；新增事件 Kind / payload 字段只增不改。
5. **本地验证**（必过）：
   ```bash
   go vet ./...
   go test ./... -race
   git diff --check
   ```
   改了 `site/` 还需过 `cd site && npm run test:content`。
6. **开 PR**：target `main`；标题用 `feat(scope): 描述 (Issue #N)` / `fix(scope): 描述 (Issue #N)`，描述里勾选 PR 模板 checklist、写 `Fixes #N`；行为变更需附验收说明（报告放 `docs/reports/`）。
7. **合并**：至少 1 人 review；fork 提交者注意——PR head 在自己 fork 时，修改后需推回 fork 分支（主仓库同名分支不会更新 PR）。

## 契约纪律

- `kernel/event`：Kind 序号与 payload 字段名是 **wire 契约**，只增不改；改事件必须同步 `docs/events.md`。
- 依赖：保持零第三方依赖（MVP 原则）；确需引入时在 PR 描述说明理由。
- 敏感数据：store 文件权限 `0o600`；不得提交凭据/本地绝对路径。
- 文档同步：改 CLI 命令面/配置键要同步 `docs/QUICKSTART.md`；改架构行为要同步 `docs/Agent-Infra-架构设计.md`。

## 验收标准

- `go vet ./... && go test ./... -race` 全绿，CI 无红
- spec（如有）的验收清单全部勾选并在 PR 描述可追溯
- 事件契约与 `docs/events.md` 一致；安全 review 无 blocking 项
