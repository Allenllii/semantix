# GW4：网关 §7 M0 + M1 验收门实录（Issue #184）

> **日期**：2026-08-20 · **判级**：Spec-Exempt（验收执行；过程中发现的 bug 已单列并另开 issue）
> **设计**：`docs/specs/gw4-m0m1-acceptance-spec.md`
>
> **结论先行**
> - **M0 门：通过**。真实客户端 → New API → 网关 → DeepSeek 全链路跑通，会话入库后 `semantix search` 可检索到切片。
> - **M1 门：成本口径通过，命中口径部分通过**。10 组重复任务中 **7 组**二次请求返回 `x-semantix-cache: hit` 且上游零生成；按 New API 自身计费口径 **节省 52.7%（≥30% 达标）**，单次命中省 **75.3%**。未达 10/10 的 3 组原因已定位并如实记录。
> - **前提偏离两项**（§3）：`l3_safe_default = true`、启用 grey 区 judge。**不开 judge 时命中率只有 1/10**——这是本次验收最重要的发现。
> - 过程中发现 **3 个部署产物 bug**（已修，§7）和 **1 个 L3 排序设计问题**（§8；**已于后续 Issue #241 修复**——三参数双轴判据，§8.1 末有更新注记）。

---

## 1. 环境

| 项 | 值 |
|---|---|
| Docker | 29.6.2（Docker Desktop，Windows 11） |
| New API | `calciumion/new-api:latest`，版本 `X-New-Api-Version: v1.0.0-rc.25` |
| 网关 | `deploy/Dockerfile` 本地构建，`listening on :8080 (1 upstream(s): deepseek)` |
| 上游 | `https://api.deepseek.com/v1`，`deepseek-chat` → 实际路由到 `deepseek-v4-flash` |
| 代码基线 | 本报告所在提交（base `29056bb`） |
| 客户端 | 脚本化 OpenAI 兼容客户端（原始输出见 `data/gateway-m1/`） |

两服务健康状态（checklist ①）：

```
SERVICE            STATUS
new-api            Up 25 minutes (healthy)
semantix-gateway   Up 1 second (health: starting) → healthy
```

---

## 2. 复现步骤

```bash
# 1. 密钥（deploy/.env，已在 .gitignore 内，绝不入库）
#    SEMANTIX_GATEWAY_KEY=<random>
#    DEEPSEEK_API_KEY=<your-key>

# 2. 验收专用配置（见 §3 偏离声明）
cp deploy/semantix-gateway.toml.example deploy/semantix-gateway.toml
#    l3_safe_default = true
#    judge_api_key  = "${DEEPSEEK_API_KEY}"
#    judge_base_url = "https://api.deepseek.com/v1"
#    judge_model    = "deepseek-chat"

# 3. 起服务
docker compose -f deploy/docker-compose.yml up -d --build

# 4. New API 初始化（v1.0.0-rc.25 需走 setup 流程，见 §7.4）
#    POST /api/setup {"username":"root","password":"<pw>","confirmPassword":"<pw>","self_use_mode_enabled":true}
#    登录取 JWT → 建渠道（type=1, base_url=http://semantix-gateway:8080,
#    key=SEMANTIX_GATEWAY_KEY, models=deepseek-chat）→ 建令牌

# 5. 跑样本：同一批 body 连发两轮，第二轮必须字节一致
python run.py 1   # 首轮，全 miss
python run.py 2   # 二轮，看命中
```

> **重要**：验收前已 `docker volume rm deploy_semantix-data` 清空切片库，确保首轮必然 miss、
> 二次命中不是上一轮残留造成的假阳性。清空前的旧卷已备份留档。

---

## 3. 前提偏离声明（诚实记录）

