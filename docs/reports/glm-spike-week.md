# GLM Spike 周报告：semantix-glm-optimization spec 的 5 个事实缺口

> 对应 Issue：#233 · spec：`docs/specs/semantix-glm-optimization.md`（harness-integration 分支草案）§8
> 判级：Spec-Exempt（E2 调研/报告，不改代码）
> 状态（2026-08-20）：五个 Spike 全部有产出。Spike-4/5 浏览器人工核（截图留证）；
> Spike-1/2/3 在**两个第三方网关**上各自完整实测——腾讯云 MaaS→GLM-5.3（§1–§3，本会话）
> 与 AtomClub→GLM-5.2（§3A，并行会话），跨栈对比见 §3B——
> 结果回答的是 spec §3.3/§3.4 的第三方托管线，Z.AI 官方端点数值待同款实验复测。
> 方法：/browse headless 浏览器逐页核对原文；受控 API 实验共 35 请求（脚本 `glm_spike.py` + 补验脚本，
> 预算护栏 ≤40 请求，实测总消耗 ~75K input tokens；原始 usage 逐条 JSONL 为会话产物）

**端点定性（先读这个再引用数字）**：spec §3.3 警告过「经聚合网关接 GLM 时不能按 Z.AI 价目/行为估算」。
本次 Spike-1/2/3 的被测对象是腾讯云托管栈（serving 实现未知，疑似 vLLM/SGLang 系 + 网关层），
其缓存 TTL、命中粒度、usage 字段形态是**该托管栈的属性**，不能直接当作 Z.AI 官方 api.z.ai 的行为回填；
但它 ①直接回答了「经第三方网关接 GLM 时 L1 纪律是否成立、如何遥测」（semantix 用户的真实形态之一），
②与 spec §3.3 阿里云托管实测（96.2% 命中）互为独立佐证。Z.AI 官方端点的同款实验脚本已就绪，key 到位即可复测。

---

## 0. 一页结论

| Spike | 状态 | 一句话结论 |
|---|---|---|
| 1 TTL/命中率曲线 | ✅ 双网关完成；Z.AI 直连待 key | 腾讯云→5.3：**TTL ∈ (8, 12] 分钟**（1–8 分钟全命中，12 分钟真过期）；AtomClub→5.2：0–120s 命中 96–98%、**301s 即降至 28%**——TTL 因栈而异达数倍（§3B） |
| 2 Anthropic 兼容端点 | ✅ 双网关完成；Z.AI 直连待 key | 两栈 `/v1/messages` 均真实工作、**cache_control 两位置均不报错可透传**；usage 形态不同：腾讯云命中才出 `cache_read_input_tokens`（增量语义），AtomClub 恒含 cache 字段族（§3B） |
| 3 「高度相似」边界 | ✅ 双网关完成；Z.AI 直连待 key | 两栈一致：**无相似/非前缀命中**，首 token 差异即零命中；粒度不同：腾讯云**全有或全无**（中段 25%/82% 差异全清零），AtomClub 块级保留改动点前 ~48%（§3B） |
| 4 国内价目核对 | ✅ 完成 | **传闻口径两列颠倒**：官方页为「缓存存储＝限时免费、缓存命中＝¥2/M」；GLM-5.3 **非阶梯计价**（¥8/¥28，1M ctx） |
| 5 免费档数据条款 | ✅ 完成 | **国际 api.z.ai API 默认不训练（无免费档例外）→ judge 可行；国内 bigmodel 保留匿名化训练权利 → 不建议** |

