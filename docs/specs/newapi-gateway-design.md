# New API 网关集成设计 —— Semantix Gateway（v1）
> 日期：2026-08-13 · 状态：**已落地（v1，Issue #133）** · 对应：Semantix 对外形态扩展
> 落地差异（以 Issue #133 文件面为准）：
> - 组件为 `gateway/` 包 + `cmd/semantix serve` 命令（不再单独 `cmd/semantix-gateway/` 二进制）；
> - 网关后端在进程内直接复用 kernel 检索/注入路径（`kernel/cache.L3Decider` + `kernel/inject` +
>   `kernel/ingest`），与 `semantix lookup --json` 子进程协议同构（该协议即这些 kernel 包的 CLI 壳）；
> - v1 未接线 `judge.RuleGate`/`promote.CascadeInvalidate`（LLM 异步裁决与级联失效留待 M1）——
>   L3 验证走 L3Decider 的 zone 灰度区 + deps/mtime/指纹链（fail-closed）；
> - L3 TTL 实现为单一 `cache.ttl_seconds`（vendor 差异化 TTL 待上游文档确认后配置）。
> 一句话目标：以 **New API（OpenAI 兼容中转）**，在它后面挂一个 **Semantix Gateway**
> （新组件，复用 kernel 三层缓存），让任意 OpenAI 兼容客户端（Claude Code / chatbox / IDE 插件等）
> 的请求在到达上游 LLM 之前先过语义缓存——**L3 命中零上游调用、L2 注入跳过重复探索**，
> 达成省 token、省成本的效果。
> 与既有集成方式的关系：LangChain 中间件是「消息级」读/写记忆，Reasonix fork 是「事件级」，
> 本设计是**「请求级」的 HTTP 网关形态**——同一个「读记忆（inject/lookup） 写记忆（extract）」模型，
> 只是挂在 API 网关上，零侵入任何 agent harness，天然适配所有 OpenAI 兼容客户端。

---

## 1. 背景与缺口
### 1.1 Semantix 现状（v0.3.1）
- kernel 三层语义缓存已实现：**L1** 前缀字节稳定（依赖上游前缀缓存价）、**L2** 语义切片注入（`kernel/inject`）、**L3** 已验证结果复用（`kernel/fingerprint` + `kernel/judge` + `kernel/promote`，issue-08 已验收）；
- 成本演示（`docs/reports/m0-cost-comparison.md`）：跨会话复用**节省 79.8%**（目标 ≥ 50%），敏感性分析在最保守参数下仍 ≥ 50%；
- 对外形态目前是**纯 CLI + kernel 库**：`extract / search / lookup / inject / verify / eval / usage`，**没有 HTTP 服务、没有 OpenAI 兼容 API 层**。
### 1.2 New API 现状

New API（QuantumNous/new-api，one-api 加强分支）是 **OpenAI 兼容的 API 中转/分发面板**：统一入口 `/v1/*`、多用户 Token 管理、渠道管理（多上游 + 模型映射 + 负载均衡 + 重试）、按模型定价计费、额度与速率限制。它本身**不带 agent loop、不做语义缓存**——这正是 Semantix 要补的位置。
### 1.3 缺口与方案
> **缺口**：New API 的「渠道」必须是一个 HTTP 上游，而 Semantix 没有 HTTP 层，两者无法直接对接。
**方案**：新增 **Semantix Gateway**（独立 Go 进程，复用 kernel 全部包），对外暴露 OpenAI 兼容 API，New API 侧把它配置为一个*自定义渠道*。数据流：
```
客户端 (Claude Code / chatbox / 任意 OpenAI 兼容客户端)
   │  base_url = https://new-api.example.com
   ▼ New API（key 管理 / 计费 / 渠道分发）
   │  渠道类型 = 自定义（base_url = http://semantix-gateway:8080）
   ▼ Semantix Gateway（★ 本设计的核心新组件）
   ├─ L3 语义缓存命中 ──────► 直接返回缓存结果（零上游调用）
   ├─ L2 注入 + 转发 ────────► 上游 LLM（DeepSeek / Claude / Kimi / GPT）
   └─ 会话旁路提取 ──────────► 切片库（写记忆，供下次复用）
```

---

## 2. 省 token 机制与收益模型
### 2.1 三层机制在网关场景的映射

