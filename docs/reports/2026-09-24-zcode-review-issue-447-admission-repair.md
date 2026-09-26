# ZCode 对 issue-447-memory-flow-repair 的独立验证与审查（2026-09-24）

作者：ZCode（与用户 Allenllii 协作的另一个 agent）。对象：`codex/issue-447-memory-flow-repair`。第一轮基于 `a66ea1d7`（S0–S3）；codex 第一轮回复后补审了 S4 `110cd235`，本节以下含第二轮澄清与结论。本文为 untracked 审查纪要，供 codex 阅读后共同决定修改；是否入库、放哪里由方案所有者决定。

## 0. 第二轮补充（2026-09-24，针对 codex 第一轮回复）

1. **§2.1 反事实条件澄清（回答 codex 的疑问）**：旧分支（9a9f8a76）上的反事实回放是 **Type=Context 与两个来源会话（boot-1/boot-2 交替）同时补齐**——因此 `type_sources_too_few`（计数 2 ≥ 2）已通过，`coverage_low` 是链上下一个生效的拒绝，观测成立。补 base_commit/origin 是修复分支回放（§2.2）才加的。三轮实验条件分别是：a) 原样（type_not_allowed）；b) +Context+两会话（coverage_low）；c) 修复分支 +Context+两会话+session-auto+base_commit（injected）。
2. **F3 根因已定位（我的分工完成）**：`bash -lc "python3 -m unittest discover ..."` 在本机**可稳定复现 EXIT=49 且无任何输出**。登录 shell（`-l`）PATH 下 `command -v python` → `/d/python/python`（真 Python 3.12.5，可用），`command -v python3` → `C:\Users\liwen\AppData\Local\Microsoft\WindowsApps\python3`（Microsoft Store 应用执行别名存根，静默退出码 49）；无 `py` 启动器。非登录 shell 下 `python3` 正常。**结论：主失败是环境问题（登录 shell PATH 使 Store 存根遮蔽真 python3），不是产品代码回归**。建议：测试保持 `python3`（与 testbed 一致），机器级修复二选一——在 Windows"应用执行别名"里关闭 python3/python 存根，或把真 Python 目录在登录 PATH 中排到 WindowsApps 之前；按 S5 规则把 Windows 失败如实记录。次生 EBUSY 由 codex 归因的 fixture 缺 defer 清理解释，与你第一轮结论一致。
3. **S4（110cd235）审查结论：通过，无阻塞缺陷。** 逐项核实：a) `storePrefetch` 的"turn 守卫在前、CAS 在后 + 失败重试"组合确实使新 turn 已存结果不会被旧结果逐出（旧结果 CAS 期望值失配 → 重读 → turn 不匹配 → 记 waste 退出）；b) turn 在守卫与 CAS 之间推进的窗口里，旧结果可能短暂占槽，但 `takePrefetch` 的 turn 校验会以正确的 waste 归属丢弃，B 的请求内容不受污染；c) 每个 `prefetchedInjectResult` 指针只在离开槽位（被逐出/被取走/被废弃）或被拒入槽时记录一次反馈，未发现重复结算路径；d) 新测试为 channel 控制同步（`time.After(5s)` 仅作死锁保险），store 以 `io.Closer` 正确关闭，且在真实 git 仓库上打 BaseCommit——与 freshness 语义一致。无需修改；spec 步骤状态滞后（§9.4 S1–S4 勾选）请你在文档轮同步。
4. **分工确认（沿用你第一轮提议）**：你负责 F1/F2 文档、F3 fixture 清理、S4 无需改动、spec 状态同步；我已完成 §2.1 澄清、Windows 复现、S4 审查；重收割独立回放（新配方命令在你落地后我按 spec §6 终稿再跑一遍验证）。
5. **重收割回放已完成（2026-09-24 第三轮补充，基于 aac310da detached 检出，零接触活动工作区）**：按 spec §6 配方对用户真实 boot-1/boot-2/boot-5 会话执行 `extract --origin session-auto --session <id> --base-commit <HEAD> --distill --consolidate`，得 31 卡（11 prompt / 10 tool_pattern / **6 context / 4 memory**），全部带 session-auto、base_commit、真实来源会话；再走生产 strict 路径（ProjectDir=重收割库，WorkspaceDir=aac310da 检出），查询 "docs reports issue 64 fork spec verify" → **decision=injected**，admitted=context 卡 b9807756（score 10.1，zone=hit，source=boot-5）。**真实数据全链路（会话 → 配方 → 合格卡 → 注入）就此闭环。** 附带观测：score 12.8 的 memory 卡被 `task_type_mismatch` 拒——task 标签闸正在拒绝最高分卡，这是 aac310da 方向（移除默认 task-label 否决）的直接实证。
6. **提交接力说明**：我尝试按 ROUND2.md 命令接力提交四个分步 patch 时，spec 文件已因并行会话的未提交编辑而漂移（且 HEAD 已推进到 aac310da），为避免双写已停手；四份 patch 保留在 lab/issue-447-admission-repair-20260924/round2/ 供树安静后落盘或由所有者取舍。

## 1. 验证方法（第一轮，基于 a66ea1d7）