对 spec 的直接影响（详见 §6 回写清单）：
1. §3.1 国内价目行从 reported 升 official，且**内容要改**（两口径均不实）；
2. 「缓存存储按 ¥/M/小时计费」的维度在官方价目表**真实存在**、现全系限免——限免结束是新增成本风险项（进 §10）；
3. §4.3 免费 flash 档跑 judge 获得条款依据，但**必须限定走国际 api.z.ai 端点**，不走国内 bigmodel 免费端点；
4. L1 字节稳定纪律在托管栈上**比官方措辞更硬**：任何一字节差异清零全部缓存收益（§3），前缀卫生的优先级只升不降；
5. 命中率遥测的**分母天花板 ~97.6%**（尾部不足块不计入 cached）：§4.2 的 85% 告警阈值合理，但验收线不可设 >95%。

---

## 1. Spike-1 缓存 TTL/命中率实测（完成 · 腾讯云栈）

**目标**：GLM 隐式缓存 TTL 未公布（官方仅称「合理的时效性」，第三方称均值 3–5 分钟），
拟合「turn 间隔 → 命中率」经验曲线，决定 §4.5 是否需要 TTL 对策。

**设计**（`glm_spike.py spike1`）：每个间隔一个**独立前缀**（~2620 tokens，run id 打头隔离历史缓存），
t=0 全部种缓存，各自等 Δt 后**只探测一次**——规避「探测本身刷新缓存」对曲线的污染
（与朴素连续探测设计的关键差异）。模型 glm-5.3。

**结果**（cached_tokens / prompt_tokens）：

| 间隔 | 实测 gap | cached | 判定 |
|---|---|---|---|
| 0.5 min | 30s | **0** / 2628 | 异常：见下「持续不可见」 |
| 1 min | 60s | 2560 / 2624 | 命中 |
| 2 min | 120s | 2560 / 2624 | 命中 |
| 3 min | 180s | 2560 / 2624 | 命中 |
| 5 min | 300s | 2560 / 2624 | 命中 |
| 8 min | 480s | 2560 / 2624 | 命中 |
| 12 min | 720s | **0** / 2624 | **过期**（复测证实，见下） |
| 20 min | 3016s* | **0** / 2624 | 过期（*进程调度延迟，实际 gap ≈50 min，结论同向） |

**两个对照补验**：
1. **12 分钟点是真过期**：该探测 miss 时自身重种缓存，约 2 分钟后复测**命中 2560**——
   条目健康、重种即生效，12 分钟的 0 是过期而非丢失；
2. **0.5 分钟点是另一种现象**：种下 30 秒探测 0，约 17 分钟后复测**仍 0**（三连不可见），
   其余 7/8 前缀行为正常——疑多实例路由不一致导致的**前缀级本底丢失**（样本 n=1，不定量）。

**结论**：
1. **经验 TTL ∈ (8, 12] 分钟**（8 分钟存活、12 分钟与 ~50 分钟两点过期）——单轮实验无法区分固定 TTL
   与负载相关 LRU 逐出，按区间表述；「第三方均值 3–5 分钟」在该栈**低估**约一倍；
2. 对 §4.5 的判定：coding agent 的常规 turn 间隔（秒级到几分钟）**落在安全区**，
   「不预设 keep-alive ping」的现行决策成立；但 >10 分钟的长思考/长工具等待（大规模测试、长编译）
   会系统性丢前缀缓存——若遥测显示此类会话占比可观，keep-alive 的成本收益要按
   「一次 ping ≈ 全量前缀重算的 1/N」重新核算（联动国内「缓存存储限免」结束后的存储费风险）；
3. 种下后 ~30 秒内探测不命中（0.5min 点 + 该点复测线索）：**写入生效有延迟或路由抖动**——
   紧凑多轮（秒级 turn）的首几轮命中率天然偏低，遥测按「排除首轮」口径统计更准。

## 2. Spike-2 Anthropic 兼容端点行为（完成 · 腾讯云栈）

**目标**：Anthropic 兼容端点的 usage 是否返回 `cache_read_input_tokens` 类字段；
对 Anthropic 风格 `cache_control` 块是忽略还是报错——决定 gateway 对 Claude Code 类 harness 的透传策略。

