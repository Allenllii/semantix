# CLI v2 架构：从 M0 验证工具到产品级 kernel CLI + harness 集成层

> 日期：2026-08-14 · 状态：规划 · 对应：M2 / U19-U27
> 一句话目标：把现有 8 个验证导向子命令升级为**四组命令树 + 统一横切契约**（--json / 退出码 / config 接线 / 日志），让 CLI 同时服务三类用户：**人**（运维与调试）、**harness**（子进程协议，H1 已定义）、**CI**（门禁与回归）。

---

## 1. 现状盘点（已做，v0.3.1）

`cmd/semantix` 现有 8 个子命令，Go 标准库 `flag` 手写 switch 分派，均有测试（`go test -race` 全绿，13 包）：

| 命令 | 用途 | 备注 |
|---|---|---|
| `extract` | 会话 JSONL → 语义切片 | `--fingerprint`、`--scope` |
| `search` | 语义检索 | bm25 / vector / hybrid（RRF） |
| `verify` | 离线回放验证（M0-2 回放） | 退出码 0/1/2/3，3=门禁未达标 |
| `eval` | 单检索 vs 三段灰色地带策略比较（Issue #7） | — |
| `eval-judge` | judge 真实性评估（Issue #8） | `--stub` CI 模式 |
| `usage` | L2/L3 成本节省统计（Issue #60） | — |
| `lookup` | 单查询检索（`semantix_lookup` 工具后端） | **H1 子进程协议入口** |
| `inject` | L2 注入块生成（`[semantix-reuse]`） | **H1 子进程协议入口** |

已接入的横切能力：`flags.go` 统一 zone 阈值 flag（灰色地带三段）。

### 1.1 关键缺口（按严重度）

| # | 缺口 | 影响 |
|---|---|---|
| G1 | `--json` 输出缺失 | H1 协议要求 `semantix lookup --json`；CI/工具集成无机器可读输出 |
| G2 | `semantix.toml` 未接线 | 示例配置注释明言"仅文档用途"；`--db` 等默认值硬编码在代码里 |
| G3 | 无 `version` / `init` / `config` 命令 | 无法自举、无法核对生效配置；版本诊断困难 |
| G4 | 无 shell completion | 命令增多后人工记忆成本上升 |
| G5 | 无 `doctor` 健康检查 | db 损坏 / embedder 缺失 / judge 配置错误只能靠报错发现 |
| G6 | 退出码与帮助文本未系统化 | 部分命令 3=门禁、部分 0/1/2；`help` 只列 6 条命令 |
| G7 | 无维护命令 | 切片库无 gc / 备份 / 迁移，长期运行数据膨胀 |
| G8 | 无日志/verbose | 排障靠 stdout 混排 |

---

## 2. 设计原则

1. **子进程协议是第一公民**：H1（`docs/reports/h1-mount-design.md`）已把 `semantix` 定义为 harness 的零耦合调用面。CLI 的每次改动不得破坏 `lookup --json` / `inject` 的协议兼容（3s 超时、失败静默降级由 fork 侧保证，CLI 侧需保持低启动延迟与确定性输出）。
2. **可脚本化**：所有输出优先结构化（`--json`），人读文本只是默认展示层。
3. **稳定契约**：退出码语义、JSON 信封、flag 命名一旦冻结即向后兼容；新字段只能追加。
4. **失败降级**：CLI 自身故障（db 损坏、config 非法）返回明确错误码，绝不 panic 或输出半结构。

---

## 3. 命令树（目标形态）

```
semantix
├── kernel 运维（已有 8 命令，行为不变）
│   ├── extract    extract 会话 JSONL → 切片
│   ├── search     语义检索
│   ├── verify     离线回放验证（门禁）
│   ├── eval       检索策略比较（Issue #7）
│   ├── eval-judge judge 真实性评估（Issue #8）
│   ├── usage      成本节省统计
│   ├── lookup     单查询检索（harness 工具后端）
│   └── inject     L2 注入块生成（harness 后端）
├── 产品与管理（新增）
│   ├── init        生成 semantix.toml + .semantix/ 目录骨架
│   ├── config      打印/校验当前生效配置（含来源标注）
│   ├── version     版本 + commit + 构建时间（--json 可选）
│   ├── doctor      健康检查（db / config / embedder / judge）
│   ├── install     把 agent-skill 安装到目标 harness
│   └── completion  bash / zsh / fish 补全脚本
├── 维护（新增）
│   ├── gc          清理过期/低分切片（按 retention + 评分阈值）
│   ├── export      切片库导出（JSONL 备份，含 Meta）
│   └── import      从导出恢复
└── 服务模式（P2 可选，见 §6）
    ├── serve       常驻本地服务（unix socket）
    └── watch       订阅事件流（usage / evolution 信号）
```

