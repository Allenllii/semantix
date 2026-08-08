# Security Policy

Semantix 是架设在 agent harness 与资源之间的**自进化 Agent Kernel 层**（语义切片库 + 三层语义缓存 + 调度/预取 + 自进化）。因为它会读取并复用用户的代码、上下文与行为模式，**安全是核心设计约束**：本地优先、默认不可信、一切可回滚可审计。

本文档是安全策略与指南入口；详细的威胁模型、攻击面与对策见 [`docs/Security-安全设计.md`](docs/Security-安全设计.md)（10 章完整设计）。

> Semantix persists and reuses information across agent sessions. Its security boundary therefore includes not only code execution, but also memory isolation, cache validity, project scoping, and speculative work.

---

## Supported Versions（支持的版本）

| 版本 | 状态 | 安全支持 |
|---|---|---|
| `main`（设计/原型阶段） | 活跃开发 | ✅ 修复直接合入 main |
| 首个稳定发布（尚未发布） | — | 发布后：最新稳定标签 + 前一个次版本 |

> 项目当前处于设计阶段（架构文档 v2 完成，P0 未开工）。安全修复将优先合入 `main`，随下一个版本发布。

---

## Reporting a Vulnerability（报告漏洞）

我们支持**私密漏洞报告**，请勿在公开 issue / PR 中披露漏洞细节。

### 如何报告

1. **首选**：GitHub 私密安全通告（Security Advisory）——仓库 **Security → Report a vulnerability**；
2. **备选**：向维护者发送私信/邮件，标题注明 `[SECURITY]`。

报告时请尽量包含（when possible）：

- affected Semantix version or commit
- operating system and runtime details
- affected subsystem (cache, slices, scheduler, prefetch, storage, adapter, etc.)
- minimal reproduction steps using dummy data
- expected security impact
- relevant logs with secrets and personal data removed

### 响应承诺

- 维护者将尽最大努力评估并在公开披露前协调修复；
- 请允许合理的调查与发布准备时间后再公开漏洞细节（coordinated disclosure）。

---

## Security boundaries（安全边界）

以下为 Semantix 中的安全敏感行为。

### Project and user isolation

语义切片、可复用结果、嵌入向量、索引与学习到的行为**不得跨用户/项目/工作区/租户泄漏**，除非显式配置允许共享。

典型安全问题：

- cross-project slice retrieval
- cross-user memory leakage
- project identity confusion
- namespace collisions that expose unrelated cached data

### Result reuse and invalidation

L3 结果复用必须 **fail closed**：仅当 Semantix 能证明结果对当前请求与项目状态仍然有效时才允许复用。

安全相关失败：

- stale result reuse after dependent files change
- reuse with mismatched project identity
- reuse across incompatible configuration or model state where that difference affects correctness
- cache poisoning that causes attacker-controlled results to be treated as trusted reusable results

### Persistent memory and semantic slices

持久化状态可能包含敏感的项目/用户信息。

安全问题：

- secrets written to persistent indexes without intended handling
- private file content exposed through logs or diagnostics
- unauthorized retrieval of stored user preferences or project knowledge
- unbounded retention of data that the system claims to delete or isolate

### Prefetch（预取）

投机预取必须保持**只读**，除非未来设计引入显式的、可审查的安全机制。

Semantix 不得执行投机性副作用：编辑文件、发送消息、变更远端 API、提交代码、部署、审批工具调用等。

### Integrations and adapters

Agent harness、模型提供方、embedding 提供方、数据库与工具适配器都可能收到 Semantix 管理的数据。当 Semantix 将数据送出预期边界、或绕过显式用户/项目作用域时，即构成漏洞。

### Trusted local inputs

除非其他漏洞允许不可信角色控制它们，由操作者显式提供的本地配置一般视为可信。

当报告能证明以下任一项时即具有安全相关性：boundary bypass、unintended disclosure、unauthorized persistence、unsafe reuse、或超出操作者显式意图的副作用。

---

## Coordinated disclosure

维护者将尽最大努力评估并协调修复，再公开披露。请允许合理的调查与发布准备时间（best-effort assessment and coordination before public disclosure）。