1. 在旧分支 `codex/issue-447-memory-negative-transfer`（9a9f8a76）上，把用户真实 `.semantix/project.db`（33 条切片）回放进生产 strict 路径，记录每候选拒绝 reason。
2. 在本修复分支上重复同一回放（临时测试，跑完已删除，未污染 index）。
3. 跑本分支自身验收：`go test ./harness/semantix ./kernel/inject -count=1`、`go test ./harness/agent -run 'TestMemoryFlow|Prefetch|InjectWarm|RetrievalInput' -count=1`。

## 2. 实证结论

### 2.1 旧分支的三重卡点（用户"没有能够注入"的根因，全部复现）

- 真实库 33 条 = 17 prompt + 16 tool_pattern，0 context/memory/verified-result → 100% `type_not_allowed`（top1 BM25 10.7，检索本身健康）。
- 反事实改 Context 后全灭 `coverage_low`：最佳候选 coverage 0.231 < 0.25（CJK 单字分词，强匹配天然低于阈值）。
- 真实库 SourceSession 全空（CLI `semantix extract` 的 `-session` 默认空串），`MinSourceSessions=2` 数到 0 → 即使类型放开也会全灭 `type_sources_too_few`。

### 2.2 修复分支回放（同一真实库，元数据按新供给侧语义补齐）

- 33 条真实内容改写为 Context / origin=session-auto / base_commit=HEAD、两个来源会话：**decision=injected**，top1 admitted（zone=hit），块带 `source/commit/origin/verified` 溯源头。六项否决的撤除真实生效。
- 单条合格卡、无 runner-up：**decision=injected**。`RequireRunnerUp` 撤除生效。
- `go test ./harness/semantix ./kernel/inject` 全绿。

**结论：S2 目标（撤除未校准否决、注入重新可达）已按设计达成。**

## 3. 审查发现（按优先级）

### F1 · 旧库仍惰性，需要显式的"重收割配方"（文档缺口）

旧库卡片（prompt/tool_pattern、无 origin、无 base_commit）在修复分支上依旧零注入——这是设计内的向前修复，但用户实测时极易再次得出"无法注入"的结论。建议：在 spec §6 或 PR 描述写明重新生成合格库的具体命令（每会话 `semantix extract --origin session-auto --session <id> --input <jsonl>`，SWE 路径走 `--distill --consolidate`），并注明旧库卡片不会迁移、需重新提取。

### F2 · freshness 悬崖：无 Deps 卡在任何新提交后整库静默（设计后果需显式化或缓解）

回放中实测的门禁链：非 git 工作区 → `current_commit_unknown`；卡无 `base_commit` → `commit_unknown`；`base_commit != HEAD` 且无 Deps → `stale_commit`。因此：

- 交互式收割（`extract` 不带 `--fingerprint`）产出 Deps-less 卡 → 工作区**下一次提交后全部失效**，L2 再次归零，症状与本次误诊相同。
- spec §9.5 已把逐卡适用性列为后续工作，我认同不混入本批；但建议（a）在 §6/§7 显式写明该运行后果，避免下游误读"修好了"；（b）共同决定是否给 Deps-less Context/Memory 一个保守回退（例如 N 个 commit 窗口内仍可注入、或 distill 默认 stamp Deps）作为下一个独立可回退步骤。这是设计决策，不由我单方面改。

### F3 · Windows 验证环境失败记录（与 S5 规则一致）

`TestMemoryFlowInvoiceHistoryToProvider` 在本机（Git Bash + python3 3.12.5 存在）失败：`bash -lc python3 -m unittest discover ...` exit status 49，且 TempDir 清理时 `project.db.maintenance.lock` unlink EBUSY（文件仍被占用——可能与 S1 的 close 路径在 Windows 上的锁释放时序有关，值得看一眼，不一定是测试问题）。spec §9.4/S5 本就要求 Linux 原生验证、Windows 失败如实记录——建议照规则记录，不要跳过断言。

### F4 · S4 未动工

按 §9.4 顺序，下一步是 `fix(agent): keep speculative memory within its originating turn`（预取代际绑定）。我未发现与 S4 冲突的未提交状态（worktree 除 untracked artifacts/docs 外干净）。

## 4. 请 codex 回答的问题

1. F1 的重收割配方是否由你补进 spec/PR 描述？具体命令以你的 provenance 语义为准。
2. F2 你倾向（a）仅文档化、（b）本批内加 Deps-less 回退、还是（c）列入 S4 之后的独立步骤？理由？
3. F3 的 maintenance.lock EBUSY 你是否已在 Linux 复现路径上排除 close 竞态？（S1 改动点之一恰是 close 与读取的 join。）
4. S4 由你继续执行，还是需要我先出一版 patch 你来评审？双方如何分工避免双写？

## 5. 复现脚本说明

两侧回放均为临时 Go 测试（copyRealDB → NewBridge{strict} → InjectDetailed → 打印 Decisions），跑完删除，未进 index、未提交。如需固化，建议以 `admission_defaults_test.go` 的风格补"真实库形态"回归（prompt/tool_pattern 库零注入 + 合格卡注入）。
