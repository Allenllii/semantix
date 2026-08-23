# Spec v1 — 净化管线接到写入/注入路径（Issue #278）

> 判级：Spec-Required（安全边界 + 注入字节形态变化）。本 spec 落地
> Issue #278「净化管线接到写入/注入路径」：把 Security-安全设计.md §3.1
> 已设计的版本化规则净化引擎接到**入库（写入）**与**注入（组装）**两条
> 路径上，消除「ingest 零过滤、inject 仅 marker 转义、judge.Sanitize 只在
> 读侧」的攻击通道。基线为当前 main（bb0cc50）。

## 1. 目标与非目标

### 1.1 现状缺口（真源审计，2026-08-22）

| 缺口 | 证据 |
|---|---|
| ingest 写入侧零安全过滤 | `cmd/semantix/extract.go:124` 与 `kernel/ingest/ingest.go:253` 都是 `Extractor.Extract` → `Store.Put` 直通；提取管线只有空内容/长度/畸形 JSON 过滤（`kernel/slice/extractor.go:93-155`），无注入检测、无脱敏 |
| 净化只在 judge 读侧 | `judge.Sanitize`（`kernel/judge/sanitize.go`，仅 ANSI/OSC/DCS 转义剥离）调用点只有 `kernel/judge/llm.go:83` 与 `cmd/semantix/sanitize.go:14`（终端输出） |
| inject 仅 marker 转义 | `kernel/inject/inject.go:33-38` `escapeMarker` 只防 `[semantix-reuse]` 块逃逸（Spotlighting 分类中最弱的分隔防御）；`Content` 原样进注入块 |
| Security §3.1 设计未实现 | 设计要求「注入前剥离指令前缀/角色扮演/命令替换特征 + 密钥/邮箱/内网路径脱敏；纯函数、版本化规则引擎、净化结果进注入集指纹」——零代码 |

### 1.2 本期范围

```text
kernel/sanitize 新包（版本化规则引擎，纯函数、无 LLM、零第三方依赖）
  ├─ EscapeSequences(s): 迁移 judge.Sanitize 的转义剥离（终端路径保留）
  ├─ Sanitize(s): EscapeSequences + 指令特征剥离 + 敏感模式脱敏
  └─ Version: 规则版本常量（写入 SliceMeta.SanitizeVersion）
写入侧: kernel/slice extractor 统一净化点（newSlice）→ CLI extract /
        ingest.Pipeline / gateway 全链路自动覆盖
注入侧: kernel/inject Build 组装时二次净化（幂等，防御存量未净化切片）
读侧:   judge/llm.go 升级为完整 Sanitize（指令+脱敏，防 judge 劫持）
对抗测试集: 绕过变体 + 全链路载荷剥离验收
```

### 1.3 非目标（列为后续）

- **datamarking（Spotlighting 强形态）**：注入块内每行来源标记（`▏`）——
  评审结论：**v1 不做**。理由：① 改变全部注入块形态 → L1 前缀缓存全面
  失效（DeepSeek 24h 字节前缀），代价远超收益；② Security §3.1 设计未
  要求（只要求剥离+脱敏）；③ marker 转义已提供结构性封闭，剥离规则
  提供内容级防护，双层已覆盖 Spotlighting 的分隔+数据标记弱形态。
  后续若做，必须配注入集冻结期切换（架构 §6.2）。
- **规则配置化**：v1 内置固定规则表（确定性最强保证），调优数据出现后
  再议（同 #277 §7 取舍）。
- **跨项目隔离增强**（Security §3.1.3）：用户级切片跨项目注入剔除项目
  专属路径——依赖 scope 数据，单列。
- **污染信号闭环**（Security §3.1.4，用户编辑/回滚 → rejected++）：依赖
  harness 反馈通道，单列。
- **LLM 净化判定**：永远不做（破坏注入集指纹，Security §2.3 明确禁止）。

## 2. 规则引擎设计（单一真源）

