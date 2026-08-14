# Issue #117 验收报告 — M2-U23: shell completion（bash/zsh/fish）

> 状态：验收通过（2026-08-14）。对应 Issue：`#117 M2-U23: shell completion（bash/zsh/fish）`。
> 依赖：U19（命令树冻结，`feat/cli-v2-u19-u24` 分支）。
> 架构真源：`docs/reports/cli-v2-architecture.md`（§3 命令树、§4.3 退出码契约、§7 验收总纲）。
> 验收方式：独立只读 subagent 详细验收 + 主 agent 逐条复核 + 修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 分支 | `feat/issue-117-completion`（基于 `feat/cli-v2-u19-u24`） |
| commit | `c3e34a6` feat(cli): U23 shell completion（实现 + 测试）；`65398ed` docs(quickstart): 加载说明；`b19a1cf` fix(cli): 验收复核修复 |
| diff 范围 | `cmd/semantix/completion.go`（+280）、`completion_test.go`（+250）、`main.go`（+78/-…）、`doctor_test.go`（+2/-1）、`docs/QUICKSTART.md`（+28/-3） |

## 2. 验收标准逐条核对

| # | 验收标准（issue #117 原文 + 架构文档 §7） | 状态 | 证据 |
|---|---|---|---|
| c1 | `semantix completion bash\|zsh\|fish` 输出补全脚本；退出码契约：`--help`→0，缺参/未知 shell/多余位置参数→2 | ✅ | `TestCompletionUsageContract` 7 用例全过；二进制实测 `completion tcsh`→exit 2、无参→exit 2、`--help`→exit 0 |
| c2 | 覆盖全部子命令与主要 flag（含枚举值 `--scope`/`--retriever`/`--embedder`/`--stub`/`--judge-protocol`），与 `--help` 实际 flag 无漂移 | ✅ | `TestCompletionScriptsCoverTree` + `TestCompletionFlagsMatchCommandHelp`（行级精确匹配锁）全过；bash 实机：`search --ta`→`--tau-high --tau-low`、`lookup --abs-l`→`--abs-low`、`inject --tau-h`→`--tau-high` |
| c3 | QUICKSTART 说明加载方式（bash/zsh/fish） | ✅ | docs/QUICKSTART.md「Shell completion」章节：`source <(semantix completion bash)` / zsh compinit+source / `semantix completion fish \| source` |
| c4 | 脚本在真实 shell 可加载可触发 | ✅ | bash 实机 8 场景全过（命令/flag/枚举值/help/未知命令不崩溃）；zsh `'\''` splice 转义与 fish `\'`/`-l` 惯用法静态核验；`TestCompletionZshFishSyntax` 在有 shell 环境自动跑 `zsh -n`/`fish -n` |
| c5 | `go test ./...` 全绿、`go vet` 通过、无无关文件混入 | ✅ | worktree 中 `go test ./...` 全包通过（cmd/semantix 6.76s）、`go vet ./cmd/semantix/` 通过；diff 仅 5 个本任务文件（`site/public/llms-full.txt` 为会话前遗留用户改动，未纳入） |

## 3. 独立 subagent 验收与复核

验收由只读 subagent 独立执行（静态审查 + 分级缺陷清单），主 agent 逐条对照真实代码与编译器行为复核：

| 发现 | 分级 | 复核结论 | 处置 |
|---|---|---|---|
| B-1：`main.go` init() 填充注册表"循环理由不成立"（subagent 依据 Go 规范推断命名函数调用不追溯函数体） | 设计风险 | **subagent 误判**。`$TMPDIR` 最小复现实证：`var commands = build()`（build 体内闭包引用 runCompletion→list→commands）在 Go 1.26.5 编译器报 `initialization cycle for commands`（与实现期报错一致）。Go initorder 分析确实追踪初始化表达式调用链中的函数体与闭包 | 不改，保留 `init()` 方案与注释 |
| B-2：search/lookup/inject 的 `completionFlags` 缺 zone 阈值 flag（`--tau-high/--tau-low/--abs-high/--abs-low`），其 FlagSet 经 `addZoneFlags` 真实注册，与 verify/eval 覆盖不一致 | 设计风险 | 属实（search.go:37、lookup.go:26/96） | **已修复**（`b19a1cf`），三命令各补 4 个 zone flags |
| B-3：`TestCompletionFlagsMatchCommandHelp` 子串匹配漏报（`-db` 可命中 `-user-db` 行） | 设计风险 | 属实（flag 包输出行 `-user-db string` 含子串 `-db`） | **已修复**（`b19a1cf`），改为 `helpShowsFlag` 行级 token 精确匹配 |
| B-4：zsh flags 无描述（`_describe` 仅 flag 名） | 可记录债务 | — | 不修（补全菜单可接受），后续迭代可加 |
| B-5：fish 全局 `-f` 禁文件补全（路径类 flag 无文件候选） | 可记录债务 | — | 不修（常见简化） |
| B-6：bash value-flag 分支 `--scope --` 时 compgen 空返回不 fallthrough 到 flag 列表 | 可记录债务 | — | 不修（边界行为可接受） |
| B-7：覆盖/确定性测试用 `strings.Contains` 宽松断言 | 可记录债务 | — | 不修（已被更强测试兜底） |

## 4. 验证命令记录（worktree `D:/semantix/.worktrees/issue-117-completion`）

```
go test ./cmd/semantix/ -run 'TestCompletion' -v        → 6/7 PASS、1 SKIP（本机无 zsh/fish）
go test ./...                                            → 全部 ok
go vet ./cmd/semantix/                                   → 通过
go build -o $TMPDIR/semantix-e2e/semantix.exe ./cmd/semantix
semantix completion bash|zsh|fish                        → 输出脚本（bash 含 tau-high 5 处）
bash 实机触发（source 脚本 + COMP_WORDS 模拟）           → 8 场景全过
```

## 5. 未闭合风险与后续边界

- 本机（Windows）无 zsh/fish，其脚本验证为静态核验；`TestCompletionZshFishSyntax` 在具备 zsh/fish 的 CI/主机上自动补齐运行级证据。
- 分支基于 `feat/cli-v2-u19-u24`（U19+U24），main 已推进（U19/U20 等已合入）；合并落地时需 rebase/merge 新 main，`completionFlags` 元数据若与后续命令冲突需同步（防漂移测试会拦截）。
- 债务 B-4～B-7 已记录，不影响本 issue 验收标准。