| 层 | 机制 | 网关中的落点 | 省钱方式 |
|---|---|---|---|
| **L1** | 前缀字节稳定 | 网关把注入块放在 **system 提示末尾、消息尾部之后**（Reasonix KV Cache 机制研究结论：静态在前、动态在后），且注入块 ID 规范序 → 字节稳定 | 上游按*缓存价*计费（DeepSeek miss $0.27/M vs hit $0.07/M，价差约 4 倍） |
| **L2** | 语义切片注入 | 未命中时 `inject.Injector.Build(query)` 检索 top-k 切片，拼入请求后转发上游 | 模型**跳过重复探索**，少生成工具调用/中间步骤 → 省*输出 token**（演示中 80% 的重复步骤被替代，是主要节省来源） |
| **L3** | 已验证结果复用 | 请求归一化 → 指纹校验（deps/mtime）→ `judge.RuleGate` 验证 → 命中直接返回缓存响应 | **零上游调用**，节省约 100% 的该请求成本 |

### 2.2 收益公式（沿用 m0-cost-comparison 模型）
```
单请求成本 = P_miss × (inject_bytes + 新内容) + P_hit × 稳定前缀 + P_out × output_tokens

baseline（无网关） = P_miss × 全量输入 + P_out × 全量输出
gateway 未命中     ≈ P_hit × 注入前缀 + P_miss × 增量 + P_out × (1 - reuse_ratio) × 全量输出
gateway L3 命中     ≈ 0（仅网关本地检索，<100ms）——计费上需 D1 的「合成 usage + 近零倍率」落地（§4.3）
```

- 合成演示实测：1800 → 360 completion tokens，成本 $0.001980 → $0.000399，**节省 79.8%**；
- 网关场景（API 网关、真实重复任务）预期区间：**30%–50%**，取决于重复率（垂类工作流重复度高，收益靠近上沿）；
- L3 命中是「纯赚」：一次缓存命中省掉该请求的全部上游费用，且延迟更低（<100ms vs 秒级）。
### 2.3 目标模型单价（2026-08 参考，网关设计按此建模）
| 模型 | 输入(miss) | 输入(缓存命中) | 输出 | 缓存机制 |
|---|---|---|---|---|
| DeepSeek-chat | $0.27/M | $0.07/M | $1.10/M | 自动前缀缓存（24h），无需显式标记 |
| Kimi (Moonshot) | ~$0.60/M | 前缀缓存价 | ~$2.20/M | OpenAI 兼容端点，自动前缀缓存（自动性待上游确认，§3.5） |
| GPT-4o 系列 | ~$2.50/M | ~$1.25/M | ~$10/M | 自动前缀缓存（provider 侧） |
| Claude Sonnet 系列 | ~$3.00/M | ~$0.30/M | ~$15/M | 需 `cache_control:{type:"ephemeral"}` 断点（≤2 个） |

> 注入块**字节稳定**是 L1 生效的前提；对 Claude 还需在注入块边界打 `cache_control` 断点（见 §3.8）。
> 单价随时可能变动，网关不硬编码价格——价格只在 New API 侧做计费用，网关只透传 usage。

---

## 3. Semantix Gateway 组件规格

### 3.1 形态与进程

- **独立 Go 进程**：`gateway/` 包 + `cmd/semantix serve` 命令，复用 `kernel/` 全部包（结构铁律不变：kernel 不得反向依赖网关）；
- 不侵入 New API（New API 无插件机制，渠道化对接是唯一合理方式）；
- 单二进制、零第三方 HTTP 依赖（标准库 `net/http` 即可，流式用 `http.Flusher`）；
- 切片库复用 `slice.NewFileStore`（JSONL 单文件 + 原子重写，0600/0700 权限约定延续；MVP 明确不用 bbolt，量大再评估切换）。
### 3.2 OpenAI 兼容 API 端点

| 端点 | 方法 | 说明 |
|---|---|---|
| `GET /v1/models` | 透传 | 返回可路由模型列表（网关上游已配置的模型名） |
| `POST /v1/chat/completions` | 核心 | 完整流水线（§3.3），支持 `stream=true`（SSE） |
| `POST /v1/completions` | 可选 | 文本补全透传（MVP 可先不做，默认 501） |
| `GET /healthz` | 健康 | 检查切片库可打开 + 上游可达性（New API 渠道健康检查用） |