**被测端点**：`tokenhub.tencentmaas.com/v1/messages`（腾讯云 MaaS 的 Anthropic 兼容路径，实测存在且完整：
返回 Anthropic message envelope——content 块数组、thinking 块带 signature、`stop_reason: end_turn`）。
注意该端点**强制显式 thinking 参数**：不带 `thinking` 的请求返回业务 400
「该模型始终思考，不支持关闭思考；请使用 low、high 或 max」；`thinking: {type: enabled, budget_tokens: N}` 可用，
OpenAI 风格 `reasoning_effort` 字段在此路径**不被接受**（同样 400）——两种兼容路径的 thinking 参数体系不互通。

**结果**（`glm_spike.py spike2`，4 请求，~2180 token system 前缀）：

| # | 变体 | HTTP | usage 实测 |
|---|---|---|---|
| 2a | 无 cache_control 基线 | 200 | `{input_tokens: 2186, output_tokens: 51}` —— **未命中时无任何 cache 字段** |
| 2b | 同前缀立即重发 | 200 | `{input_tokens: 10, output_tokens: 16, cache_read_input_tokens: 2176}` —— **命中透出 Anthropic 字段，input 为增量** |
| 2c | system 块带 cache_control | 200 | 同 2b（照常命中 2176）—— **不报错** |
| 2d | 消息内容块带 cache_control | 200 | 全量 input（另一缓存键首次出现，非命中失败）—— **不报错** |

**gateway 决策（该栈）**：
1. `cache_control` 两个位置均不报错 → **透传安全，净化中间件无须剥离该字段**（剥离逻辑保留为 per-provider 配置项，Z.AI 官方端点复测前不默认开启）；
2. usage 适配器该端点走 **Anthropic 语义**：命中时才出现 `cache_read_input_tokens`，`input_tokens` 不含缓存部分（2176+10=2186 对账成立）；无 `cache_creation_input_tokens`（隐式缓存无写费概念）——与 OpenAI 风格路径（恒有 `prompt_tokens_details.cached_tokens`、prompt 为全量）**同栈不同形态**，§4.2 的 per-provider 适配必须细化到 per-路径；
3. Anthropic 路径与 OpenAI 路径共享同一隐式缓存（2b 立即命中，粒度一致）。

**残留**：`api.z.ai/api/anthropic` 官方端点行为待同款脚本复测（本 key 不覆盖）。

## 3. Spike-3 「高度相似」边界（完成 · 腾讯云栈）

**目标**：官方缓存文档称自动识别「相同**或高度相似**的内容」——验证 GLM 隐式缓存是否存在
非严格前缀命中（若有，L1 字节稳定纪律的收益模型与 CacheBlend 类演进的紧迫度都要重估）。

**结果**（`glm_spike.py spike3` + `spike3b.py`，9 请求，~2620 token 前缀 P 置于 system，尾部为 user 消息）：

| # | 变体 | cached_tokens / prompt_tokens |
|---|---|---|
| r0 | P + 尾A（种缓存） | 0 / 2620 |
| r1 | P + 尾B（同前缀异尾） | **2560 / 2620（97.7%）** |
| r2 | P 中段 25% 处改一句 + 尾B | **0** / 2624 |
| r3 | 首字符加 "X" + 尾B | **0** / 2621 |
| r4 | 中段多一个空格 + 尾B | **0** / 2621 |
| r5 | 与 r1 完全相同重发 | 2560 / 2620 |
| 3b-ctrl | 独立前缀对照（种后即测） | 2560 / 2623 |
| 3b-late | 前缀 82% 处改一句 | **0** / 2626 |

**结论**：
1. **「高度相似」命中不存在**：首 token、中段（25% 与 82% 两处）、单空格差异一律 cached=0——
   该栈为严格匹配，无相似/滑动容错；spec §4.6 表 Spike-3 行（是否已具备非严格前缀命中）在托管栈上答案为**否**，
   CacheBlend 类非前缀复用未被 provider 侧实现，L1 字节稳定仍是唯一路径；
