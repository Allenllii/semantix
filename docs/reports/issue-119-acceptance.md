# Issue #119 验收报告 — M2-U25: install 命令（agent-skill 安装器）

> 状态：验收通过（2026-08-14）。对应 Issue：`#119 M2-U25: install 命令（agent-skill 安装器）`。
> 依赖：U19 命令树（`feat/cli-v2-u19-u24` 分支）。
> 架构真源：`docs/reports/cli-v2-architecture.md`（§3 命令树、§4.2 JSON 信封、§4.3 退出码契约、§7 验收总纲）。
> 验收方式：独立只读 subagent 详细验收 + 主 agent 逐条复核 + 修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 分支 | `feat/u25-install`（基于 `feat/cli-v2-u19-u24`） |
| commit | `aee10df` feat(cli): U25 install command；`aef010e` test(cli)；`ea21225` docs(quickstart)；`3854d26` fix(cli): 验收复核修复 |
| diff 范围 | `cmd/semantix/install.go`（+413）、`install_test.go`（+312）、`main.go`（+8/-1）、`main_test.go`（+3）、`doctor_test.go`（+2/-1）、`docs/QUICKSTART.md`（+19/-2） |

## 2. 验收标准逐条核对（issue #119 原文 + 架构文档 §7）

| # | 验收标准 | 状态 | 证据 |
|---|---|---|---|
| c1 | `semantix install --target reasonix\|claude-code\|custom` 按 agent-skill/ 现有文档落盘 skill + 工具 schema | 通过 | 实测落盘 6 文件（SKILL.md、tools/semantix-lookup.md、hooks/session-bypass.md、config/、scripts/×2）字节一致；默认目录正确（reasonix→`~/.semantix/agent-skill`，claude-code→`~/.claude/skills/semantix`，custom 无 `--dir`→exit 2）；`TestInstallCopiesPayload` |
| c2 | 幂等：重复安装可安全重跑 | 通过 | 第二次运行 6×`[unchanged]`、0 次写盘、exit 0（单测 + 二进制实测）；`TestInstallIdempotent`、`TestInstallUpdateOverwritesChangedFiles` |
| c3 | 提供卸载说明或 `--uninstall` | 通过 | `--uninstall` 精确移除 manifest 记录文件、用户文件保留、二次卸载安全 no-op、manifest 自删、空目录自底向上裁剪；QUICKSTART 有文档；`TestInstallUninstall*` 4 用例 |
| c4 | 引用 agent-skill/SKILL.md 与 hooks/session-bypass.md | 通过 | 三个 target 的 next-steps 均引用两文件（修复后 claude-code 补齐 session-bypass.md 引用）；payload 本身含两文件；`TestInstallCopiesPayload` 断言输出包含 |
| 附 | §7 总纲：`go test ./...` 全绿 / `go vet` 通过 / help 正确 / 退出码契约 / 新增命令有单测 | 通过 | 17 包全 ok；`go vet ./cmd/semantix/` 通过；help 输出 install 已挂载、planned 仅剩 `init config version completion`；14 个 install 单测全过 |

## 3. 独立 subagent 验收与复核

验收由只读 subagent 独立执行（静态审查 + 二进制实测 + 分级缺陷清单），主 agent 逐条对照修复并复验。

| 发现 | 分级 | 复核结论 | 处置 |
|---|---|---|---|
| B-1：显式 `--source` 非法时静默回退到 env/exe/cwd，可能装错 payload | 必须修 | 属实（实测 `--source <bogus>` 从 cwd 安装成功） | **已修复**（`3854d26`）：`--source` 非空即为终局，非法直接 exit 1；新增 `TestInstallExplicitSourceIsFinal` |
| B-2：uninstall 盲信 manifest 路径，`../` 可删 dest 之外的文件 | 必须修 | 属实（实测穿越删除外部文件） | **已修复**（`3854d26`）：`installPathInDest` 校验每条目（拒绝绝对路径与 `..` 组件），非法条目跳过并提示；新增 `TestInstallUninstallRejectsTraversal`，实测受害文件完好 |
| B-3：`--json` 失败路径无 §4.2 信封（stdout 为空） | 必须修 | 属实（`doctor.go` 已有先例） | **已修复**（`3854d26`）：`installFail` 输出 `ok:false + error:{code,message}` 信封，退出码不变；新增 `TestInstallJSONFailureEnvelope` |
| C-1：损坏 manifest 被降级为成功（exit 0） | 设计风险 | 属实 | **已修复**（`3854d26`）：损坏/不可读 manifest 与 stat/remove 意外错误 → exit 1；新增 `TestInstallUninstallCorruptManifest` |
| C-2：claude-code next-steps 未引用 hooks/session-bypass.md | 设计风险 | 属实 | **已修复**（`3854d26`）：三 target 行为一致 |
| C-3：复制中途失败留下无 manifest 的孤儿文件 | 设计风险 | 属实 | **已修复**（`3854d26`）：部分复制仍写 manifest，`--uninstall` 可清理 |
| C-4：dest==src 自安装后 `--uninstall` 清空 payload | 设计风险 | 属实 | **已修复**（`3854d26`）：`installDestOverlapsSource` 拒绝 dest 与 src 相同或为其子目录；新增 `TestInstallRefusesSourceOverlap` |
| D-1：新增文件 LF vs 仓库 CRLF（gofmt 噪声） | 可记录债务 | 预存条件（基座分支同样全列），新增两个文件 gofmt 干净 | 不修，后续单独提交 `.gitattributes`（`*.go text eol=lf`） |
| D-2：畸形 manifest / 穿越 / dest==src / json-失败路径无测试 | 可记录债务 | 属实 | **已闭合**：B/C 修复同步补齐 5 个新测试 |

## 4. 验证命令记录（worktree `D:/semantix/.worktrees/u25-install`）

```
go test ./...                                      全绿（17 包）
go test ./cmd/semantix/ -run 'TestInstall' -v      14/14 PASS
go vet ./cmd/semantix/                              通过
go build -o $TMP/semantix-e2e-u25/semantix.exe     BUILD OK
二进制实测：
  install claude-code ×2                            → 6×[installed] → 6×[unchanged]，exit 0（幂等）
  install --json                                    → 信封 ok=true、error:null、files≥6，jq 可解析
  --uninstall ×2                                    → 6×[removed] → "nothing tracked"，exit 0（二次安全）
  外来文件保留 / --target bogus、custom 无 --dir、无 --target → exit 2
  --source 缺失 / --source 非法（含有效 env 回退） → exit 1（修复后不再静默回退）
  --json + source 缺失                              → ok:false + error:{code:1} 信封（修复后）
  穿越 manifest（"../victim.txt"）                  → 条目跳过 + 提示，受害文件完好，安全条目仍移除（修复后）
  损坏 manifest                                     → exit 1（修复后）
  dest==src                                         → exit 1 拒绝（修复后）
```

## 5. 未闭合风险与后续边界

- 本机（Windows）无 `go test -race`（无 cgo，基座环境限制，非本分支引入）；`-race` 需在 CI/类 Unix 主机补齐。
- 分支基于 `feat/cli-v2-u19-u24`（U19+U24）；合并落地时需 rebase/merge 到 main，`main.go` plannedByGroup 与 QUICKSTART 若与后续命令冲突需同步。
- 债务 D-1（行尾统一）已记录，不影响本 issue 验收标准。