请求/响应结构严格遵循 OpenAI 协议；错误响应也是 OpenAI 格式（`{"error": {"message", "type", "code"}}`），保证 New API 与客户端能正确识别。
### 3.3 请求处理流水线（核心）
```
POST /v1/chat/completions {model, messages, stream, ...}
   │
   ├─ 1. 鉴权：校验网关 Key（New API 转发时带的上游 key，见 §4.1）
   ├─ 2. 归一化：提取 (project/scope, 最后一条用户消息 → query, 完整 messages 指纹)
   │       项目 scope 来源：可配置 header（如 x-project）或 New API 渠道固定值
   ├─ 3. L3 查询：查缓存库
   │       · 命中（指纹 + deps/mtime 校验 + RuleGate 通过）：
   │       │  ├─ 非流式：直接返回缓存响应（含原始 usage）
   │       │  └─ 流式：按缓存响应重建 SSE 分块回放（§3.4）
   │       · 未命中 → 4
   ├─ 4. L2 注入：inject.Injector{Scope,K,Budget,Zones}.Build(query)
   │       有命中 → 注入块拼入请求（system 提示末尾，字节稳定）
   ├─ 5. 上游转发：模型映射（§3.8 适配层）→ 调用上游（超时/重试由 New API 与网关双层兜底）
   ├─ 6. 响应：透传 content + usage；L3 候选判定（§3.5）
   └─ 7. 异步写记忆：请求/响应旁路 → 会话 JSONL → ingest → extract（不阻塞主链路）
```

**关键原则**：
- **缓存永不阻塞主链路**：检索、注入、写库都是本地操作（<10ms 级）；上游失败/超时按 OpenAI 错误格式返回，客户端可重试；
- **L3 命中必须可观测**：响应头或 usage 中标记命中（如 `x-semantix-cache: hit` / `prompt_tokens_details.cached_tokens`），便于 New API 侧计费与用户看到省钱；
- **ablation 开关**：`SEMANTIX_GATEWAY_DISABLE=1` 一键退化为纯透传（风险预案）。
### 3.4 流式（SSE）
- **未命中流式**：网关向上游转发 `stream=true`，把上游 SSE 事件**逐块透传**；上游若未在末块返回 usage，网关在 `[DONE]` 前补一个含注入量 usage 统计的末块；保持 `[DONE]` 终止；
- **命中流式**：缓存存的是完整响应；网关按 OpenAI SSE 协议把缓存内容切成 `choices[0].delta.content` 分块回放（每块 ≤ 4KB 或按原缓存分块）。注意这是*重建流*：`id`/`created`、工具调用首块（`index`/`id` 必须在第一个 delta）、`finish_reason` 位置、token 边界都与上游原始流不同；M1 落地时用真实客户端回归验证（见 §8）；
- **字节稳定注意**：透传不重排、不重写上游事件，避免破坏客户端兼容性；注入块只在请求侧生效，不触碰响应流。
### 3.5 L3 语义缓存设计

```
缓存键 = hash(scope | 归一化 query | 模型名 | deps 指纹 | messages 上下文指纹)
```

- **messages 上下文指纹**：完整 messages（含 system/历史）的归一化哈希——防止不同会话历史/系统提示下*相同 query 互相复用**（跨上下文复用与信息泄漏）；MVP 用全量指纹，宽松模式（仅前缀参与）待 M1 后评估；
- **归一化 query**：最后一条用户消息去空白/规范化（复用 `sanitize` 纪律）；
- **deps 指纹**：复用 `fingerprint.Capture/Verify`（path → sha256）+ mtime 快速失败（`SliceMeta.Mtimes`）——*文件一变缓存即失效**（issue-08 已验收的机制）；网关条目的 deps root 由配置提供（如项目根目录），缺失文件一律视为已变更 → 失效；*网关生成中 deps 为空的结果默认不进入 L3**（`l3_safe=false`，需显式配置才启用）——否则指纹 RuleGate 验证形同虚设（空 deps 时 Chain 会跳过指纹阶段）；
- **验证**：`judge.RuleGate.Chain`（grey zone 规则，Krites §3.1）；grey 区候选可配置 `SEMANTIX_JUDGE_API_KEY` 走 LLM judge 确认；*judge 一律异步、离主链路执行**（不阻塞响应，保持 <100ms）；
- **提升与级联失效**：`promote.Store` 存提升条目 + 包级函数 `promote.CascadeInvalidate(store, sourceSliceID, currentContent)`——上游响应内容变化（content 版本号变更）时*级联失效**同一源切片衍生的下游缓存条目；
- **TTL**：缓存条目按模型 vendor 差异化（DeepSeek 24h / DashScope 5m / Anthropic 5m，沿用 `reasonix-kvcache-mechanisms.md` 的 vendor-aware 结论）；Kimi/Moonshot 与 GPT 的缓存 TTL **以上游文档确认后配置**（Moonshot 历史上需显式建缓存，勿假设自动生效）；
- **模型名进缓存键**：防止 Claude 的响应被 GPT 复用（跨模型语义相同但行为/风格不同，绝不混用）；
- **只缓存可安全复用结果**：带工具调用副作用的结果默认不入 L3（R-Slice 需 `--l3-safe` 或 deps 指纹非空，见 `SliceMeta.L3Safe`）。
### 3.6 L2 注入设计

