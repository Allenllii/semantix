# Spec v1 — 会话级 reasoning effort 就地切换（Issue #330）

> 判级：Spec-Exempt。无 wire / 落盘格式变更——`provider.Request.EffortOverride`
> 本就是已声明字段（`harness/provider/provider.go:225`），config schema 不动；
> 无判定模型变更、无新顶层包。净增量是驱动端口加两个方法、Agent 加一个原子
> 字段、一个带契约注释的解析助手。
>
> 基线：upstream/main `db7a936`（含 #328 / #329）。

## 1. 问题

除 reasoning effort 外，每个会话运行时旋钮都有就地 setter；`/effort` 是唯一一个
为了改一个值要把会话拆了重建的。

### 1.1 真源审计（2026-08-23，worktree `feat/issue-330-set-effort`）

| 事实 | 证据 |
|---|---|
| setter 确实不存在 | `grep -rn SetEffort --include=*.go .` 全仓只有一处，还是 `harness/serve/serve.go:452` 的注释散文 |
| 要抄的模板 | `Controller.SetAgentPreset`（`harness/control/controller.go:2792`）→ runner 接口断言 + `c.executor`；Agent 侧 `harness/agent/agent.go:1203` 写进 `agentPreset atomic.Value`（`:401`） |
| effort 冻在构造期 | `boot.Options.EffortOverride` → `entry.Effort` → `provider.Selection{Effort: opts.EffortOverride}` |
| 运行时通道已在线且已被占用 | `harness/agent/sampling_request.go:131` 是 `EffortOverride: a.governorOverride(),`——**只读 governor，不读任何会话值** |
| governor 是默认关闭的实验 | `harness/agent/governor.go:17`：`var governorEnabled = os.Getenv("SEMANTIX_EXPERIMENT_GOVERNOR") == "1"`（注意：issue 正文写的是 `REASONIX_` 前缀，代码已随改名迁到 `SEMANTIX_`） |
| 请求按 round 冻结 | `samplingRequest` 文档自述「once-prepared, frozen … All stream retries replay this exact payload」；round 循环在 `harness/agent/run_loop.go:386` 重建 |
| 校验词表与传输词表不一致 | `config.EffortCapabilityForEntry` 会给出 `adaptive`/`enabled`/`none` 这类非深度档；`harness/provider/openai/effort.go:65-75` 的 `depthOnly` 恰好把它们剥掉 |
| 裸 string 说不出「回到 auto」 | `config.NormalizeEffort` 对 `auto` 返回 `("", nil)` |
| provider adapter 刻意不依赖 config | `grep -rln "semantix/harness/config" harness/provider/` **零命中** |

## 2. 已定的两处选型

issue 把这两条明确留给评审，本 spec 记录结论与理由，实现按此执行。

### 2.1 优先级：Option A —— 用户显式级别赢

`EffortOverride` 只有一个槽位。规则定为：**会话级别一旦设定（含显式 auto）
即生效，governor 只在未设定时生效。**

理由：governor 是 env 默认关闭的实验（`governor.go:17`），不是已交付的安全
不变量；真正的成本兜底在 scheduler 的预算路径（降 tier / 硬停 round），不靠
这个槽位；而一个实验静默压过用户的显式动作，是错误的默认。

代价：用户可以钉死 `max` 多花钱，边界由上述预算机制兜住。

### 2.2 一次性提示：不做

issue 第 8 步与选型绑定——「选 B 才做，或评审另有要求」。既然定了 A，用户的
命令本来就生效，没有要提醒的事。因此**不动 `event.go` 的 wire-stable 常量块**，
也就没有 `CONTRIBUTING.md:29` 要求的 `docs/events.md` 同步义务。

## 3. 设计

### 3.1 三态，因此必须是 `*string`

会话 effort 有三个行为各不相同的状态：

| 状态 | `EffortOverride` | governor |
|---|---|---|
| 未设定 | governor 的值（或空） | 生效 |
| 显式 auto | `""`（用 provider 配置的默认深度） | **被抑制** |
| 具体深度 | 该深度 | 被抑制 |