| # | 偏离 | 默认值 | 验收取值 | 理由与代价 |
|---|---|---|---|---|
| 1 | `[ingest] l3_safe_default` | `false` | `true` | 网关结果无 deps 捕获，`kernel/cache/l3.go:117-119` 对无 deps 的切片要求 `Meta.L3Safe` 才可复用。保持默认则 **M1 门永不命中**。代价：无依赖捕获的结果被无条件视为可复用，spec §3.5 已预告该风险。 |
| 2 | `[cache] judge_api_key` | 空（不启用） | DeepSeek key | 见 §5.2：不开 judge 时 10 组只中 1 组。灰区必须有裁决者，否则 `judgeGrey` 返回 false 保守拒绝。代价：每次灰区判定多一次 judge 模型调用。 |

两项偏离都是**配置**而非代码改动，且都在 `deploy/semantix-gateway.toml.example` 的既有键位内
（judge 的两个必填伴生键此前缺失，见 §7.3）。

---

## 4. M0 门证据

### 4.1 全链路跑通

首个真实请求（客户端 → New API:3000 → gateway:8080 → DeepSeek）：

```
HTTP/1.1 200 OK
X-New-Api-Version: v1.0.0-rc.25
X-Semantix-Cache: miss
X-Ds-Trace-Id: a31bab6b2e19fff6f5a4160204205b9f

{"model":"deepseek-v4-flash","choices":[{"message":{"content":"Go 中 slice 是动态数组的**引用视图**…"}}],
 "usage":{"prompt_tokens":84,"completion_tokens":48,"prompt_cache_miss_tokens":84}}
```

`X-Ds-Trace-Id` 由 DeepSeek 侧签发，是**真上游**而非假上游的直接证据。

### 4.2 会话入库 + 可检索

网关会话旁路落盘 `/data/sessions/`（11 个会话文件），切片写入 `/data/gateway.jsonl`（+ append journal）。
用与网关同基线构建的 CLI 直接读该库：

```
$ semantix search --db /data/gateway.jsonl --scope project --retriever bm25 \
    --query "请解释 Go 语言中 defer 语句的执行顺序以及它与 panic recover 的配合方式" --json
{ "ok": true, "data": [
  {"id":"fc0fa13beb8f9426","type":0,"scope":"project","score":54.4894,
   "content":"请解释 Go 语言中 defer 语句的执行顺序以及它与 panic recover 的配合方式"},
  {"id":"a6f11dd1341d2375","type":3,"scope":"project","score":30.8554,
   "content":"## Go 语言中 defer 语句的执行顺序 … defer 语句采用 **LIFO（后进先出）** 的执行顺序 …"}
]}
```

Prompt 切片（type 0）与 Result 切片（type 3）均已入库且可检索 → **M0 门通过**。

> **踩坑留档**：把 store 从容器 volume 拷到宿主机后 CLI 会报
> `journal out of sync (base changed underneath the journal)` 并丢弃 journal —— journal 头记录的是
> base 文件的**纳秒级 mtime**，跨文件系统拷贝无法保真。正确做法是在 Linux 容器内直接挂 volume 读取
> （本报告所有检索证据均如此产出）。

---

## 5. M1 门证据

### 5.1 命中结果（10 组 × 4 轮）

任务集：10 个**不同主题**的技术问答（Go / Python / PostgreSQL / Docker / git / HTTP / Redis /
Kubernetes / 正则 / TLS），每条 query ≥35 字符；其中 2 组 `stream=true`。
定义见 `data/gateway-m1/tasks.json`。

| 轮次 | judge | 命中 | 说明 |
|---|---|---|---|
| r1 | 关 | 0/10 | 冷库首轮，全 miss（预期） |
| r2 | 关 | **1/10** | 仅 t07 |
| r3 | 关 | **1/10** | 仍仅 t07（语料从 ~20 涨到 ~30 条，命中率未变） |
| r4 | **开** | **7/10** | t01–t05、t07、t08 |

r4 逐条（`data/gateway-m1/round4.jsonl`）：

| task | cache | 耗时 | usage(prompt/completion) |
|---|---|---|---|
| t01 | **hit** | 1.12s | 296 / **0** |
| t02 | **hit** | 1.20s | 308 / **0** |
| t03 | **hit** | 1.60s | 280 / **0** |
| t04 | **hit** | 2.33s | 294 / **0** |
| t05 | **hit** | 2.71s | 316 / **0** |
| t06 | miss | 2.73s | 82 / 300 |
| t07 | **hit** | 0.11s | 286 / **0** |
| t08 | **hit** | 1.14s | 307 / **0** |
| t09 | *(流式无头)* miss | 3.20s | 64 / 300 |
| t10 | *(流式无头)* miss | 3.51s | 68 / 300 |