- 检索：`inject.Injector`（K=5、Budget=4096、Zones 灰度分类默认开）；
- 注入位置：**system 提示末尾**（前缀尾部，保证注入块之后的历史消息字节稳定 → L1 生效）；对 Claude 在注入块边界打 `cache_control` 断点；
- 注入块形态沿用内核：`[semantix-reuse] ... [/semantix-reuse]`，*低权威定位**（内容仅供模型参考，不当作指令）；块内 ID 规范序（内核行为）保证字节稳定；
- 注入块不改变客户端可见的 model/messages 语义，仅内部改写后转发。
### 3.7 会话提取（写记忆）
- **旁路落盘**：每个请求/响应对（含工具调用如存在）追加到 `~/.semantix/sessions/<gateway-session-id>.jsonl`（0600），每行 `{role, content, tool_calls}`，与 `ingest.JSONLSource` 格式兼容；
- **提取**：异步执行 `ingest.Pipeline.Run` → 切片入库（P/C/R/T/M）；可复用现有 extract 逻辑（`cmd/semantix/extract.go`）；
- **scope 策略**：默认 `project`（New API 渠道级隔离），可选 `user`（按客户端 key 前缀映射，见 §4.4）；
- **写库失败不影响主链路**：提取是 best-effort，失败仅记日志 + usage 统计。
### 3.8 上游适配层（DeepSeek / Claude / Kimi / GPT）
网关统一对上游发 **OpenAI 兼容协议**，适配差异收敛到配置：

| 上游 | base_url（示例） | 鉴权头 | 需特殊处理 |
|---|---|---|---|
| DeepSeek | `https://api.deepseek.com/v1` | `Authorization: Bearer` | 无需 cache_control（自动前缀缓存），刻意不发 |
| Kimi | `https://api.moonshot.cn/v1` | 同上 | 同 OpenAI 兼容，自动前缀缓存 |
| GPT | `https://api.openai.com/v1` | 同上 | 同 OpenAI 兼容 |
| Claude | `https://api.anthropic.com/v1` | `x-api-key` + `anthropic-version` | ① 需要把 messages 转 Anthropic 格式（或走官方 OpenAI 兼容端点）；② 注入块边界打 `cache_control:{type:"ephemeral"}`（≤2 断点：system 尾 + 最后消息尾） |