2. **全有或全无，且粒度粗于 DeepSeek**：改动点之前的部分（25% 处≈650 tokens、82% 处≈2150 tokens）**也不命中**——
   不是 64-token 块级前缀树行为；机制上无法从外部区分「system 段整体 hash」与「极大最小命中门槛（>2150 tokens）」，
   但工程含义相同：**注入块任何一字节漂移 = 全部 L1 收益清零**，官方「细微格式差异可能影响缓存」在该栈是必然清零；
3. **命中报告天花板 ~97.6%**（2560/2620，尾部 ~60 tokens 不足块/门槛部分恒不计入 cached）：
   命中率遥测与验收线要按该天花板校准（§0 影响 5）。

## 3A. AtomClub gateway 预实验：Spike-1/2/3（2026-08-20）

> **证据边界**：本节测量的是 `api.atomclub.cn` → `glm-5.2` 路由，不能替代
> `api.z.ai` 官方直连实验。代理层可能改变路由、缓存和 usage 字段；因此 §1–§3 的 Z.AI
> 状态仍为待 key。本节用于提前验证实验方法，并为实际使用 AtomClub gateway 的部署提供端点级证据。

**实验设置**：合成长前缀（约 6,396 prompt tokens，唯一 run id 隔离历史缓存），
`max_tokens=1`、`temperature=0`；只记录脱敏 usage 和状态码，不发送用户代码、不落盘 API key。
运行窗口：2026-08-20 05:31–05:39 UTC。每个 TTL 间隔使用独立前缀且只探测一次，避免探测刷新。

### 3A.1 TTL / 命中比例

| 目标间隔 | 实际间隔 | prompt_tokens | cached_tokens | cached / prompt |
|---:|---:|---:|---:|---:|
| 0s | 0.0s | 6,396 | 6,272 | 98.06% |
| 30s | 31.0s | 6,396 | 6,144 | 96.06% |
| 60s | 60.8s | 6,396 | 6,272 | 98.06% |
| 120s | 120.3s | 6,396 | 6,272 | 98.06% |
| 300s | 301.0s | 6,396 | 1,792 | 28.02% |

**预实验判断**：0–120 秒保持 96%–98% 前缀命中；约 5 分钟时只剩 28%，
已触发 §1 的「5 分钟点 <50%」提前考虑 TTL 对策条件。该曲线每点仅一个样本，足以形成
端点级工程预警，但不足以估计统计命中率；Z.AI 直连时仍按 §1 设计补齐 8/12/20 分钟点和重复样本。

### 3A.2 Anthropic Messages 兼容行为

AtomClub 的有效兼容入口为 `POST https://api.atomclub.cn/v1/messages`。响应 `type=message`，
usage 含 `input_tokens`、`cache_creation_input_tokens`、`cache_read_input_tokens`、
`output_tokens`、`claude_cache_creation_5_m_tokens`、`claude_cache_creation_1_h_tokens`。

| 请求 | HTTP | input_tokens | cache_read_input_tokens | cache_creation_input_tokens |
|---|---:|---:|---:|---:|
| 长前缀基线 | 200 | 6,394 | 0 | 0 |
| 同前缀、尾部变化 | 200 | 6,394 | 6,272 | 0 |
| system 块带 `cache_control: ephemeral` | 200 | 6,394 | 6,272 | 0 |
| message 块带 `cache_control: ephemeral` | 200 | 6,394 | 6,144 | 0 |

**预实验判断**：两处 `cache_control` 均被接受，不需要为 AtomClub 路由强制剥离；但
`cache_creation_input_tokens` 始终为 0，观测到的是 GLM 隐式缓存，而不是 Anthropic 显式缓存创建。
AtomClub usage 适配器应在 OpenAI 路由解析 `prompt_tokens_details.cached_tokens`，在 Messages
路由解析 `cache_read_input_tokens`。此结论不外推到 `api.z.ai/api/anthropic`。