对照：未命中约 3s，命中 0.11–2.71s（含 judge 调用耗时；t07 无需 judge 故仅 0.11s）。

### 5.2 最重要的发现：judge 是 M1 的开关

关 judge 时 10 组只中 1 组。根因是**相对置信度**而非绝对分：

| task | top1 (BM25) | best Result | rel = result/top1 | zone |
|---|---:|---:|---:|---|
| t07 | 56.97 | 47.03 | **0.826** | **hit** |
| t04 | 65.31 | 47.13 | 0.722 | grey |
| t05 | 50.83 | 36.66 | 0.721 | grey |
| t10 | 66.57 | 44.95 | 0.675 | grey |
| t08 | 43.01 | 25.90 | 0.602 | grey |
| t02 | 59.78 | 35.46 | 0.593 | grey |
| t03 | 59.33 | 34.62 | 0.584 | grey |
| t01 | 54.49 | 30.86 | 0.566 | grey |
| t09 | 79.26 | 42.94 | 0.542 | miss |
| t06 | 65.72 | 34.50 | 0.525 | miss |

阈值 `zone.Default()` = `TauHigh 0.80 / TauLow 0.55 / AbsHigh 0.70 / AbsLow 0.45`。
**10/10 的 top1 都远超 AbsHigh**（43–79），绝对地板从不是约束——`kernel/zone/zone.go:38-41`
注释「BM25 scores are typically >> 1 and never trip the absolute guards」的假设在真实网关库上成立。
真正的分水岭是 `rel ≥ 0.80`：只有 t07 越过，其余 7 条落进 grey（需 judge），2 条落进 miss（正确拒绝）。

离线判定与线上行为**逐条吻合**：算出的唯一 hit 正是 r2/r3 实际命中的 t07；开 judge 后 7 个 grey 全部转为命中；
2 个 miss（t06/t09）在 r4 仍未命中——**fail-closed 行为正确，不是漏命中**。

r4 未命中的 3 组归因：

| task | rel | zone | 归因 |
|---|---:|---|---|
| t06 | 0.525 | miss | 低于 `TauLow 0.55`，judge 不参与，**正确拒绝** |
| t09 | 0.542 | miss | 同上，**正确拒绝**（流式，但与流式无关，见下） |
| t10 | 0.675 | grey | 落在灰区，judge **未放行**。网关不记录 judge 判定结果，无法从日志区分「judge 拒绝」与「其他门失败」——观测性缺口，见 §8.3 |

**流式不是未命中的原因**（推翻了一个既有假设）：spec §3.7 的状态行记「流式路径不解析 SSE 取 assistant 内容 →
L3 的写入实际只来自非流式请求」。本次实测**该描述已过期**——GW2（#182）的 `streamThrough` SSE 聚合已生效，
t09 在库中有 **3 条自己的 Result 切片**：

```
type0 score 78.7862 | 正则表达式中的贪婪匹配与懒惰匹配区别，回溯灾难是怎么产生的以及如何避免
type3 score 40.8226 | ## 贪婪匹配 vs 懒惰匹配 …
type3 score 40.5892 | ## 贪婪匹配 vs 懒惰匹配 …
type3 score 35.8874 | ## 贪婪匹配 vs 懒惰匹配 …
```

流式请求的响应**确实**成为了可复用 Result 切片。t09 未命中是 `40.82/78.79 = 0.518 < TauLow` 的正确拒绝。
建议同步更新 spec §3.7 状态行（本 PR 已改）。