### 2.1 包结构与 API（kernel/sanitize）

```go
package sanitize

// Version 标识当前规则集版本，随 SliceMeta.SanitizeVersion 持久化。
// 规则变更（增删特征/脱敏模式）必须 bump 版本号——旧版本库的存量
// 切片由注入侧二次净化兜底，新入库切片记录新版本。
const Version = "v1"

// EscapeSequences 剥离 ANSI/OSC/DCS/PM/APC/SOS 转义序列（迁移自
// judge.Sanitize，Issue #8 验收④ 的唯一转义扫描器）。终端输出路径用。
func EscapeSequences(s string) string

// Sanitize 是完整净化管线：EscapeSequences → 指令特征剥离 → 敏感脱敏。
// 纯函数、确定性：同输入必同输出（Security §2.3）。幂等：
// Sanitize(Sanitize(s)) == Sanitize(s)（注入侧二次净化不改变已净化内容）。
func Sanitize(s string) string
```

`kernel/judge.Sanitize` 删除，调用点迁移：`cmd/semantix/sanitize.go`
（stripESC，终端）→ `sanitize.EscapeSequences`；`kernel/judge/llm.go:83`
→ `sanitize.Sanitize`（读侧升级为完整管线）。转义扫描器测试
（sanitize_test.go / sanitize_c1_test.go）随迁至 kernel/sanitize，零改动
保持绿。

### 2.2 指令特征剥离规则（v1 内置表）

大小写不敏感、整词边界匹配的**特征短语剥离**（删除命中短语本身；
不删整句——整句剥离误伤高且难确定性）。来源：MINJA / Memory Poisoning
综述的常见载荷形态 + Security §3.1 列举。

| 类别 | 特征短语（英文） | 特征短语（中文） |
|---|---|---|
| 指令覆盖 | `ignore previous instructions`、`ignore all previous instructions`、`disregard previous instructions`、`forget all previous instructions`、`override all instructions` | `忽略之前的指令`、`忽略以上所有内容`、`无视之前的指令`、`忘记之前的指令` |
| 系统提示冒充 | `you are now`、`from now on you are`、`your new system prompt`、`your new instructions`、`act as a system` | `你现在是`、`从现在起你是`、`你的新系统提示`、`你的新指令` |
| 角色扮演 | `pretend you are`、`act as`（后接角色时的通用前缀，v1 仅剥离短语本身） | `扮演`、`假装你是` |
| 命令替换 | `run the following command`、`execute this command`、`run this command`、`execute the following` | `执行以下命令`、`运行以下命令` |

- 匹配实现：rune 级 fold（复用 inject.replaceFold 的模式）+ 两侧空白
  规整（剥离后折叠连续空白，避免 `ignore previous instructions  ，` 残留
  悬挂标点）；剥离循环至**固定点**（折叠可能暴露新短语，如
  `ignore all  previous instructions` 双空格折叠后成完整载荷——固定点
  保证 `Sanitize(Sanitize(s)) == Sanitize(s)`，注入侧两遍组装依赖此
  不变量）。
- 剥离是**内容变换**：命中即删，不标记、不替换为占位符（标记属
  datamarking，非目标；占位符会让"讨论注入攻击"的正常会话内容失真，
  剥离已满足 Security §3.1「剥离指令前缀」字面要求）。
- 中文特征在 extract 的 Prompt/Result 切片上实测命中率极低（正常会话
  不会写「忽略之前的指令」）——误伤面可控。**已知误伤示例（v1 权衡，
  非 bug）**：否定/引用句中的短语会被剥离——「不要忽略之前的指令」→
  「不要」、「Please don't pretend you are a bot」→「Please don't a bot」。
  安全优先于保真，规则表只收高置信短语以最小化此面。

### 2.3 敏感模式脱敏规则（v1 内置表）

正则匹配 → 固定占位符（确定性）：