---

## 4. 横切契约（本次所有命令统一遵守）

### 4.1 全局 flag（每个子命令都接受）

| flag | 说明 | 默认 |
|---|---|---|
| `--config <path>` | 配置文件路径 | `./semantix.toml`（不存在则跳过） |
| `--db <path>` | 切片库路径（覆盖 config） | config 值 → `.semantix/project.db` |
| `--json` | 结构化输出 | false |
| `--verbose` / `--log-level` | 日志（stderr，不污染 stdout） | off |

### 4.2 JSON 信封（`--json` 输出统一）

```json
{
  "ok": true,
  "command": "lookup",
  "data": { "...命令私有字段，只追加不删除..." },
  "error": null,
  "version": "0.3.1"
}
```

失败时 `ok:false` + `error:{code,message}`，退出码仍按 §4.3。

### 4.3 退出码契约（全命令统一）

| 码 | 语义 | 用例 |
|---|---|---|
| 0 | 成功 | — |
| 1 | 运行错误（IO、db、检索失败） | 排障 |
| 2 | 用法错误（未知命令、flag 非法） | shell 脚本 |
| 3 | **门禁未达标**（verify 命中率、eval-judge 一致性、doctor 检查项失败） | CI |

### 4.4 配置加载优先级（G2 修复）

```
CLI flag  >  env（SEMANTIX_CONFIG / SEMANTIX_DB / …）  >  semantix.toml  >  内置默认
```

`semantix.example.toml` 的 `[project] [store] [retrieval] [inject] [verify] [cost]` 全部字段映射到各命令默认值；`semantix config` 输出每项的实际来源（flag/env/file/default），消除"配置不生效"类问题。

---

## 5. 演进顺序与依赖

```
M2-第一批（P0，并行/ready）
  U19 命令树重构 + 退出码/帮助系统化   ← 契约先行
  U20 config 加载接线                  ← 无依赖，可并行
  U22 --json 结构化输出                ← 依赖 U19 的 JSON 信封约定；H1 协议依赖项
M2-第二批（P1）
  U21 init / config / version          ← 依赖 U20
  U23 shell completion                 ← 依赖 U19（命令树冻结）
  U24 doctor 健康检查                  ← 依赖 U20
  U25 install（agent-skill 安装器）    ← 无依赖
  U26 gc / export / import             ← 无依赖
P2 可选
  U27 serve / watch 常驻模式           ← 依赖 U22；子进程协议已可用，本项是延迟优化
```

并行约束：各 issue 文件面零重叠（U19 改 `main.go` 分派与帮助；U20 新增 `config` 包；U22 改各命令输出层；其余新增独立文件）。

---

## 6. 服务模式（U27，P2 可选）

H1 子进程协议（每次调用一个 `semantix` 进程）已满足闭环需求。serve/watch 仅在以下信号出现后启动：

- harness 单会话内 lookup/inject 调用 > 5 次（启动开销可测）；
- 需要跨进程共享内存索引（embedding 缓存）；
- 团队需要实时事件流（usage/evolution）做仪表盘。

设计：unix socket `semantix.sock` + 单实例锁（`flock`）+ JSON-RPC 子集（`lookup` / `inject` / `usage.snapshot`），协议与 CLI flag 一一对应，保证 serve 与 CLI 同构可切换。**默认不做，保持简单。**

---

## 7. 验收总纲（每 issue 完成时须满足）

- [ ] `go test -race ./...` 全绿；新增命令有单测（分派/flag 解析/退出码/JSON 信封）
- [ ] 每个命令 `--help` 可用；`semantix help` 按四组列出全部命令
- [ ] `--json` 输出可被 `jq` 解析，信封字段符合 §4.2
- [ ] 退出码符合 §4.3 契约
- [ ] H1 协议命令（lookup/inject）行为向后兼容：3s 内返回、失败静默降级
- [ ] `semantix doctor` 全绿（仅 U24 后要求）