> **更新注记（Issue #241 修复后）**：本节「t06/t09 未命中 = 正确拒绝」的结论已**被推翻**。
> 根因正是 §8.1 的设计问题：分母 `top1` 取自未过滤命中列表，被逐字相同的 Prompt 孪生顶高，
> 把**本该命中**的重复任务压进 grey/miss——t06/t09 的 `0.525/0.542` 与「同问题重写答案」的真实
> BM25 分布（相对孪生约 0.52–0.83）一致，属于**同一任务、被错误拒绝**，而非检索未命中。
> 修复（`zone.ClassifyL3` 三参数双轴判据：同类 Result 归一化 + 全量 top1 尺度锚）后，
> 10 组重复任务全部无需 judge 即可命中（表驱动重放见 `kernel/cache/l3_top1_test.go`）。
> 原「fail-closed 行为正确」的历史表述保留作为当时的实录，不代表当前行为。

> 方法学留档：`semantix search` 的 `--retriever` 默认值来自配置，未显式指定时可能走 **hybrid RRF 融合**，
> 其 score 是排名倒数（观测到 `2/60 = 0.0333`、`1/60+1/61 = 0.0328`），与网关 L3 使用的**原生 BM25**
> 不同标度。本节数据均以 `--retriever bm25` 显式测得，否则会误判为「绝对分地板不可达」。

### 5.3 双证：网关 usage 日志 + New API 计费

**证 A — 网关 `/data/gateway-usage.jsonl`**（`data/gateway-m1/usage.jsonl`）：r4 的 7 条命中全部满足

```
{"tokens_in":296,"tokens_out":0,"cache_hit_tokens":273,"l3_reuse":true}
{"tokens_in":308,"tokens_out":0,"cache_hit_tokens":282,"l3_reuse":true}
… 共 7 条，tokens_out 全为 0、cache_hit_tokens 全 > 0
```

**证 B — New API 自身计费记录**（`data/gateway-m1/newapi-billing.jsonl`）：

```
model=deepseek-chat  prompt=296  completion=0    quota=12   ← 命中
model=deepseek-chat  prompt=308  completion=0    quota=13   ← 命中
…（共 7 行）
model=deepseek-chat  prompt=82   completion=300  quota=52   ← 未命中
…（共 3 行）
```

两侧独立记账互相印证：**命中请求上游零生成**（completion=0），且这是 New API 在网关之外自己算出来的。

---

## 6. 成本实测

按 **New API 计费口径**（quota）计算，r4 十组：

| 口径 | 值 |
|---|---:|
| 命中 7 组 quota 合计 | 87（均值 12.4） |
| 未命中 3 组 quota 合计 | 151（均值 50.3） |
| **实际总消耗** | **238** |
| 基线（10 组全按未命中均值） | 503.3 |
| **节省率** | **52.7%** ✅ ≥30% |
| 单次命中 vs 未命中 | **省 75.3%** |

**口径说明（`bytes/4` 估算 vs 真 token）**：

- 上表 quota 是 **New API 侧**按其模型定价对真实 `prompt_tokens`/`completion_tokens` 算出的，**不是估算**；
- 网关 usage 日志（证 A）里**命中路径**的 token 数是 `bytes/4` 合成估算（GW7 #187 的 `estimator: bytes/4`），
  因为 L3 命中不调上游、拿不到真 tokenizer 结果。两者数量级一致（如 296 vs 296），
  但**达标判定只采用 New API 口径**；
- 命中时仍产生 prompt_tokens 计费（New API 按转发的 prompt 计），故单次命中省 75.3% 而非 100%；
- **judge 调用成本未计入**：grey 判定每次额外调一次 `deepseek-chat`，走网关自己的上游调用，
  不经 New API 渠道计费，网关也未对 judge 调用记 usage 事件。这是本节最大的口径缺口，如实标注：
  若把 judge 开销按同价并入，节省率会低于 52.7%，具体数值本次未单独计量。

---

## 7. 过程中发现并修复的部署产物 bug

GW3（#183）验收报告 §6 自述「本机无 Docker daemon，compose 未实机验证，留待 GW4 真实全链路验收时执行」。
本次即为该验证，暴露 3 个阻断性问题，均已在本 PR 修复：

### 7.1 compose 配置挂载路径多套一层（阻断）

