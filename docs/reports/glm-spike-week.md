# GLM Spike 周报告：semantix-glm-optimization spec 的 5 个事实缺口

> 对应 Issue：#233 · spec：`docs/specs/semantix-glm-optimization.md`（harness-integration 分支草案）§8
> 判级：Spec-Exempt（E2 调研/报告，不改代码）
> 状态（2026-08-20）：**Spike-4/5 完成（浏览器人工核，headless Chromium 渲染后读取并截图留证）；
> Spike-1/2/3 实验脚本就绪、被 Z.AI API key 阻塞**（本机环境与常见变量名下均无 key；api.z.ai 连通性已验证，401）
> 方法：Spike-4/5 走 /browse headless 浏览器逐页核对原文；Spike-1/2/3 为受控 API 实验（脚本见 §1-§3，预算护栏 ≤40 请求 / max_tokens≤64，估算成本 < $0.5）

---

## 0. 一页结论

| Spike | 状态 | 一句话结论 |
|---|---|---|
| 1 TTL/命中率曲线 | ⏸ 待 key | 脚本就绪：8 个独立前缀×间隔（0.5–20 min）各测一次，避免探测刷新效应 |
| 2 Anthropic 兼容端点 | ⏸ 待 key | 脚本就绪：4 请求测 usage 字段形态 + cache_control 容忍度（system 块/消息块两位置） |
| 3 「高度相似」边界 | ⏸ 待 key | 脚本就绪：6 请求（种缓存/同前缀/中段差异/首 token 扰动/空格级差异/全量重发） |
| 4 国内价目核对 | ✅ 完成 | **传闻口径两列颠倒**：官方页为「缓存存储＝限时免费、缓存命中＝¥2/M」；GLM-5.3 **非阶梯计价**（¥8/¥28，1M ctx） |
| 5 免费档数据条款 | ✅ 完成 | **国际 api.z.ai API 默认不训练（无免费档例外）→ judge 可行；国内 bigmodel 保留匿名化训练权利 → 不建议** |

对 spec 的直接影响（详见 §6 回写清单）：
1. §3.1 国内价目行从 reported 升 official，且**内容要改**（两口径均不实）；
2. 「缓存存储按 ¥/M/小时计费」的维度在官方价目表**真实存在**、现全系限免——限免结束是新增成本风险项（进 §10）；
3. §4.3 免费 flash 档跑 judge 获得条款依据，但**必须限定走国际 api.z.ai 端点**，不走国内 bigmodel 免费端点。

---

## 1. Spike-1 缓存 TTL/命中率实测（待 key）

**目标**：GLM 隐式缓存 TTL 未公布（官方仅称「合理的时效性」，第三方称均值 3–5 分钟），
拟合「turn 间隔 → 命中率」经验曲线，决定 §4.5 是否需要 TTL 对策。

**设计**（`glm_spike.py spike1`，脚本随会话产物归档）：
- 每个间隔一个**独立前缀**（~3000 tokens，run id 打头隔离历史缓存），t=0 全部种缓存，
  各自等 Δt ∈ {0.5, 1, 2, 3, 5, 8, 12, 20} 分钟后**只探测一次**——
  规避「探测本身刷新缓存」对曲线的污染（这是与朴素连续探测设计的关键差异）;
- 度量：`usage.prompt_tokens_details.cached_tokens / prompt_tokens`，记录实际间隔与延迟；
- 模型 glm-5.3（spec 经济学地基所在档位；缓存行为可能因模型而异，不用免费档代测）。

**判定标准**：若 5 分钟点命中率已显著衰减（<50% 前缀命中），则多轮 agent 会话的长思考/长工具等待
（coding agent 常见 >5 min turn 间隔）将系统性丢缓存，§4.5 的 TTL 对策需要提前；
若 12–20 分钟仍高命中，则「不预设 keep-alive ping」的现行决策成立。

## 2. Spike-2 Anthropic 兼容端点行为（待 key）

**目标**：`api.z.ai/api/anthropic` 的 usage 是否返回 `cache_read_input_tokens` 类字段；
对 Anthropic 风格 `cache_control` 块是忽略还是报错——决定 gateway 对 Claude Code 类 harness 的透传策略。

**设计**（`glm_spike.py spike2`，4 请求）：
1. 无 cache_control 基线（长 system 前缀）→ 记录 usage 完整字段形态；
2. 同前缀立即重发 → 隐式缓存是否以 Anthropic 风格字段（`cache_read_input_tokens`）或
   OpenAI 风格（`prompt_tokens_details.cached_tokens`）透出，还是完全不透出；
3. system 块携带 `cache_control: {type: ephemeral}` → 200（忽略）还是 400（报错）；
4. 消息内容块携带 cache_control → 同上（两个位置行为可能不同）。

**gateway 决策矩阵**：报错→净化中间件必须剥离 cache_control；忽略→可透传（低风险）；
字段透出形态决定 §4.2 usage 适配器对该端点走哪套解析。

## 3. Spike-3 「高度相似」边界（待 key）

**目标**：官方缓存文档称自动识别「相同**或高度相似**的内容」——验证 GLM 隐式缓存是否存在
非严格前缀命中（若有，L1 字节稳定纪律的收益模型与 CacheBlend 类演进的紧迫度都要重估）。

**设计**（`glm_spike.py spike3`，6 请求，~3000 token 前缀 P，请求间隔 4s）：

| # | 变体 | 判定 |
|---|---|---|
| r0 | P + 尾A（种缓存） | 基线 |
| r1 | P + 尾B | 前缀命中基线：期望 cached≈len(P) |
| r2 | P 中段（Section 0015）改一句 + 尾B | cached 只到改动点前→严格前缀；仍≈len(P)→存在非前缀/相似命中 |
| r3 | 首字符加 "X" + 尾B | cached=0→严格从 token 0（同 DeepSeek）；>0→有滑动容错 |
| r4 | 中段多一个空格 + 尾B | 官方「细微格式差异可能影响缓存」的空格级敏感度实测 |
| r5 | 与 r1 完全相同重发 | 全量命中上界（含尾部） |

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
5. **§8 Spike-4/5** 标记完成，指向本报告；Spike-1/2/3 补充「脚本就绪、待 key」状态。

## 7. 残留缺口

1. Spike-1/2/3 需一个 Z.AI API key（国际站）——脚本与预算护栏就绪，跑完即可回填 §1–§3；
2. Z.AI *Data Processing Addendum for API Services* 文本未核验（法务复核项）；
3. 国内「限时免费」无截止日期标注——季度复核（spec §10 已有机制）时重查两列价目。

---

*方法与产物：/browse headless Chromium 人工核对（截图×2 留证）；实验脚本 `glm_spike.py`（含预算护栏）随会话产物归档；本报告为 issue #233 的阶段产出。*
