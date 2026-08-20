# Spec Post — GW4：网关 §7 M0+M1 验收门实录（Issue #184，待审）

> **判级**：Spec-Exempt（验收执行——不写新功能代码；过程中发现的 bug 按需另开 issue）。
> **真源**：`docs/specs/newapi-gateway-design.md` §0.3（M0/M1 门现状两行）、§3.4（流式透传/命中回放）、
> §3.5（L3 设计：l3_safe / ContextHash / judge fail-closed）、§3.7（会话旁路写记忆）、§4.3（计费口径）、
> §7（M0/M1 Gate 定义）、§9 D1（计费倍率待决策）；依赖验收报告：`issue-182-acceptance.md`（GW2，流式响应侧写记忆）、
> `issue-183-acceptance.md`（GW3，部署产物）、`issue-187-acceptance.md`（GW7，bytes/4 计量口径）。
> **状态**：⏳ 待审。审后开工；产出 `docs/reports/gateway-m1-acceptance.md`。

---

## 1. 目标与范围

**核心目标**：把 §7 两个验收门从「只有假上游 e2e 证据」补齐为**真实环境实录**：
`真实客户端 → New API → 网关 → DeepSeek` 全链路（M0）＋ 重复任务二次命中零上游调用、
成本节省 ≥30% 实测（M1），全部证据落在 `docs/reports/gateway-m1-acceptance.md`，
并回写 spec §0 状态行、关闭 issue #184。

**范围内**：

| # | 项 | 说明 |
|---|---|---|
| 1 | 环境就绪 | 启动本机 Docker Desktop；`.env` 注入 `SEMANTIX_GATEWAY_KEY` / `DEEPSEEK_API_KEY`（用户提供）；`docker compose -f deploy/docker-compose.yml up -d --build`；New API 面板初始化（默认账号、令牌、渠道、模型定价） |
| 2 | M0 门 | chatbox（首选真实客户端）建会话 → `http://127.0.0.1:3000`（New API）→ 网关 → DeepSeek 全链路跑通；会话入库后 `semantix search` 检索到切片（真上游证据） |
| 3 | M1 门 | ≥10 组重复任务样本，每组同 body 首轮 miss + 二轮 `x-semantix-cache: hit` 且上游零调用（网关 usage_log + New API 消费记录双证） |
| 4 | 成本实测 | 按 New API 计费口径算节省率 ≥30%；注明 `bytes/4` 估算 vs 真 token 差异 |
| 5 | D1 决策 | 与 Song 确认 L3 命中计费倍率，写入报告（未确认则如实标注待确认，不预支倍率收益） |
| 6 | 产出物 | `docs/reports/gateway-m1-acceptance.md`（全证据 + 复现步骤）；spec §0 状态行回写；issue #184 关闭 |

**不在范围内（明确不碰）**：
- 不写任何功能代码（包括不修「短 query L3 命中率低」——这是 kernel zone 既有 fail-closed 设计，
  若实测命中率因此不达标，如实记录为已知边界并给出后续建议，另开 issue）
- 不落地 D1 的 New API 定价改动本身（确认后可在报告中给出落地说明，改配置由人工执行）
- M2（Claude 多模型 / 计费周报对账）不涉及
- 不引入真实用户自然流量等待窗口——样本为自造重复任务，不等流量

---

## 2. 现状事实（代码/环境证据）

### 2.1 Issue 与依赖状态

| 事实 | 证据 |
|---|---|
| #184 OPEN、0 评论、0 PR；assignees jh10724-dotcom + Allenllii | `gh issue view 184` |
| 依赖全部合入：GW2 #182（PR #203）、GW3 #183（PR #207）、GW7 #187（PR #211） | `gh pr list`（2026-08-17 全部 MERGED） |
| spec §0.3 两行：M0 门「合取门只满足后半」；M1 门「真实环境与成本节省实测无记录」 | `newapi-gateway-design.md:98-99` |
| §0.4：「79.8% 来自合成演示，不是网关链路实测」 | `newapi-gateway-design.md:106-108` |

### 2.2 本机环境