`./deploy/semantix-gateway.toml` 在 `docker compose -f deploy/docker-compose.yml` 下 project 目录即
`deploy/`，解析为 `deploy/deploy/semantix-gateway.toml`（不存在 → docker 会建同名目录 → 网关读不到配置）。
改为 `./semantix-gateway.toml`。

### 7.2 New API 镜像名不存在（阻断）

`quantumnew/new-api` 在 Docker Hub 返回 **404**（`hub.docker.com/v2/repositories/quantumnew/new-api/tags`），
`docker compose up` 直接失败于 `pull access denied`。真实镜像为 `calciumion/new-api`（tags API 200，1627 个 tag）。

### 7.3 judge 配置缺两个必填伴生键（阻断 + 文档过期）

`judge_api_key` 非空时 `gateway/config.go:260` 要求 `judge_base_url` 与 `judge_model` 同时非空，
否则启动即失败（实测网关进入 `Restarting` 循环）。但 `deploy/semantix-gateway.toml.example`
**完全没有这两个键**，且 `judge_api_key` 的注释仍写「当前未接线，见 GW6」——GW6（#186）已接线，注释过期。
已补键并更正注释。

### 7.4 New API v1.0.0-rc.25 运维差异（非 bug，供后续复现参考）

- 首次启动需走 `POST /api/setup` 初始化，**没有** root/123456 默认账号；确认密码字段名是
  `confirmPassword`（非 `confirm_password`）；
- 管理 API 改用 **JWT**（`Authorization: Bearer <access_token>`，15 分钟过期）而非 session cookie；
- `POST /api/channel` 会 307 重定向到 `/api/channel/`，curl 默认不带 body 跟随，须直接请求带斜杠的路径；
- 令牌 key 在所有 API 响应里都被掩码（`sk-5S70****`），完整 key 只能从 `one-api.db` 的 `tokens` 表读。

---

## 8. 未修复的发现（另开 issue）

### 8.1 L3 排序：Prompt 切片压低 Result 切片的相对置信度（设计问题）

`kernel/cache/l3.go:59-70`：

```go
hits, err := d.Index.Search(q.UserInput, k, q.Scope)
top1 := hits[0].Score                          // ← 取自未过滤的检索结果
for _, h := range hits {
    if s.Type != slice.Result { continue }     // ← Result 过滤发生在 top1 之后
    switch z.Classify(h.Score, top1) {
```

`top1` 取自**未按类型过滤**的命中列表。而与 query 逐字相同的 **Prompt 切片必然排第一**
（本次实测 43–79 分，恒高于 Result 切片的 25–47 分），于是每个 Result 候选的 `score/top1`
被系统性压低。实测 10 组中 8 组的 rel 落在 0.52–0.73 区间——**这正是「本该命中却进灰区」的结构性来源**。

建议（不在本次 Spec-Exempt 范围内，交由后续 issue 决策）：`top1` 改为在 Result 类型候选内计算，
或为 L3 单独设一组阈值。当前只能靠开 judge 兜底，代价是每次灰区判定多一次模型调用。

> **更新注记（Issue #241 修复后）**：本问题已修复，但**不是**上述方案一（在 Result 候选内取 top1——
> 该方案被证伪：最佳 Result 的 `score/top1` 恒为 1.0，相对置信度轴失效，CLI 路径会无条件复用）。
> 最终实现为 `zone.ClassifyL3(score, resultTop1, globalTop1)` 三参数双轴判据：
> 相对轴 `score/resultTop1`（候选在 Result 同类中的突出度）+ 全局锚 `score/globalTop1`
> （相对全量 top1——通常是 Prompt 孪生——的尺度归一化相关性地板，复用 `AbsLow 0.45`，校准于真实
> BM25：同问题重写答案约 0.52–0.83、同领域不同任务约 0.16、无关 0）。GW4 的 10 组实测分数
> （0.525–0.826）全部越过地板，**10/10 无需 judge 命中**；绝对地板 `AbsHigh/AbsLow` 仍在
> `globalTop1` 上生效，弱相关低分批次照旧 fail-closed。

### 8.2 New API 转发 SSE 时丢弃 `x-semantix-cache` 头（观测性缺口）