| 模式 | 示例 | 占位符 |
|---|---|---|
| 平台密钥 | `sk-[A-Za-z0-9]{20,}`、`AKIA[0-9A-Z]{16}`、`ghp_[A-Za-z0-9]{36}`、`xox[baprs]-[A-Za-z0-9-]{10,}` | `[REDACTED_KEY]` |
| 邮箱 | 标准 email 正则（rune 安全，不跨行） | `[REDACTED_EMAIL]` |
| 主目录路径 | `(/home\|/Users)/[A-Za-z0-9_.-]+`、Windows `C:\Users\[A-Za-z0-9_.-]+` | `[REDACTED_PATH]` |

- 只脱敏**高置信**模式（误伤风险最低的形态）：密钥前缀特征明确、
  邮箱结构完整、主目录路径带 `/home|/Users|C:\Users` 锚。普通项目路径
  （`/workspace`、`/opt`）不脱敏——v1 不做通用路径脱敏（误伤面太大）。
- 脱敏在剥离**之后**执行（剥离可能暴露新的敏感串——如指令载荷内嵌
  密钥）。

### 2.4 幂等性与字节稳定

- `Sanitize` 纯函数 + 幂等：注入侧对已净化切片再净化零变化 → 正常
  会话（无特征无敏感模式）的注入块与 main 逐字节一致，L1 前缀缓存
  不受影响；含载荷内容首次写入即净化 → 注入块含的是净化后文本。
- 规则升级：bump `Version`；新版本只影响**新写入**切片（写时净化），
  存量切片由注入侧二次净化兜底，无需全库重写（无重写路径，天然
  避免重写风暴）。

## 3. 写入侧接线（ingest 净化）

`kernel/slice/extractor.go` `newSlice`（所有提取切片的统一构造点）：

```go
func newSlice(t SliceType, sc Scope, content []byte, meta SliceMeta) *Slice {
    content = []byte(sanitize.Sanitize(string(content))) // Issue #278
    meta.SanitizeVersion = sanitize.Version
    return &Slice{ ID: sliceID(content, t, sc), ... }
}
```

- 覆盖全部写入路径：CLI `extract`、`kernel/ingest.Pipeline`（gateway 用）、
  未来任何经 Extractor 的写入——单点接线，无遗漏面。
- `sliceID` 基于**净化后** content（与 CompressionVersion 先例一致：
  内容变换 → ID 变化），同内容不同规则版本不碰撞。**dedup 语义注记**：
  净化升级（bump Version）后，同一原始内容的新切片 ID 不同于旧切片，
  新旧可短暂共存（各自 ID 指纹自己的净化形态）；注入侧幂等回退保证
  旧切片出块时同样无载荷——不构成安全缺口，仅库内多一份同源内容，
  由 gc 价值淘汰收敛。
- `SliceMeta.SanitizeVersion string`（`json:"sanitize_version,omitempty"`），
  仿照 `CompressionVersion`；legacy/未净化切片为空串（注入侧兜底）。
- **ToolPattern 注记**：净化对工具名序列同样生效（统一安全一致性）；
  当前工具名无 sk-/email 形态故为 no-op，未来若工具名含敏感形态会
  改变其 ID——v1 接受（安全一致性优先）。

## 4. 注入侧接线（inject 净化）

`kernel/inject/inject.go` `Build` 组装块时：

```go
content := sanitize.Sanitize(string(h.Slice.Content)) // 幂等，存量兜底
content = escapeMarker(content)                        // 原有块逃逸防护
```

- 顺序：先净化后转义（转义针对块标记，与净化正交；净化先保证进入
  块的内容已无载荷特征）。
- 预算（budget）计算改用净化后内容长度（`buf.Len()` 分支）。
- 净化后内容进注入集指纹（指纹基于最终注入块字节——自动包含）。
- `kernel/cache.L3Decider` 的 L3 复用路径**不做**净化（读侧 judge 已
  净化；L3 直接复用原文是「结果复用」语义，内容变换会破坏结果等价性）
  ——L3 的结果正确性由 judge + 指纹门保证，不在本 spec 范围（spec 只在
  L2 注入与 ingest 写入接净化）。