裸 `string` 分不出前两者——`NormalizeEffort("auto")` 返回 `("", nil)`，与「从未
设过」同形，用户将永远无法从 governor 手里要回 provider 默认值。因此 Agent 侧存
`atomic.Value` 持 `*string`。

端口的 `SessionEffort() string` 用 `"auto"`（显式 auto）与 `""`（未设定）区分，
保住 issue 指定的签名而不丢信息。

### 3.2 解析助手

`harness/agent/sampling_request.go:131` 由单一来源改为
`EffortOverride: a.effectiveEffortOverride()`。该函数的**文档注释逐字写明
§2.1 的规则**——那是将来唯一有人会看的地方，而两个方向上悄悄搞错正是本 issue
要防的失败模式。

```
未设定 → a.governorOverride()
已设定 → *sessionEffort（显式 auto 即 ""）
```

### 3.3 时序契约：下一个 model round，不是下一个 turn

in-flight 请求已冻结并在重试时逐字重放（`sampling_request.go:152-192`），所以
中途设定的级别最早在**下一 round 的重建**（`run_loop.go:386`）咬合。两个要写下来
而不是留给人踩的推论：

- **无工具调用的 turn 没有下一个 round**，级别落到再下一个 turn 的首个请求；
- **`executeBatch` → `applyScheduledTier` 那个缝不需要加任何东西**——到那时本 round
  的请求已经在线上，读它是惰性的。

### 3.4 Controller 侧的 entry 从哪来

`SetEffort` 需要 `*config.ProviderEntry` 才能校验，而 Controller 今天没有。

**选 `config.Load()` per call**，复用 `currentEffortEntry`（`harness/control/slash.go`）
的形状。issue 推荐的是「构建期缓存 + `/model` 切换时刷新」，本 spec 不采用：

- 缓存要同时改构建路径与 `/model` 路径，多两处可能失配的状态；
- 同样的磁盘读**今天已经在更热的路径上跑**——`effortArgItems` 的补全下拉每次
  按键都会调 `currentEffortEntry`。`SetEffort` 是斜杠命令，频率低若干量级。

若将来 profiling 证明它要紧，再升级到缓存，届时是纯内部改动。

### 3.5 校验：按传输真能承载的深度词表

只校验 `EffortCapabilityForEntry(entry).Levels` 会放进传输层随后静默丢弃的档位
（例如 MiniMax 的 `adaptive`、Zhipu/LongCat 的任意值）。

规则是「override 只调深度，从不调思考开关」，实现为 `harness/config` 的导出
助手，剥掉 `auto|enabled|adaptive|disabled|none|off`。

**为什么不直接从 adapter 导出**：`grep -rln "semantix/harness/config" harness/provider/`
零命中——provider adapter 刻意不依赖用户配置。让 `harness/control` 反过来 import
`harness/provider/openai` 也一样是破坏分层，且 `requestEffortVocabulary` 吃的是
内部类型 `effortEndpoint`（由 `provider.Config` 而非 `config.ProviderEntry` 构造），
导出它要连带导出那个类型。

**漂移守卫用仅测试的跨包对拍**：在 `harness/provider/openai` 的测试里 import
`harness/config`，对一张词表断言两侧结果逐一相等。测试文件的 import 不构成生产
依赖，分层不变，而任一侧改了规则都会翻红。

### 3.6 端口放 `Settings`

effort 是运行时会话设置，不是 goal/plan-mode 关切，因此声明在 `Settings`
（`harness/control/port.go:242`）而非 `Goals`。`port.go` 末尾的编译期断言块继续成立
即可——全树没有任何手写实现 `SessionAPI` 的 double，都是内嵌接口。

## 4. 非目标

- `/effort` 的 CLI 改接（另开 issue；被 `harness/cli/switch_recovery_test.go` 钉住重建）。
- `harness/acp` 的 `switchSessionEffort` 与 `harness/serve` 的 `/effort` 拦截改接。
- 任何 effort 的 mid-turn provider 热替换。
- 不动构造期路径：`boot.Options.EffortOverride` 仍是**起始**级别的来源，
  `SetEffort` 在其上叠加会话覆盖。

## 5. 任务拆解（TDD，逐任务一次 commit）

