# Semantix P3/P4 落地补丁 — sched/prefetch 接线到 Reasonix fork

> 日期：2026-08-14 · 交付：`semantix-sched-prefetch.patch`
> 目标仓库：`DeepSeek-Reasonix`（reasonix fork，模块 `reasonix`）
> 前置：U13/H1 已就位（`internal/semantix/sink.go` + `inject.go` 存在）；本补丁把 **U13 真正接线** 并落地 **P3 调度（sched）** 与 **P4 预取（prefetch）**。

---

## 1. 补丁内容（13 文件）

| 文件 | 类型 | 改动 |
|---|---|---|
| `internal/config/config.go` | 修改 | 新增 `SemantixConfig`（`[semantix]` 段：enabled/binary/inject/budget/sessions_dir） |
| `internal/semantix/bridge.go` | 新增 | `Bridge`：聚合 kernel 接线（配置 + HarnessSink 镜像包装 + Inject/Lookup 执行器），全 fail-open |
| `internal/semantix/sched.go` | 新增 | fork 侧规则调度器（P3 MVP）：并行分组 + 行为学习门 + tier 决策（与 `kernel/sched.RuleDecider` 同步的移植） |
| `internal/semantix/inject.go` | 修改 | 抽出 `execCLI`（3s 超时软降级），`Inject` 复用 |
| `internal/tool/semantix.go` | 新增 | `semantix_lookup` 工具（read-only，CLI 子进程，软降级为空结果） |
| `internal/agent/agent.go` | 修改 | `Options.Semantix` + Agent 字段（bridge/sched/prefetchedInject）+ `startInjectWarm`（N12 等待期预热） |
| `internal/agent/turnruntime.go` | 修改 | `injectBlock`：每轮锁定的 `[semantix-reuse]` 块 |
| `internal/agent/run_loop.go` | 修改 | `beginRunTurn` 首轮注入；`handleToolRound` 记录 tier |
| `internal/agent/sampling_request.go` | 修改 | 请求组装时注入块插在系统提示后；预热兜底 |
| `internal/agent/execute_batch.go` | 修改 | `executeBatch` 用 `plan.ParallelGroups` 替换静态 `partitionToolCalls`；工具结果回灌行为统计 |
| `internal/boot/boot.go` | 修改 | 构造 bridge + 挂 sink 镜像 + 注册 `semantix_lookup` + executor Options 传 bridge + ctrl 后 SetLabel |
| `internal/semantix/sched_test.go` | 新增 | 调度器测试（分组/黑名单/行为门/恢复/tier/MaxParallel） |
| `internal/tool/semantix_test.go` | 新增 | 工具测试（schema/缺参/软降级） |

配套的 kernel 侧权威实现（`semantix` 仓）：
- `kernel/sched/rule_decider.go` + 测试（12 个，`-race` 绿）
- `kernel/prefetch/matrix.go` + 测试（13 个，`-race` 绿）

---

## 2. 应用步骤

```bash
cd /path/to/DeepSeek-Reasonix
git apply --check /path/to/semantix/patches/semantix-sched-prefetch.patch   # 预检
git apply /path/to/semantix/patches/semantix-sched-prefetch.patch            # 应用
```

若上游 Reasonix 已漂移导致个别 hunk 冲突：`git apply --reject` 会生成 `.rej` 文件，按第 3 节设计说明手工合入即可。

---

## 3. 设计说明（每个接线点为什么这么做）

### 3.1 U13 接线（此前只有零件，无调用方）
- **配置**：`config.Config.Semantix` 走既有 TOML 解码，零解析代码。
- **sink 镜像**：`boot.go` 在 `sink = control.NewGoalUsageTee(...)` 之后 `semantixBridge.Sink(sink)` 包装——事件链最外层，所有前端（CLI/desktop/serve）事件都被镜像；`SetLabel(ctrl.Label())` 在 controller 创建后调用，JSONL 文件名用真实会话 label；镜像文件延迟创建、写失败仅丢弃该事件（`sink.go` 既有语义），**绝不阻塞主循环**。
- **工具**：`tool.NewSemantixLookup(binary)` 注册在 `reg := tool.NewRegistry()` 后，read-only，3s 超时，binary 缺失时返回空结果（软降级）。