**模型映射**：`semantix-gateway.toml` 中定义 `upstreams[].model_alias`（New API 侧模型名 → 上游模型名）；New API 渠道的模型名即网关的 model_alias，网关负责换成上游真实模型名。
**超时与重试**：网关给上游设保守超时（connect 10s / 首字节 60s / 总时长延续 New API 配置）；重试逻辑**主要在 New API 渠道层**（New API 有重试/负载均衡），网关只做一次重试兜底（幂等 GET 类；chat 请求重试仅对网络错误）。
### 3.9 配置（semantix-gateway.toml，v1 已实现）
```toml
# semantix-gateway.toml（semantix serve 读取；另有 SEMANTIX_GATEWAY_* 环境变量层）
# 注：配置加载器实现 ${VAR} 环境变量替换 + ~ 路径展开；
#     任何未解析的 ${...} 启动即报错（防把字面量当凭据）。
[server]
addr = "127.0.0.1:8080"              # 默认回环绑定；配 New API 内网时改为 :8080
gateway_key = "${SEMANTIX_GATEWAY_KEY}"   # New API 渠道转发时携带的 key（env 注入，不入库；空 = 关闭鉴权，仅限内网）
[store]
db = "~/.semantix/gateway.jsonl"          # 切片库 + L3 缓存库（JSONL 单文件，§3.1）
scope = "project"                         # 默认切片作用域
[retrieval]
top_k = 5
budget = 4096                             # L2 注入块字节预算
[cache]
ttl_seconds = 86400                       # L3 条目 TTL（秒；0 = 不过期）
l3_safe = false                           # 显式开启网关 L3 写回（需 deps_root，§3.5）
deps_root = "${SEMANTIX_GATEWAY_DEPS_ROOT}"  # 依赖指纹根目录（文件一变缓存即失效）
[ingest]
sessions_dir = "~/.semantix/sessions"     # 会话旁路落盘目录（空 = 关闭写记忆）
extract = true                            # 旁路会话异步提取为切片
[usage]
db = "~/.semantix/gateway-usage.jsonl"    # usage 记录（kernel/usage，与 `semantix usage` 对账）

[[upstreams]]                             # 每个 New API 渠道模型对应一段
name = "deepseek-chat"
base_url = "https://api.deepseek.com/v1"
api_key = "${DEEPSEEK_API_KEY}"
model_alias = ["deepseek-chat", "ds-chat"]   # 网关侧别名（New API 渠道模型名，§4.2 第一层）
upstream_model = "deepseek-chat"          # 上游真实模型名（§4.2 映射目标，第三层）
vendor = "deepseek"                       # deepseek | anthropic | openai | moonshot（预留）
timeout_seconds = 60                      # 单次上游调用超时
```

### 3.10 安全

- 网关 Key（`SEMANTIX_GATEWAY_KEY`）由 New API 渠道配置注入（渠道密钥字段），*客户端不接触**；网关不校验客户端 key——校验在 New API 层完成（New API 已做用户认证/配额）；
- 网关 key 支持**轮换与按渠道隔离**（每渠道独立 key 更佳）；明确边界：*New API 容器被攻破 = 网关凭据全部暴露**（内网 + 凭据不落盘缓解，不构成绝对隔离）；
- 上游 key 只存环境变量，*不落配置文件与日志**；
- 切片库 0600/0700 权限、原子写、防 symlink（沿用现有安全约定）；
- 敏感内容：默认 `scope=project` 隔离；脱敏接入点预留（`sanitize` 已有，正式版按策略启用）；
- 网关无公网暴露面：只允许 New API 所在网段访问（compose 内网即可，见 §5）。

---

## 4. New API 接入配置

### 4.1 渠道配置

New API 管理后台 → 渠道 → 新建渠道：
| 字段 | 值 |
|---|---|
| 类型 | **自定义**（Custom / OpenAI-API 兼容，按 New API 版本选择） |
| 名称 | semantix-gateway |
| 代理地址 (Base URL) | `http://semantix-gateway:8080`（compose 内网名）或公网地址（若网关单独部署） |
| 密钥 (Key) | `SEMANTIX_GATEWAY_KEY` 的值（New API 转发请求时作为 `Authorization: Bearer` 带给网关） |
| 模型 | 该渠道要路由的模型（如 `deepseek-chat`、`claude-sonnet-4`、`kimi-k2`、`gpt-4o`） |
| 模型映射 | 可选：New API 展示名 → 上游真实名（不映射则同名透传） |
| 启用 | 勾选 |

> 一个渠道 = 一个上游服务 + 一组模型。可**每个上游模型建一个渠道**（deepseek / claude / kimi / gpt 各一个渠道，均指向同一网关地址），便于 New API 侧按渠道做分组、权重与禁用。
### 4.2 模型映射示例

```
New API 侧模型名（客户端可见） → 网关 model_alias  → 上游真实模型
deepseek-chat                   → deepseek-chat      → deepseek-chat
claude-sonnet                   → claude-sonnet      → claude-sonnet-4-20250514（示例）
kimi-k2                         → kimi-k2            → moonshot-v1-128k（示例）
gpt-4o                          → gpt-4o             → gpt-4o
```

### 4.3 计费与配额
- New API 按渠道模型*定价倍率**计费（按上游单价配置模型价格）；
- 网关透传上游 usage，命中缓存时：
  - **L3 命中返回合成 usage**：`completion_tokens=0`、`prompt_tokens=缓存前缀量`并标记 `prompt_tokens_details.cached_tokens` 全部为缓存——否则缓存里的 completion tokens 仍会按全价计费，「≈0 成本」不成立；
  - New API 侧给网关渠道配*近零价格倍率**（如 0.001x）落 L3 命中的费用——*这是商业决策，见 §9 待决策项 D1**；