### 3A.3 「高度相似」边界

| 变体 | prompt_tokens | cached_tokens | cached / prompt |
|---|---:|---:|---:|
| 首次种缓存 | 6,392 | 0 | 0% |
| 前缀相同、只改尾部 | 6,392 | 6,272 | 98.12% |
| 320 段中第 160 段改单词 | 6,392 | 3,072 | 48.06% |
| 第一个 token 前增加字符 | 6,393 | 0 | 0% |
| 第 160 段增加一个空格（复测） | 6,396 | 3,072 | 48.03% |
| 完全重放 | 6,392 | 6,272 | 98.12% |

**预实验判断**：行为符合严格前缀缓存——中段差异只保留差异点之前的缓存，首 token 差异
完全失配，单个空格同样截断后续缓存；未观察到跨差异点的「语义高度相似」命中。因此实现仍应
优先保证字节/token 稳定，不把官方「高度相似」措辞当作可依赖的非前缀缓存能力。

**成本与安全**：本轮约 16.6 万输入 tokens，输出上限极低；实际账单以 AtomClub 控制台为准。
实验密钥只存在于交互式进程内，报告与脚本均不包含密钥。由于凭据曾通过会话提供，实验后应轮换。

## 3B. 跨栈对比：同一实验设计在两个网关上的分歧（本次 Spike 周最重要的单一发现）

§1–§3（腾讯云 MaaS→GLM-5.3）与 §3A（AtomClub→GLM-5.2）用同构实验设计测同一模型家族，
三个行为维度给出**互不相同**的答案（注意变量未隔离：网关栈与模型版本同时不同，差异不能归因单一因素）：

| 维度 | 腾讯云→5.3（§1–§3） | AtomClub→5.2（§3A） | 阿里云→5.2（spec §3.3 单源） |
|---|---|---|---|
| TTL | **(8, 12] 分钟**，过期为原子失效 | **~5 分钟已剩 28%**，形态像块级 LRU 部分逐出（1792/6396 恰为块倍数） | 2.5h 会话 96.2% 命中（未测 TTL） |
| 中段差异行为 | **全有或全无**（25%/82% 处改动均全清零） | **块级保留改动点前**（中点改动仍命中 48%） | 未测 |
| Anthropic usage 形态 | 命中才出 `cache_read_input_tokens`，miss 时无 cache 字段 | **恒含** `cache_read/cache_creation` 及 `claude_cache_creation_5_m/1_h` 字段族 | 未测 |
| 严格前缀（首 token 敏感） | ✅ 是 | ✅ 是 | — |
| 「高度相似」语义命中 | ❌ 无 | ❌ 无 | — |
| cache_control 容忍 | 两位置不报错 | 两位置不报错 | — |

**结论**：
1. **跨栈稳定不变量只有三条**：严格前缀匹配、首 token 差异即零命中、cache_control 透传不报错——
   只有这三条可以作为 provider 无关的通用假设；**TTL、命中粒度、usage 字段形态全部按栈标定**；
2. spec 推演二的「provider 无关机制、按 provider 标定」从设计原则升级为**实测硬需求**：
   两个真实网关连缓存粒度机制都不同（原子 vs 块级），任何硬编码单一行为模型都会在另一个栈上失真；
3. 对遥测（§4.2）的直接要求：per-provider × per-路径适配之外，**TTL/粒度画像也应从遥测在线估计**
   （命中率曲线按端点自动拟合），而不是配置文件写死；
4. L1 前缀卫生（§4.1）的优先级在两栈结论一致：字节稳定是唯一可依赖的通用红利。

## 4. Spike-4 国内 bigmodel 价目核对（完成）