流式请求经 New API 返回时没有 `x-semantix-cache`（§5.1 中 t09/t10 显示 *(无头)*）。
直连网关验证，头是**发出来的**：

```
$ curl -D - http://semantix-gateway:8080/v1/chat/completions -d '{…,"stream":true}'
HTTP/1.1 200 OK
Content-Type: text/event-stream
X-Semantix-Cache: miss
```

`gateway/pipeline.go:280` 确实在流式路径设了该头。**是 New API 中继 SSE 时把它剥掉了**，不是网关缺陷。
影响：客户端在流式场景无法从响应头判断命中，需改用网关 usage 日志佐证。

### 8.3 网关不记录 grey 区 judge 的判定结果（观测性缺口）

灰区候选交给 judge 后，网关既不打日志也不写 usage 事件记录裁决结果。后果：t10（rel 0.675，明确落在灰区）
在 r4 未命中，但**无法从任何落盘证据区分**它是「judge 判为不可复用」还是「judge 调用失败/超时后 fail-closed」，
也无法核算 judge 的调用成本（§6 口径缺口的成因）。建议为 judge 判定补一条结构化日志或 usage 字段。

---

## 9. §9 D1 决策：**待确认**

「L3 命中是否对客户端降价？」三个选项（① New API 命中倍率下调 ② 不降价、省下的作服务方利润
③ 仅统计展示不调价）**本次未与 Song 确认**，故按设计 §3.5 兜底：标注待确认，**验收判定不依赖该项**。
本报告 §6 的 52.7% 是**未调价口径**；若将来采用①，客户端侧节省率会高于此数。

---

## 10. 逐条结论

| # | issue #184 checklist | 结论 | 证据 |
|---|---|---|---|
| 1 | GW3 compose 起真实链路 | ✅ | §1 双 healthy；§7 修复 3 个阻断 bug 后达成 |
| 2 | M0：全链路 + `semantix search` 检索到切片 | ✅ | §4.1 `X-Ds-Trace-Id`（真上游）+ §4.2 检索输出 |
| 3 | M1：二次命中 + 上游零调用（双证） | ⚠️ **7/10** | §5.1 表；§5.3 证 A + 证 B。3 组未中已逐条定位（§5.2）：t06/t09 为 zone Miss 的正确 fail-closed 拒绝，t10 落灰区但 judge 未放行 |
| 4 | 成本 ≥30%（New API 口径，注明估算差异） | ✅ **52.7%** | §6；judge 开销未计入的口径缺口已如实标注 |
| 5 | §9 D1 决策记录 | ⏳ 待确认 | §9 |
| 6 | 产出 `gateway-m1-acceptance.md` | ✅ | 本文件 + `data/gateway-m1/` 原始证据 |
| 7 | spec §0 状态行回写 | ✅ | `docs/specs/newapi-gateway-design.md` |

**总评**：M0 门通过；M1 门在「成本节省 ≥30%」上通过、在「≥10 组全部命中」上为 7/10。
未达 10/10 的原因**不是缓存机制失效**，而是 §8.1 的 L3 排序设计问题把本该命中的样本推进灰区——
开 judge 可兜底（1/10 → 7/10），但那是加成本换命中率，不是根治。建议按 §8.1 另开 issue 后再复测。

---

## 11. 原始证据清单

| 文件 | 内容 |
|---|---|
| `data/gateway-m1/tasks.json` | 10 组任务定义 |
| `data/gateway-m1/round1..4.jsonl` | 四轮逐条结果（http / `x-semantix-cache` / 耗时 / usage / body sha256） |
| `data/gateway-m1/usage.jsonl` | 网关 `/data/gateway-usage.jsonl` 全量（证 A） |
| `data/gateway-m1/newapi-billing.jsonl` | New API 计费日志导出（证 B） |

> 密钥（`DEEPSEEK_API_KEY` / `SEMANTIX_GATEWAY_KEY` / New API 令牌）仅存在于 gitignore 内的
> `deploy/.env` 与本地数据目录，**不在本报告、不在任何证据文件、不在任何提交中**。