- 网关 `usage` 记录（`kernel/usage.Recorder/Summarize`）用于 Semantix 侧成本统计与验证报告，与 New API 侧计费对账用。
### 4.4 多用户 / 多项目隔离（可选）

- 方案 A（简单）：整个网关一个 scope=project，所有用户共享切片库（适合个人/小团队）；
- 方案 B（多项目）：New API 渠道按项目拆分（每项目一个渠道 + 固定 `x-project` header 由渠道配置注入），网关按 header 选 scope 库；
- 方案 C（用户级）：客户端 key 前缀映射到 scope=user 库。*注意：网关看不到客户端 key**（New API 只转发渠道 key，见 §4.1），需 New API 侧透传用户身份（如按用户拆渠道 + 固定 header，或 New API 支持的用户标识转发）才可行。MVP 用 A，需要时再 B。

---

## 5. 部署方案（推荐：单机 Docker Compose）
```
docker-compose.yml
├─ new-api          镜像: quantumnew/new-api  端口 3000 → 公网（或经 nginx）
│    volumes: ./data/new-api:/data            （SQLite 持久化）
├─ semantix-gateway 镜像: <semantix 构建产物镜像（见下）>  端口 8080（仅内网）
│    env: SEMANTIX_GATEWAY_KEY / DEEPSEEK_API_KEY / ANTHROPIC_API_KEY / MOONSHOT_API_KEY / OPENAI_API_KEY / SEMANTIX_JUDGE_API_KEY
│    volumes: ./data/semantix:/root/.semantix  （切片库 + 会话旁路持久化）
└─ (可选) nginx：TLS 终结 + /v1/* 反代到 new-api:3000
```

要点：
- **new-api 用 SQLite**（单机够用，零外部依赖）；规模上来再迁 MySQL；
- 网关镜像：`go build ./cmd/semantix` → scratch/distroless 单二进制镜像；网关自身不引入新的
  第三方依赖（沿用仓库已有的 toml 解析库），文件存储为 JSONL，镜像 <10MB；
- 网络：`semantix-gateway:8080` **只暴露给 new-api 容器**（compose 内部 network），不发布到宿主机；
- 健康检查：New API 渠道可用性检测可配置为 `GET /healthz`（或渠道自检开关）；
- 日志：stdout JSON 日志 + usage 汇总（`semantix usage` 可对账）；建议接 Loki/Promtail 或仅文件轮转。
### 5.1 非 Docker 备选
- 二进制直跑：`semantix serve` 与 `new-api` 同机/异机，`base_url` 填实际地址；用 systemd/launchd 托管 + 环境变量注入 key。

---

## 6. 数据流时序
### 6.1 L3 命中（零上游调用）
```
客户端 ──POST /v1/chat/completions──► New API（认证/计费）──► 网关
   │                                    │                        │
   │  200 {content, usage}  ◄───────────┤ 指纹+RuleGate 校验通过      │
   │  headers: x-semantix-cache: hit     │ 直接返回缓存响应        │
   ◄──────────── 响应 <100ms，无上游费用 ────────────────────────┤
```

### 6.2 未命中（L2 注入 + 上游）
```
客户端 ──► New API ──► 网关
                        ├─ inject.Build(query) → 注入块（字节稳定）
                        ├─ 转发上游（OpenAI 兼容协议 / Claude 转格式 + cache_control）
                        ├─ 上游 200 / SSE 流
                        ├─ 透传响应 + usage（cached_tokens 标记注入前缀）
                        └─ 异步：旁路写会话 JSONL → ingest 提取 → L3 候选入库
客户端 ◄── 响应 ◄──────────────────────────────────────────────
```

---

