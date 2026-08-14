# Issue #115 (M2-U21) 实现方案：init / config / version 命令

> 日期：2026-08-14 · 状态：方案（待评审） · 对应：Issue #115（`scope/kernel, priority/P1`）
> 依据：`docs/reports/cli-v2-architecture.md` §3/§4/§5（U19-U21）

## 1. 目标与范围

在 `cmd/semantix` 新增三个产品与管理命令，满足 Issue #115 四条验收标准：

1. `semantix init`：生成 `semantix.toml`（带注释）+ `.semantix/` 目录骨架；已存在时提示覆盖策略。
2. `semantix config`：打印生效配置，每项标注来源（flag/env/file/default）；支持 `--json`。
3. `semantix version`：输出版本 + commit + 构建时间；支持 `--json`。
4. 三命令 `--help` 与退出码符合 U19 契约（§4.3：0 成功 / 1 运行错误 / 2 用法错误 / 3 门禁未达标）。

## 2. 前置依赖与落地策略

Issue #115 标注依赖 U20；U20 建议与 U19 并行，U19 为契约先行。三者当前均为 OPEN、代码库均未落地。本方案将三者的**最小必需子集**纳入同一交付：

| 依赖 | 本方案落地的最小子集 | 不纳入本方案的部分 |
|---|---|---|
| U19 命令树/退出码/帮助契约 | `version/init/config` 挂载进 `main.go` 分派；`--help` 可用；退出码严格按 §4.3 | 其余命令树重构、`help` 四组全量列出、既有 8 命令退出码逐个核对（属 U19 本身） |
| U20 config 加载接线 | `kernel/config` 包：TOML 解析 + 优先级合并 + 来源追踪 + 校验（`config` 命令的消费端） | `extract/search/lookup/inject/verify/usage` 默认值改读 config（属 U20 本身，后续接线） |

> 约束：本方案**不改动** `lookup` / `inject` 的 H1 子进程协议行为（§2 设计原则 1），`version/init/config` 均为新增独立文件，与既有命令文件面零重叠。

## 3. 命令设计

> 三个命令均为非门禁命令，退出码不使用 `3`（`3` 仅 `verify`/`eval-judge`/`doctor` 等门禁命令使用，见 §4.3）。

### 3.1 `semantix version`

- 输入：无位置参数；flag `--json`（可选）。
- 数据来源：包级变量 `version` / `commit` / `buildTime`，经 `-ldflags -X` 注入（见 §6）；默认兜底 `version="dev"`、`commit="unknown"`、`buildTime=""`。
- 人类可读输出（stdout，三行）：
  ```
  version   0.3.1
  commit    abc1234
  build_time 2026-08-14T05:32:39Z
  ```
  （未注入时 `version` 显示 `dev`，`build_time` 为空则不输出该行。）
- `--json` 输出（§5 信封）：
  ```json
  {"ok":true,"command":"version","data":{"version":"0.3.1","commit":"abc1234","build_time":"2026-08-14T05:32:39Z"},"error":null,"version":"0.3.1"}
  ```
- 退出码：恒 0（无失败路径）；`--help` → 0；未知 flag → 2。

### 3.2 `semantix init`

- flag：
  - `--config <path>`：输出文件路径，默认 `./semantix.toml`（与全局 §4.1 一致）。
  - `--force`：已存在时覆盖。
  - `--json`：结构化输出（全局 flag §4.1，成功时 `data={created:[...],skipped:[...]}`）。
- 行为：
  1. 生成 `semantix.toml`（带注释模板，内容 = `semantix.example.toml` 全字段 + 注释，措辞改为「生成模板」）。
  2. 创建 `.semantix/` 目录骨架（与 `semantix.toml` 同级），写入 `.semantix/.gitkeep` 占位；目录已存在则幂等跳过，不报错。
- 覆盖策略：
  - `semantix.toml` 已存在且无 `--force`：stderr 提示 `semantix.toml already exists; re-run with --force to overwrite`，**退出码 1**（运行错误，非用法错误）。
  - `--force`：直接覆盖写入。
  - `.semantix/` 已存在：跳过（幂等）。
- 退出码：0 成功 / 1 写失败或已存在未覆盖 / 2 flag 用法错误；`--help` → 0。

### 3.3 `semantix config`