| 事实 | 证据 |
|---|---|
| Docker Desktop 已安装但 **daemon 未运行**（`desktop-linux` context，npipe 连不上） | `docker info` 失败 |
| 无 `SEMANTIX_GATEWAY_KEY` / `DEEPSEEK_API_KEY` 等环境变量 | `Get-ChildItem Env:` 过滤零匹配 |
| `semantix` CLI 已装：`C:\Users\liwen\go\bin\semantix.exe`；`~/.semantix` 存在（sessions/、opencode.db），**无网关 store** | `where semantix`、目录列表 |
| 工作区处于 **mid-merge**（`fix/pr222-conflict`，冲突已解决未提交） | `git status`「All conflicts fixed but you are still merging」 |

### 2.3 网关代码可观测性（验收要用的证据点）

| 事实 | 证据 |
|---|---|
| 命中/未命中响应头 `x-semantix-cache: hit` / `miss` | `gateway/pipeline.go:247,280,470`、`gateway/anthropic.go:749` |
| L3 命中时写 usage 事件：`l3_reuse: true`、`TokensOut: 0`、`CacheHitToken`；miss 也每请求一行 | `gateway/pipeline.go:54,251,336` + `kernel/usage/usage.go:28-36`（`Event{SessionID, TokensIn, TokensOut, CacheHitToken, L3Reuse}`） |
| usage 落盘：`[ingest] usage_log = "/data/gateway-usage.jsonl"` | `deploy/semantix-gateway.toml.example:30` |
| 合成 usage 带 `"estimator":"bytes/4"`（len/4 估算非真 tokenizer） | §0.3 末行 + GW7 验收报告 |
| **`[ingest] l3_safe_default = false`（example 配置）**——无 deps 的网关结果**默认不进 L3** | `deploy/semantix-gateway.toml.example:31` + §3.5 |
| store 路径 `/data/gateway.jsonl`；镜像内**只有网关二进制，无 semantix CLI** | `deploy/semantix-gateway.toml.example:15` + `deploy/Dockerfile` |
| compose：New API `3000:3000`，网关 `8080` 仅内网，双 healthcheck | `deploy/docker-compose.yml` |
| 短 query（≤2 词）BM25 绝对分 < `AbsHigh(0.7)` → 整检索 Grey → 未配 judge → 保守 Reject（L3 fail-closed） | `issue-182-acceptance.md` §6 + `semantix search -abs-high` 默认 0.7 |

---

## 3. 执行设计

### 3.1 阶段 0：环境就绪（阻塞项，需用户配合）

1. 启动 Docker Desktop，等待 daemon 就绪（`docker info` 通过）；
2. 用户提供 `DEEPSEEK_API_KEY`；生成 `SEMANTIX_GATEWAY_KEY`（随机串）；
   写 `deploy/.env`（compose 自动读取，`${VAR:?}` 门禁）：
   ```dotenv
   SEMANTIX_GATEWAY_KEY=<random>
   DEEPSEEK_API_KEY=<user-provided>
   ```
3. **验收专用配置**：`cp deploy/semantix-gateway.toml.example deploy/semantix-gateway.toml`，
   并显式改一行（风险登记见 §6）：
   ```toml
   [ingest]
   l3_safe_default = true   # 验收门专用：否则网关结果永不进 L3，M1 无法命中（example 默认 false）
   ```
4. `docker compose -f deploy/docker-compose.yml up -d --build`；`docker compose ps` 双服务 healthy；
5. New API 面板（`http://127.0.0.1:3000`，默认账号初始化）：建令牌（供客户端用）；
   建渠道：类型「自定义」、代理地址 `http://semantix-gateway:8080`、密钥 = `SEMANTIX_GATEWAY_KEY`、
   模型 `deepseek-chat`、渠道可用性检测 `GET /healthz`；
6. New API「模型」里为 `deepseek-chat` 配置定价（按 DeepSeek 官方价输入/输出 per-M 单价）——
   无定价则消费记录无金额，成本实测无从算起。

### 3.2 M0 门：真实全链路 + 会话入库可检索

1. chatbox（已装则直接配置；未装装 Windows 版）→ Settings 填：
   `API Host = http://127.0.0.1:3000`、`API Key = <New API 令牌>`、`Model = deepseek-chat`；
2. 发起 ≥1 条**多轮真实任务对话**（任务型、query 含完整句子，≥3 词；如「帮我写一个
   PowerShell 脚本，把目录下所有 .md 文件按修改时间排序并输出列表」）；