### 3.2 P3 调度（sched）
- fork 不能 import `kernel/sched`（独立模块），因此 `internal/semantix/sched.go` 是 **RuleDecider 的移植**（约 130 行，含行为学习），kernel 侧保持权威实现 + 全测试。
- 挂点：`executeBatch` 开头 `a.decideRound(calls)` 一次；`a.planBatches` 把 `RoundPlan.ParallelGroups` 映射回 `toolCallBatch`，无 plan 时回退静态 `partitionToolCalls`（零风险 no-op）。
- 行为学习：每个工具执行后 `a.observeSched` 回灌成功率；低成功率只读工具被拆出并行组（候选门，N04）。
- **tier（flash/pro）MVP 局限**：`provider.Request` 无 Model 字段（模型在 boot 构造时绑定），fork 侧仅通过 `Notice` 事件标注 `sched tier=...` 供观测；真正的模型热切换需要 provider resolver 支持，留待后续。

### 3.3 P4 预取（prefetch）
- kernel 侧 `MatrixPrefetcher`（转移矩阵 + 浪费惩罚）已实现并测试，`sched.RuleDecider.SetPrefetchPlanFunc` 可挂载（kernel 仓内闭环）。
- fork 侧执行端：`startInjectWarm` 在 `streamWithFrozen` 拿到 provider 流后**后台预热注入块**（`context.WithoutCancel` 脱离流取消，内部 3s 超时），结果存 `atomic.Pointer[string]`；`buildSamplingRequest` 在同步注入缺失时用它兜底。这是"LLM 等待期填满只读预热"的 MVP 形态。

### 3.4 字节稳定（前缀缓存）
注入块在 `beginRunTurn` 按用户消息组装一次并锁进 `turn.injectBlock`，整个 turn 的所有轮次复用同一块 → 系统提示之后、用户消息之前的注入前缀字节稳定，L1 缓存持续命中。注入块以独立 `system` 消息插入（`prependSystemBlock`），不改写会话历史。

---

## 4. 验证步骤（应用后）

```bash
cd DeepSeek-Reasonix
go build ./...                     # 编译全绿
go vet ./internal/semantix/ ./internal/tool/ ./internal/agent/ ./internal/boot/
go test ./internal/semantix/ ./internal/tool/ -race   # 新增 11 个测试
```

端到端冒烟（kernel 二进制在 PATH 上）：

```toml
# reasonix.toml
[semantix]
enabled = true
inject  = true
budget  = 4096
```

```bash
reasonix run "跑一下测试"          # 观察 Notice: sched tier=...、.semantix/sessions/<label>.jsonl 生成
semantix extract --input .semantix/sessions/<label>.jsonl   # 会话切片入库
semantix lookup --query "跑测试"   # 下个会话可检索/注入
```

---

## 5. 风险与边界

- **行为学习状态在内存**：fork 侧 `Scheduler` 统计随进程生命周期；持久化/跨会话学习在 kernel 侧演进（`evolve`）后通过 CLI 协议回灌。
- **tier 未切模型**：如上，事件标注先行。
- **注入是低权威**：`[semantix-reuse]` 块内容来自切片库，kernel 侧 `inject` 已有 marker 转义防逃逸；fork 侧未再加过滤（信任 kernel 输出）。
- **每轮一次子进程**：inject/lookup 各 3s 上限，失败即空（fail-open）；高频工具轮下开销约一次进程启动，可接受，后续可换常驻 daemon。

---

## 6. 目标仓库漂移处理（2026-08-14 实测）

目标仓合并 upstream main-v2（desktop）且 `internal/tool/semantix.go` 已存在（`bf0d859` 恢复注册）后，应用本补丁注意：

1. `git apply --check` 会报 `internal/tool/semantix.go` / `semantix_test.go` "already exists"——**属预期**：目标仓现有实现（含 4 测试：schema / 缺参 / 软降级 / 子进程）已覆盖补丁同文件目标，跳过这两个文件即可：

   ```bash
   git apply --exclude=internal/tool/semantix.go --exclude=internal/tool/semantix_test.go semantix-sched-prefetch.patch
   ```

2. 补丁 `boot.go` 中的 `reg.Add(tool.NewSemantixLookup(cfg.Semantix.Binary))` 行需**移除**：目标仓现有 `semantix_lookup` 通过 `init()` `RegisterBuiltin` 注册（CLI + desktop 均生效），且该行引用的 `NewSemantixLookup` 构造函数在当前目标仓版本不存在。

3. 其余 11 文件可干净应用；应用后按 §4 验证。