- flag：`--config <path>`（默认 `./semantix.toml`）、`--db <path>`（覆盖 `store.db`）、`--json`。
- 行为：加载生效配置（§4 优先级），**逐项打印 `key=value  # source=<flag|env|file|default>`**；`--json` 时输出信封，`data` 为 `{key:{value,source}}` 的扁平映射。
- 人类可读示例：
  ```
  project.name = "my-project"      # source=default
  store.db     = ".semantix/project.db"  # source=file
  store.scope  = "project"         # source=flag
  retrieval.limit = 5              # source=default
  ```
- 非法配置（TOML 语法错误 / 字段类型错 / 值域非法）：stderr 输出字段定位（行/列或 `key`），**退出码 2**（对齐 U20「非法配置 → 2 + 字段定位」）。
- 退出码：0 成功 / 1 文件 IO 错误（如 `--config` 路径不存在） / 2 用法错误（非法 flag）或非法配置内容；`--help` → 0。

> 来源标注规则：flag 覆盖 > env 覆盖 > file > default；某字段最终取到哪一层，`source` 就记哪一层。

## 4. `kernel/config` 加载层设计（U20 最小集）

### 4.1 数据结构

```go
package config

type Config struct {
    Project   Project   `toml:"project"`
    Store     Store     `toml:"store"`
    Retrieval Retrieval `toml:"retrieval"`
    Inject    Inject    `toml:"inject"`
    Verify    Verify    `toml:"verify"`
    Cost      Cost      `toml:"cost"`
}
// 各子结构字段与 semantix.example.toml 一一对应（name/db/scope/retriever/limit/...）。
```

- 所有字段用**指针/可选**语义承载，避免「未设置」与「零值」混淆，从而支持优先级合并与来源追踪。
- 每个字段同时记录 `Source`（`flag|env|file|default`），供 `config` 命令输出。

### 4.2 TOML 解析与选型

- 推荐 `github.com/BurntSushi/toml`：纯 Go、零传递依赖、`ParseError` 带行/列定位（满足 U20 字段定位要求），`#` 注释兼容现有 `semantix.example.toml`。
- 备选 `github.com/pelletier/go-toml/v2`（性能更好、维护活跃）。**本方案按 BurntSushi/toml 落地，若团队偏好 v2 可等价替换**（接口仅在 `Load` 一层）。
- `go.mod` 将新增该依赖（当前 `go.mod` 零依赖）。

### 4.3 优先级合并（§4.4）

```
CLI flag  >  env  >  semantix.toml  >  内置默认
```

- env 映射（前缀 `SEMANTIX_`）：`SEMANTIX_CONFIG`（配置文件路径）、`SEMANTIX_DB`（`store.db`）、`SEMANTIX_SCOPE`（`store.scope`）、`SEMANTIX_RETRIEVER`、`SEMANTIX_LIMIT`、`SEMANTIX_INJECT_BUDGET`、`SEMANTIX_INJECT_TOP_K`、`SEMANTIX_VERIFY_HOLDOUT`、`SEMANTIX_VERIFY_TARGET_HIT_RATE`。
- `--config` 自身也走同一优先级：显式 `--config <path>` > `SEMANTIX_CONFIG` env > 默认 `./semantix.toml`（不存在则跳过加载，各字段回落默认）。
- 内置默认与现有 `flags.go` 对齐：`store.db=.semantix/project.db`、`store.scope=project`、`retrieval.limit=5`、`inject.budget=4096`、`inject.top_k=5`、`verify.holdout=0.3` 等。

### 4.4 校验

- 类型错（数字字段给字符串）→ 由 TOML 解析报错，含位置。
- 值域校验：`store.scope ∈ {session,project,user}`、`retrieval.limit > 0`、`retrieval.retriever ∈ {bm25,vector,hybrid}`、`inject.budget > 0`、`verify.holdout ∈ [0,1)`。
- 全部校验错误聚合返回（非首错即停），错误信息带 `key` 定位。

## 5. JSON 信封（§4.2，本次三命令统一）

```go
type envelope struct {
    OK      bool            `json:"ok"`
    Command string          `json:"command"`
    Data    any             `json:"data"`
    Error   *envErr         `json:"error"`   // {code,message}，成功时 null
    Version string          `json:"version"`
}
```

- `version` 命令：`data={version,commit,build_time}`。
- `config` 命令：`data={key:{value,source}}`。
- `init` 命令：`--json` 时 `data={created:[...], skipped:[...]}`（记录生成/跳过的文件，便于脚本核对）；无 `--json` 保持人读摘要。
- 信封 `version` 字段 = 包级 `version` 变量（与 `version` 命令输出的版本同源，未注入时为 `dev`）。
- `error.code` 复用退出码数字语义（1/2）。