3. 证据采集（时间戳对齐，逐条入报告）：
   - 客户端收到的完整响应（chatbox 对话截图或导出）；
   - 网关日志：该请求行（含 `x-semantix-cache: miss`）+ `usage_log` 对应行；
   - 旁路会话文件：`docker compose exec semantix-gateway ls /data/sessions/`（容器内确认存在）；
   - **检索证据**（容器内无 semantix CLI，拷出后用本机 CLI）：
     ```powershell
     docker compose -f deploy/docker-compose.yml cp semantix-gateway:/data/gateway.jsonl ./tmp/gw4/gateway.jsonl
     semantix search --db ./tmp/gw4/gateway.jsonl -scope project -retriever bm25 -json "<任务 query>"
     ```
     断言：JSON 输出非空、命中切片的 `type` 含 Result/Prompt、内容与本次对话相关；
4. 等异步 ingest 完成再检索（旁路文件出现后轮询 store，最多等 ~30s）。

### 3.3 M1 门：二次命中 + 零上游调用双证

**样本设计（10 组重复任务）**：
- 每组 = 一个「任务族」：固定完整消息序列（system + 2-3 轮历史 + 最后用户 query，多轮任务形态）。
  第 1 次请求（miss，真实流式，积累 L3 切片）→ 确认 Result 切片入库（3.2 的检索流程，query 用样本 query）
  → 第 2 次**重放完全相同的 body**（字节一致——L3 的 ContextHash 是全量 messages 指纹，改一个字都不命中）；
- query 全部用 ≥3 词完整任务句（规避 §2.3 的短 query fail-closed 边界）；
- 覆盖：10 组 = 8 组通用任务（脚本生成/文档总结/代码审查类）+ 2 组与本次验收同域的任务（网关/缓存问题）；
- 复现脚本：`scripts/gw4-replay/`（临时目录，不入库）用 PowerShell `Invoke-RestMethod` 逐组
  POST 两次 `/v1/chat/completions`（`stream=false` 便于采集；另抽 ≥2 组带 `stream=true` 验证流式路径），
  记录每次响应头与状态码。

**双证（每组样本采集）**：

| 证 | 采集方式 | 断言 |
|---|---|---|
| 证 A：网关侧 | `usage_log` 该组第 2 次请求行 `l3_reuse: true` + `TokensOut: 0`；响应头 `x-semantix-cache: hit` | 命中且计费输出 0 |
| 证 B：New API 侧 | New API 面板日志/消费记录页导出第 1、2 次两条记录（或 API 查询） | 第 2 次 completion_tokens=0（网关合成 usage 透传后 New API 按此计费） |

### 3.4 成本实测（New API 计费口径）

- 计费基准：New API「模型」定价（§3.1-6）按 DeepSeek 官方单价（输入 miss $0.27/M、输出 $1.10/M，2026-08 参考价）；
- 每样本组：`baseline = Σ(首轮 usage × 单价)`；`cached = Σ(二轮命中 usage × 单价)`（合成 usage：
  `completion_tokens=0` + cached prompt 前缀量，D1 落地前按输入单价算）；
- 总节省率 = `1 − Σcached / Σbaseline`，≥10 组样本，**≥30% 达标**；
- **口径差异如实标注**：网关 usage 为 `len/4` 字节估算（`estimator:"bytes/4"`），非真 tokenizer；
  DeepSeek 官方账单按真 token——报告给出估算口径与真值差异的说明（英文偏差约 ±10%、中文偏大，
  见 GW7 结论），不把两者混为一谈；
- D1 若确认「命中降价」：报告补一列「倍率落地后」的节省率（预期更高），但**达标判定只按未调价口径**。

### 3.5 D1 决策记录（与 Song 确认）

确认单（写入报告）：「L3 命中是否对客户端降价？」
选项：① New API 命中倍率下调（如 0.25x）② 不降价，省钱为服务方利润 ③ 仅统计展示不调价。
- Song 确认 → 报告记录决策 + 理由 + New API 侧落地方式（渠道/模型价格配置）；
- Song 未确认（等待期）→ 报告标注「待确认」，验收判定不依赖该项。

### 3.6 产出物

1. `docs/reports/gateway-m1-acceptance.md`（结构：背景 / 环境与配置（含 l3_safe 偏离声明）/
   复现步骤 / M0 证据 / M1 样本表与双证 / 成本表 / D1 决策 / 已知边界与风险 / 结论）；