**方法**：headless Chromium 渲染 https://bigmodel.cn/pricing 后读取正文与表格（2026-08-20），
关键表格截图留证（会话产物 `spike4-bigmodel-pricing.png`）。该页为 JS 渲染，纯 HTTP 抓取不可得，
与 spec 判断一致。

**核验结果**：

1. **GLM-5.3 非阶梯计价**：上下文 1M，输入 **¥8/M**、输出 **¥28/M**，无输入长度分档。
   「GLM-5.3 阶梯计价」的传闻不实——**阶梯的是 GLM-5.1 / 5-Turbo / 5 / 4.7 / 4.5-Air**（按输入长度、
   部分含输出长度维度分档）。
2. **缓存两列口径与传闻相反**：价目表含两个缓存列——
   「缓存存储（百万tokens/小时）」＝**限时免费**（全系一致）；「缓存命中（百万tokens）」＝**¥2**（GLM-5.3/5.2）。
   传闻「缓存命中限时免费、缓存存储 ¥1/M/小时」**两列颠倒，且 ¥1/M/小时 数字在当前官方页不存在**
   （已全文检索页面 DOM，仅别处模型价格出现 ¥1 字样；「限时免费」无 tooltip/脚注标注原价）。
3. **spec 已有条目确认**：GLM-5.2 国内 ¥8/¥2/¥28 ✓；GLM-4.7-Flash 国内**全免费**（输入/输出/缓存存储/命中四列均免）✓。
4. 全系人民币价目（输入/输出/缓存命中，缓存存储全部限免）：

| 模型 | 档位 | 输入 | 输出 | 缓存命中 |
|---|---|---|---|---|
| GLM-5.3（新品） | 1M 统一 | ¥8 | ¥28 | ¥2 |
| GLM-5.2 | 1M 统一 | ¥8 | ¥28 | ¥2 |
| GLM-5.1 | [0,32K) / [32K+) | ¥6 / ¥8 | ¥24 / ¥28 | ¥1.3 / ¥2 |
| GLM-5-Turbo | [0,32K) / [32K+) | ¥5 / ¥7 | ¥22 / ¥26 | ¥1.2 / ¥1.8 |
| GLM-5 | [0,32K) / [32K+) | ¥4 / ¥6 | ¥18 / ¥22 | ¥1 / ¥1.5 |
| GLM-4.7 | 三档（输入×输出长度） | ¥2–4 | ¥8–16 | ¥0.4–0.8 |
| GLM-4.7-FlashX | 200K | ¥0.5 | ¥3 | ¥0.1 |
| GLM-4.7-Flash | 200K | 免费 | 免费 | 免费 |

5. **推演影响**：国内缓存命中折扣 = 2/8 = **25%**，与国际站 18.6%（$0.26/$1.4）不同——
   §4.4 的注入成本公式与 `semantix usage` 节省金额标定必须 per-endpoint 配置价目，不能共用一套折扣率。
   spec 推演一「GLM 缓存折扣浅」在国内站同样成立（25% vs DeepSeek ~3.3%）。
6. **新增风险项**：「缓存存储按 ¥/M/小时计费」的维度在价目表结构中真实存在（当前全系限免）——
   限免结束后，长 TTL/大前缀的缓存驻留会产生**按时长计费**的新成本轴，届时 keep-alive 类对策的
   成本模型要把存储费计入（进 spec §10 风险表）。

## 5. Spike-5 免费 flash 档数据条款（完成）

**方法**：headless Chromium 核对国际站与国内站条款原文（2026-08-20），关键条款截图留证
（会话产物 `spike5-zai-api-terms.png`）。

**国际站（api.z.ai，运营主体 JINGSHENG HENGXING TECHNOLOGY PTE.LTD，Terms of Use
Last Update: April 14, 2026，https://docs.z.ai/legal-agreement/terms-of-use）**：

- 正文 IV：*"For enterprises and developers using API Services, we will not use your User Content
  for developing or improving Services unless you explicitly agree to such use."*