## 6. 文件变更清单

| 文件 | 变更 |
|---|---|
| `cmd/semantix/version.go`（新） | `var version/commit/buildTime`；`runVersion` |
| `cmd/semantix/init.go`（新） | `runInit`（模板生成 + 覆盖策略 + 目录骨架） |
| `cmd/semantix/config_cmd.go`（新） | `runConfig`（打印 + `--json`） |
| `cmd/semantix/main.go` | switch 增加 `version`/`init`/`config` 三支；`printUsage` 增补三行 |
| `cmd/semantix/envelope.go`（新） | `envelope` / `envErr` 信封类型（U22 复用） |
| `kernel/config/config.go`（新） | 结构体 + 优先级合并 + 来源追踪 + 校验 |
| `kernel/config/config_test.go`（新） | 优先级/来源/错误路径单测 |
| `cmd/semantix/version_test.go` `init_test.go` `config_cmd_test.go`（新） | 分派/退出码/JSON 单测 |
| `go.mod` / `go.sum` | 新增 BurntSushi/toml |
| `scripts/release/build.sh`、`build-full.sh`（可选增强） | 追加 `-X main.commit=... -X main.buildTime=...` 注入（保持 `main.version` 不变） |

> 现有 `build.sh`/`build-full.sh` 已注入 `-X main.version=${VERSION}`，但因代码无 `version` 变量，当前注入静默无效（Go 1.26 对不存在符号的 `-X` 不报错）。本方案新增变量后该注入即生效，向后兼容。

## 7. 边界与错误路径 + 测试计划

边界与错误路径（单测必须覆盖）：
- `version`：`buildTime` 为空时不输出该行；`--help` → 0；未知 flag → 2。
- `init`：`.semantix/` 已存在但为**文件**（非目录）→ `MkdirAll` 失败 → 1；`--config` 指向目录（无法写为文件）→ 写失败 → 1；父目录不存在 → 由 `MkdirAll` 创建。
- `config`：空配置文件 → 全 default 来源；`--config` 指向目录 → IO 错误 → 1；env 值为空串 → 视为未设置（回落下一层）；env 数字值非法（如 `SEMANTIX_LIMIT=abc`）→ 校验错误 → 2；`SEMANTIX_CONFIG` env 指向的配置文件生效。
- `kernel/config`：TOML 仅含部分 section → 缺省字段回落 default，不误判为 file 来源。

测试条目：
- `version`：无注入默认值（`dev/unknown`）；`--json` 信封可被 `json.Unmarshal`；未知 flag → 2；`--help` → 0。
- `init`：首次生成（校验文件存在 + `.semantix/.gitkeep`）；二次运行无 `--force` → 1 + 提示；`--force` → 覆盖；`--config` 指定路径；`.semantix/` 已存在幂等；`--json` 输出 `created/skipped`。
- `config`：默认全 default 来源；file 覆盖 + 来源标注；env 覆盖；flag 覆盖；`--json` 结构与来源正确；非法 TOML → 2 + 定位信息；`--config` 不存在 → 1；env 非法值 → 2。
- `kernel/config`：优先级合并四层逐一断言；值域校验错误路径；类型错误定位；部分 section 缺省回落 default。
- 回归：`go test -race ./...` 全绿；`lookup/inject` 既有单测不回归。

## 8. 验收对照表（Issue #115）

| # | 验收标准 | 方案落点 |
|---|---|---|
| 1 | init 生成 toml + 骨架、覆盖策略 | §3.2 |
| 2 | config 打印 + 来源 + `--json` | §3.3、§4 |
| 3 | version 版本/commit/构建时间 + `--json` | §3.1 |
| 4 | 三命令 `--help` 与退出码符合 U19 | §3 各节 + §5 信封 `error.code` 语义 |

## 9. 风险与待决

- TOML 库已默认选型 `github.com/BurntSushi/toml`（§4.2）；如需换 `pelletier/go-toml/v2` 仅改动 `Load` 一层，落地时确认一次即可。
- `config` 命令的 flag 面仅接 `--config/--db`，其余字段的 flag 覆盖留待 U20 完整接线时扩展；本方案保证来源追踪机制可扩展。
- `version` 的 `commit`/`buildTime` 依赖 build 脚本增强注入，未增强时输出 `unknown`/空，不阻塞验收。