## 5. 读侧升级（judge）

`kernel/judge/llm.go:83`：`c.Content = Sanitize(c.Content)` → 改为
`sanitize.Sanitize(c.Content)`（转义 + 指令剥离 + 脱敏）。judge 是 L3
验证路径的信任边界，完整管线防「转义剥离后仍含指令特征」的载荷劫持
judge（Security §3.2.1「复用 §3.1 的净化管线」）。

## 6. 配置与可观测

- **无新配置键**：净化无条件执行（安全边界默认开，Security §2 分级
  fail 策略：安全边界失败 fail-closed；净化是纯函数无失败路径）。
- 可观测：`SliceMeta.SanitizeVersion` 随切片持久化（存量空串可被
  `semantix search --json` 等读侧查询看到）；不新增事件/统计（安全
  边界最小面）。

## 7. 对抗测试集（Security §3.1.2 要求）

`kernel/sanitize` 包内 + 全链路：

- **规则单测**：每条特征短语剥离（英/中、大小写变体、前后空白）；
  敏感模式脱敏（密钥/邮箱/主目录，含边界形态：短密钥不脱敏、多字节
  UTF-8 邮箱不破坏）；幂等断言；确定性断言（同输入两次同输出）。
- **绕过变体**（对抗）：全大写/混合大小写、Unicode 相似字符（如
  `iɡnore` 用 U+0269）、零宽字符插入、转义序列包裹载荷
  （`\x1b[31mignore previous instructions\x1b[0m`）、嵌套特征、载荷
  分片跨行——v1 规则命中表内变体，未命中变体记录为已知缺口（断言
  当前行为，标注「已知绕过」不假装完备，Security §2.3「确定性 ≠
  完备性」）。
- **全链路**：含注入载荷的会话 JSONL → `Extract` → `inject.Build` →
  断言载荷特征不在注入块中、敏感模式已脱敏、注入块确定性（两次
  构建字节相同）。
- **回归锚**：普通会话（无特征无敏感模式）→ extract/inject 输出与
  main 逐字节一致（单测 fixture 对比）。

## 8. 测试计划与验收标准

- [x] kernel/sanitize：转义迁移测试全绿（原 judge 测试迁入零改动）；
      指令剥离/脱敏/幂等/确定性/绕过变体测试；Version 常量。
- [x] kernel/slice：newSlice 净化接线测试（含载荷内容 → 净化后入库、
      SanitizeVersion 记录、ID 基于净化后内容）；既有 extract 测试
      全绿（无敏感 fixture 不应受影响）。
- [x] kernel/inject：净化+转义顺序、预算用净化后长度、幂等（二次
      注入字节不变）；既有 inject 测试全绿。
- [x] kernel/judge：llm.go 读侧完整管线（llm_test 断言更新）。
- [x] 全链路：载荷会话 → extract → inject 剥离验收；普通会话字节
      回归锚。
- [x] 回归：`go test ./...`（除既有 pre-existing 环境失败：Windows
      symlink 特权 / reasonix 在 PATH 导致 run(nil) 走 launchAgent）
      全绿。
- [x] 文档：Security §3.1 标注「已落地（Issue #278）」、README 状态行、
      docs/events.md 无事件变更（零新增 wire）。

## 9. 参考

- MINJA（arXiv:2503.03704）：正常查询写入恶意记忆；
- Memory Poisoning 系统研究（arXiv:2606.04329）：写入通道与结构性弱点；
- Spotlighting（arXiv:2403.14720）：分隔/数据标记/编码（本 spec 采用
  其分隔+数据弱形态，datamarking 留后续）；
- Adaptive Attacks（arXiv:2503.00061）：单层防御不足——本 spec 的
  写入+注入+读侧三层接线即是对「写入侧先挡一道」的落地；
- Security-安全设计.md §2.3 / §3.1 / §3.2 / §10。