- Additional Terms for API Services §3(b)：*"We will only use End User Content as necessary to
  provide you with the API Services, comply with applicable law, enforce our policies, and prevent
  abuse. [We will not use End User Content to develop or improve Services, unless you explicitly
  agree to such use.]"*
- **API Services 条款未按模型/价格档位区分——免费模型（GLM-4.7-Flash）无训练例外条款**。
- 注意方向相反的另一段：**个人用户（individual users，即 chat 产品）默认授权**将非个人数据的
  User Content 用于「developing and improving our machine learning and artificial intelligence
  technologies」。引用条款时不可混淆两类主体。
- 附注两条：①Additional Terms §2 指向 *Data Processing Addendum for API Services*，页面无直链，
  本次未核验 DPA 文本（残留缺口，法务复核项）；②§1(f)(iii)(iv) 禁止医疗/金融/法律等特定资质场景与
  「decision-making activities」——语境针对面向个人的重大决策，切片 judge 属内部工程流程，
  判定不在禁止之列（留法务复核标注）。

**国内站（open.bigmodel.cn，用户协议与隐私政策 https://docs.bigmodel.cn/cn/terms/user-agreement、
/cn/terms/privacy-policy）**：

- 协议与隐私政策一致保留：对内容匿名化处理后「（**包括使用匿名数据进行机器学习或模型算法训练**）……
  此类处理后的数据的使用**无需另行征得您的同意**」。
- 「免费服务」专章仅三条（定义/可调整可终止/使用即同意），**无免费档专属数据条款**——
  免费与付费共用上述匿名化训练条款；**全文未见「API 输入不用于训练」的排除性承诺**。
- 另：匿名化针对的是个人信息维度；用户**代码片段的商业秘密属性不因匿名化而消除**，
  条款风险对 coding 场景实质存在。

**判定（回答 spec §6 的 Spike-5 问题）**：

| 端点 | 可否用于切片 judge（用户代码片段外发） | 依据 |
|---|---|---|
| 国际 api.z.ai · GLM-4.7-Flash（免费） | **可行** | API 用户默认不训练，无免费档例外；sanitize 管线仍保留（深度防御 + DPA 未核验前的兜底） |
| 国内 open.bigmodel.cn · GLM-4.7-Flash（免费） | **不建议** | 匿名化训练默认授权 + 无 API 排除承诺；企业协议另签前不外发代码片段 |

## 6. spec §3 回写清单（待 spec 分支吸收）

spec 本体在 `claude/semantix-agent-integration` 分支（未合 main），本报告不直接改 spec 文件；
以下为逐条修正，供该分支吸收：

1. **§3.1 国内价目行**（原 reported）改为，置信度 **official（2026-08-20 亲验）**：
   > 国内 bigmodel：GLM-5.3/5.2 = ¥8 / ¥2（缓存命中）/ ¥28，1M ctx **非阶梯**；5.1/5-Turbo/5/4.7 按输入长度阶梯；
   > 4.7-Flash 全免费；**缓存存储列（¥/M/小时）全系「限时免费」**；
   > 旧传闻「命中限免、存储 ¥1/M/h」两列颠倒不实；国内命中折扣 25%（对照国际站 18.6%）。
2. **§3.1 末行新增**：缓存存储按 token-小时计费的维度在国内价目表结构中已存在（现限免）——
   限免结束构成新的成本轴（联动 §10 风险表新增一行）。
3. **§4.4 注入成本公式**：0.814 折扣常数改为 per-endpoint 参数（国际 0.814 / 国内 0.75），
   与「所有经济学参数配置化」的 §10 缓解措施对齐。
4. **§4.3 免费离线档**：judge/评估作业限定**国际 api.z.ai 端点**；§6 安全一节的 Spike-5 待核项
   替换为本报告 §5 判定（国际可行/国内不建议 + DPA 残留缺口）。