## 7. 实施路线与验收标准
| 阶段 | 内容 | 工期 | Gate（验收） |
|---|---|---|---|
| **M0 网关 MVP** | `gateway/` 包（`semantix serve`）：OpenAI 兼容层（chat/completions + SSE 透传 + /healthz）、鉴权 + 上游 DeepSeek 适配 + 会话旁路入库 | 1–2 周 | 任意 OpenAI 兼容客户端 → New API → 网关 → DeepSeek 全链路跑通；会话入库后 `semantix search` 可检索到切片 |
| **M1 缓存闭环** | L2 注入 + L3 缓存（指纹/RuleGate/promote/TTL）、流式命中回放 + usage 标记 | +1–2 周 | 重复任务第二次命中 `x-semantix-cache: hit` 且零上游调用；合成演示成本节省 ≥ 50%（复用 m0-cost-comparison 脚本） |
| **M2 多模型 + 上线** | Claude（格式转换 + cache_control）、Kimi / GPT 适配 + 模型映射表 + 计费对账 + 健康监控 | +1 周 | 四家模型渠道全通；命中率/节省率有周报数字；New API 侧对账一致 |

**M0 为决策门**：若网关形态下「请求级注入」收益显著低于预期（重复率低的场景），及时转向纯 L3 缓存策略或按会话聚合注入，控制投入在 2 周内。

---

## 8. 风险与边界
| 风险 | 缓解 |
|---|---|
| L3 误命中（文件变了还复用旧结果） | deps 指纹 + mtime 快速失效 + RuleGate grey zone；副作用结果默认不入 L3（`l3_safe=false`）；**网关生成中 deps 为空的条目默认不入 L3**（§3.5） |
| 注入块改变模型行为（低质量参考干扰） | 注入块低权威定位 + ablation 开关一键关闭 + 灰度分类（Zones）只注入明确可复用的切片 |
| 流式命中回放兼容性 | MVP 先做非流式 L3 命中 + 流式透传；流式命中回放为 M1 项，用真实客户端回归 |
| 敏感数据进切片库 | scope 隔离（project/user）、0600 权限 + 脱敏接入点预留；`scope=user` 方案 C 为可选增强 |
| 缓存键跨模型混用 | 模型名进缓存键（§3.5），绝不跨模型复用 |
| L3 跨上下文复用/泄漏（同 query 不同历史） | messages 上下文指纹进缓存键（§3.5）；共享 scope 仅限可信用户（§4.4） |
| L3 缓存无界增长 / JSONL 全量重写放大 | 条目上限 + TTL 惰性清理 + 异步批量写（D4，§9） |
| 共享 scope 切片注入投毒（对抗指令） | 方案 A 仅限可信用户；多租户用方案 B/C 隔离；judge 内容消毒 |
| L3 命中计费不落地（completion 仍全价） | §4.3 合成 usage + New API 近零倍率（D1，§9） |
| 网关成为单点/性能瓶颈 | 纯本地操作 <10ms；`/healthz` 探活；New API 侧可配多网关渠道做负载均衡（同一切片库需共享 volume） |
| 上游 key 泄露 | 仅环境变量 + 内网暴露 + 日志脱敏 |
| 计费口径不一致（网关 vs New API） | usage 统一以网关透传/回填为准，`kernel/usage` 周对账 |

---

## 9. 待决策项（D1–D4）
| 编号 | 决策 | 选项 | 建议 |
|---|---|---|---|
| **D1** | L3 缓存命中是否对客户端降价 | ① New API 模型价格倍率下调（如命中 0.25x）② 不降价，省钱体现为服务方利润 ③ 仅统计展示不调价 | 个人使用建议 ①（钱省在自己账户）；商业化看②③——*上线前定** |
| **D2** | 网关是否支持 `/v1/embeddings` 透传 | 支持 / 不支持 | MVP 不支持；有向量检索 embedding 需求再加（透传很简单） |
| **D3** | 多用户 scope 方案 | A 单库 / B 多项目渠道 / C 用户级库 | MVP 用 A，B 按需 |
| **D4** | 网关缓存库与切片库分库还是合库 | 分库 / 合库（同一 JSONL Store） | 合库（同一 Store，L3 缓存作为特殊 Slice 类型）；条目上限 + TTL 惰性清理，防无界增长 |

---

## 10. 结论

Semantix Gateway 是 Semantix 从「agent kernel / CLI」走向*「API 网关形态」**的关键一步：
把已验证的 79.8% 成本节省机制（L2 注入 + L3 复用 + L1 字节稳定）搬到 New API 后面，*零侵入任何客户端或 harness**；一条命令部署（`go build ./cmd/semantix`），天然服务 DeepSeek / Claude / Kimi / GPT 四家模型。M0 决策门控制投入在 2 周内，收益不成立可随时退化为纯透传。