**T1 — config 侧深度词表 + 跨包对拍**
1. 写 `harness/config` 的表驱动测例与 openai 侧对拍测例，跑，预期 RED。
2. 实现导出助手。跑，预期 GREEN。commit。

**T2 — Agent 会话 effort 盒子 + 优先级助手**
1. 写 `harness/agent/effort_test.go`：优先级表（governor 引擎/未引擎 × 会话未设/
   显式 auto/具体深度），**每格都钉住 governor 状态**——同一字段两个来源，不钉
   就是构造性 flaky。跑，预期 RED。
2. 加 `sessionEffort atomic.Value`（持 `*string`）、`SetSessionEffort`/
   `SessionEffort`、`effectiveEffortOverride`（注释写死规则），改 `:131`。
3. 跑，预期 GREEN。commit。

**T3 — 时序契约测例**
1. 两 round 的 turn：round 1 请求冻结后设会话级别，断言 round 2 携带新值而
   round 1 不带；重试 round 的请求与首次 `reflect.DeepEqual`；单 round（无工具
   调用）turn 不受影响、级别落到下一 turn。commit。

**T4 — Controller.SetEffort / SessionEffort + 端口**
1. 写测例：不支持的 entry 报错且不改状态；`Levels` 里有但被深度过滤剥掉的档位
   报错且不改状态；成功后 `SessionEffort()` 可读回；`SetEffort("auto")` 与从未
   设过可区分。跑，预期 RED。
2. 实现两个方法 + `Settings` 端口声明 + i18n 三语错误串。
3. 跑，预期 GREEN。commit。

**T5 — 不重建 + 竞态**
1. 计数型 provider factory / tier resolver，断言 `SetEffort` 前后构造次数不变。
2. `-race` 下另一 goroutine 在 turn 运行中调 `SetEffort`，run 干净结束。
   （本机无 cgo，`-race` 由 CI 承担；本地跑无 -race 版本并如实标注。）commit。

**T6 — 验收**：`go build ./...`、`go vet ./...`、
`go test ./harness/agent/... ./harness/control/... ./harness/config/... ./harness/provider/openai/...`。
spec commit，开 PR。

## 6. 验收标准

- `Controller.SetEffort(level string) error` 与 `SessionEffort() string` 存在并
  声明在 `Settings`，`port.go` 编译期断言块仍成立。今天 RED（全仓只有注释）。
- 会话级别设定后，下一个 `provider.Request` 的 `EffortOverride` 等于该级别；
  清回 auto 后为 `""` 且 governor 仍被抑制；未设定时恢复 `a.governorOverride()`。
- **优先级表测例**：governor 引擎 × 会话三态共 6 格，每格都显式钉住 governor
  状态；规则与 `effectiveEffortOverride` 的文档注释逐字一致。
- **下一 round 生效**：两 round turn 中 round 1 不带、round 2 带。
- **冻结重放**：重试 round 的 `provider.Request` 与首次 `reflect.DeepEqual`。
- **无工具调用 turn**：不受影响，级别落到下一 turn 首个请求（断言而非假设）。
- `SetEffort` 对 (a) `EffortCapability.Supported` 为 false 的 entry、(b) 在
  `Levels` 中但被深度过滤剥掉的档位（如 MiniMax 的 `adaptive`），均返回错误且
  不改动任何状态。
- `SetEffort` 前后无 provider 重建（计数断言）。
- `-race` 下并发 `SetEffort` 与运行中的 turn 互不干扰。
- `go vet ./...`、`go build ./...` 干净。

### 6.1 已知残余

- 本 issue 交付的是 setter，**零生产调用方**（CLI / ACP / serve 的改接是明确的
  非目标）。这是设计意图，不是遗漏。
- `anthropic` 与 `responses` adapter 今天完全不读 `provider.Request.EffortOverride`
  （`grep` 零命中），所以本 issue 的效果目前只覆盖 `harness/provider/openai`。
  这正是 #331 的存在理由；本 issue 不阻塞于它，但两者应在同一里程碑，
  且**在 #331 落地前不得改接 CLI**——否则会把一个当前可用的 `/effort` 变成对那
  两族的静默 no-op。
- 本机无 C 编译器（`go test -race` 报 `-race requires cgo`），`-race` 由 CI
  ubuntu-latest 承担。