5. **§3.3 新增两行托管栈实测**（置信度 measured，本报告 §1–§3/§3A/§3B）：
   > 腾讯云 MaaS→GLM-5.3：严格前缀、**全有或全无**（首 token/中段 25%/82%/单空格差异均清零）；
   > 命中报告天花板 ~97.6%；**TTL ∈ (8,12] 分钟**（原子过期）；种下 ~30s 内不可见、1/8 前缀持续不可见；
   > Anthropic 路径命中才透出 `cache_read_input_tokens`（增量语义）；thinking 参数体系与 OpenAI 路径不互通。
   >
   > AtomClub→GLM-5.2：严格前缀但**块级部分命中**（中段差异保留改动点前 ~48%）；
   > **TTL ~5 分钟已剩 28%**（似块级 LRU）；Anthropic 路径恒含 cache 字段族。
   >
   > 跨栈不变量仅：严格前缀、首 token 敏感、cache_control 透传不报错；
   > TTL/粒度/usage 形态全部 per-栈标定（§3B）——「按 provider 标定」为实测硬需求。
6. **§4.1-2（thinking 回传纪律）补充**：Anthropic 兼容路径强制显式 `thinking: {type: enabled, budget_tokens}`
   且不接受 `reasoning_effort`——sched 低延迟路径的「reasoning_effort=low」映射要 per-路径翻译。
7. **§4.2 usage 解析适配**：per-provider 细化为 **per-provider × per-路径**——同一托管栈的 OpenAI 路径
   （恒有 `prompt_tokens_details.cached_tokens`、prompt 全量）与 Anthropic 路径（命中才有
   `cache_read_input_tokens`、input 增量）字段形态不同；对账公式 `cache_read + input = 全量` 已实测成立。
8. **§4.1（gateway 净化中间件）**：cache_control 剥离改为 per-provider 配置项——腾讯云栈实测透传安全，
   Z.AI 官方端点复测前不默认剥离。
9. **§9 验收标准**：P0 的「命中率 ≥90%」按 §3 结论加脚注——遥测天花板 ~97.6% 且存在前缀级本底丢失，
   验收线不得设 >95%。
10. **§8 Spike 清单**：4/5 完成；1/2/3 在腾讯云托管栈完成（回答 §3.3/§3.4 线），
    Z.AI 官方端点复测项保留（脚本就绪）。

## 7. 残留缺口

1. **Z.AI 官方端点复测**：`api.z.ai` OpenAI 风格与 `/api/anthropic` 路径的同款实验（TTL/边界/字段形态）——
   本次 key 为腾讯云 MaaS，覆盖不到官方端点；脚本 `glm_spike.py` 换 env 即可复跑；
2. Z.AI *Data Processing Addendum for API Services* 文本未核验（法务复核项）；
3. 国内「限时免费」无截止日期标注——季度复核（spec §10 已有机制）时重查两列价目；
4. **0.5min 前缀持续不可见的机制未定**（种、30s 探、17min 复测三连 0，其余 7/8 正常）——
   疑多实例路由不一致；需扩大样本定量「本底丢失率」后再决定是否进遥测告警模型；
5. 腾讯云 MaaS 的 GLM-5.3 计费口径（含缓存命中价）未核——本报告价目数据仅覆盖 Z.AI 国际站与 bigmodel 国内站。

---

*方法与产物：/browse headless Chromium 人工核对（截图×2 留证）；受控 API 实验脚本 `glm_spike.py`/`spike1b.py`/`spike3b.py`（含预算护栏）与逐请求 usage JSONL（`results/spike{1,2,3}.jsonl`）为会话产物；被测端点 `tokenhub.tencentmaas.com`（腾讯云 MaaS 托管 GLM-5.3，key 由用户提供）与 `api.atomclub.cn`（→GLM-5.2，§3A 并行会话实测）；两把实验 key 均经会话传递，实验完成后建议轮换；本报告为 issue #233 的产出。*