2. `docs/specs/newapi-gateway-design.md` §0 状态行 + §0.3 两行回写（M0/M1 门已记录，链接报告）；
3. issue #184 关闭评论（报告链接 + 结论摘要）。

---

## 4. 验收标准（issue checklist 逐条 → 证据）

| # | checklist（issue #184 原文） | 判定标准 | 证据落点 |
|---|---|---|---|
| 1 | 用 GW3 compose 起真实链路（真实客户端 → New API → 网关 → DeepSeek） | `docker compose ps` 双 healthy + chatbox 对话成功返回 | 报告 §3 复现步骤 + §4 截图 |
| 2 | M0 门：全链路跑通 + 会话入库后 `semantix search` 检索到切片 | 检索 JSON 命中且内容相关（Result 切片存在） | 报告 §4.2 |
| 3 | M1 门：同任务族第二次命中 `x-semantix-cache: hit` 且上游零调用（网关日志 + New API 计费双证） | ≥10 组全部 hit + 证 A/证 B 齐 | 报告 §5 |
| 4 | 成本实测：≥10 组重复样本，节省 ≥30%（New API 计费口径，注明 len/4 差异） | 表内数字 + 口径说明齐全 | 报告 §6 |
| 5 | §9 D1 决策记录（与 Song 确认后写入报告） | 报告 §7 含决策或「待确认」标注 | 报告 §7 |
| 6 | 产出 `docs/reports/gateway-m1-acceptance.md` | 文件存在且含全部证据与复现步骤 | 仓库文件 |
| 7 | spec §0 状态行回写（M0/M1 门已记录） | git diff 可见回写行 | spec 文件 |

---

## 5. 风险与边界

| 风险/边界 | 处置 |
|---|---|
| Docker daemon 未运行、无 DeepSeek key（阻塞项） | §3.1 第一步需用户配合；未就绪不虚报环境 |
| **`l3_safe_default = false` 默认配置 → M1 永不命中**（最关键执行坑） | 验收配置显式改 `true`（§3.1-3），报告中声明偏离并登记风险：空 deps 时 RuleGate 跳过指纹阶段（spec §3.5 已预告） |
| 短 query（≤2 词）L3 fail-closed（zone 绝对地板，GW2 验收 §6 预告） | 样本 query ≥3 词完整句；若个别组仍 miss，如实记录并分析，不换样本掩盖 |
| ContextHash = 全量 messages → 二轮重放必须字节一致 | 脚本记录并重放完全相同的 body（§3.3） |
| ingest 异步 → 二轮早于入库则 miss | 二轮前用检索确认 Result 切片已入库（§3.2-4） |
| judge 未配 key → 灰区保守 Reject | 样本 query 需强 BM25 匹配（绝对分 ≥0.7）；如出现 Grey 判定，如实记录 |
| 流式 vs 非流式路径差异 | 10 组中 ≥2 组 `stream=true` 验证流式积累与命中（GW2 已修响应侧写记忆） |
| 计费口径：len/4 估算 vs 真 token | §3.4 双口径呈现，达标判定只按网关/New API 口径，差异另述 |
| D1 未确认 | §3.5 兜底：标注待确认，不影响其他判定 |
| 工作区 mid-merge（fix/pr222-conflict） | 文档产出另起干净分支 `feat/issue-184-gw4-acceptance`（基于 origin/main），不污染未完成 merge |
| 真实流量窗口依赖 | 无——样本自造，不等流量（issue 估算里的「等真实流量窗口」不适用本设计） |

---

## 6. 文件面 / 依赖 / 估算

- **文件面**：新增 `docs/reports/gateway-m1-acceptance.md`；修改
  `docs/specs/newapi-gateway-design.md`（§0 状态行 + §0.3 两行）；`deploy/semantix-gateway.toml`
  （本地产物，gitignore 内）与 `.env` 不入库；`tmp/gw4/` 临时证据不入库。
- **依赖**：GW2/GW3/GW7 已合入（无代码依赖）；外部：用户提供 `DEEPSEEK_API_KEY` + 启动 Docker（阶段 0），
  Song 确认 D1（§3.5，可并行）。
- **估算**：1-2 天（环境就绪 + 样本采集 + 报告撰写）。
