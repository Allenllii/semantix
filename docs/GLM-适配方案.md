# Semantix × GLM 适配方案

> 面向智谱 GLM 生态的技术介绍。Semantix 是一个**跨会话复用 / 记忆内核**,给 Claude Code、DeepSeek‑Reasonix 这类 AI 编程助手加装一层"越用越省、越用越懂你"的能力。本文说明我们对 **GLM(GLM‑5.x)** 做了哪些一等公民适配,以及为什么这套复用机制**恰好放大 GLM 自身的缓存经济学**。
>
> 一句话:**你做过一次的事,不再花第二次钱——而且不牺牲回答质量。** 对 GLM 用户,这意味着把智谱隐式缓存的命中折扣从"单会话内"延伸到"跨会话"。

---

## 1. Semantix 是什么

Semantix 坐落在 **编程助手(harness)** 与 **模型 provider** 之间,不改助手、不依赖厂商出新 API:

- **语义切片库**:从历史会话提取可复用单元(任务模板 / 上下文知识 / 工具序列 / 可复用结果),持久化到本地库;
- **三级语义缓存**:把"语义相似但字节不同"的跨会话请求,转化为模型侧**前缀缓存的真实命中**;
- **自适应调度 / 投机预取**:按任务意图编排并发、模型档位与预取,等待期预取只读资源。

它是独立内核(`kernel/`),对接任意 harness;GLM 只需作为一个 provider 接入即可享受全部能力。

---

## 2. 对 GLM 的适配(已在 v0.7.0 落地)

### 2.1 GLM 是一等 provider,不是"能连上就行"

端到端接线,不是占位:

| 能力 | 说明 | 代码位置 |
|---|---|---|
| **端点自动识别** | `open.bigmodel.cn`(国内)与 `api.z.ai`(海外)均识别为 Zhipu GLM 栈 | `harness/provider/openai/host.go: IsZhipu` |
| **思考开关按 GLM 语义** | GLM 通过 `thinking.type`(`enabled`/`disabled`)门控思维链、**静默忽略 `reasoning_effort`**;Semantix 的 `/effort` 正确映射到 `thinking.type`,不会把无效的 `reasoning_effort` 发给 GLM | `harness/provider/openai/openai.go`(zhipu 分支) |
| **保留思考回传纪律** | GLM Coding Plan / Anthropic 兼容端点要求 `reasoning_content` 完整回传;注入层不触碰 reasoning 序列,避免破坏缓存 | provider + 注入层 |
| **全系型号** | `glm-5.2 / glm-5.1 / glm-5 / glm-5-turbo`,视觉 `glm-5v-turbo`,以及 `glm-4.7 / 4.7-flash / 4.7-flashx / 4.6 / 4.5 / 4.5-air / 4.5-flash` | `harness/config/provider_presets.go: glmAPIModels` |
| **多形态端点** | OpenAI 兼容(bigmodel.cn / z.ai)、Coding Plan、以及 **Anthropic Messages 兼容**(`glm-5.2[1m]` 等) | `provider_presets.go` |

### 2.2 开箱默认 GLM,一键可切,任意模型都能用

- **全新安装默认 `default_model = "glm"`**(指向 `open.bigmodel.cn`,读 `GLM_API_KEY`);
- 无配置首启时按环境变量自动择优(`GLM_API_KEY` → 海外 `ZAI_API_KEY` → 其它),避免"没 key 卡首启";
- **BYOK**:任何 OpenAI / Anthropic 协议兼容端点,填 `base_url + api_key_env` 即可用——**即使某模型没有做专门适配,也能用上基础功能**;
- 想换模型只改一行 `default_model`,或用 `semantix-agent setup` 向导。

### 2.3 缓存卫生 + 命中遥测(让 GLM 隐式缓存真正命中)

GLM 的隐式缓存是**严格前缀**:前缀有一个字节不同,命中收益即清零。Semantix 为此专门做了:

- **前缀净化中间件**(默认开启):剥离会污染前缀的计费 / attribution 标记行、透传 `cache_control`,保证发往 GLM 的请求前缀**字节稳定**,从而稳定命中隐式缓存(`gateway/config.go: SanitizeConfig`);
- **前缀固定序**:对时间戳 / 工具枚举 / MCP 列表等易抖动部分做确定性排序,消除同义请求间的字节漂移(harness 前缀审计);
- **per‑provider 命中遥测**:把 GLM 端点的缓存命中字段(Anthropic 风格 `cache_read_input_tokens` / OpenAI 风格 `prompt_tokens_details.cached_tokens`)统一归一,`semantix usage` 直接显示**命中率与节省金额**(`gateway/anthropic.go`、`cmd/semantix/usage.go`)。

---

## 3. 为什么 Semantix 对 GLM 特别有价值

GLM 官方定价里,**缓存命中价 ≈ 标价的 1/4 上下**(如 GLM‑5.2 命中约 25%)。也就是说:**命中越多,GLM 越便宜**。但厂商侧前缀缓存只在"字节完全相同、且在 TTL 窗口内"时命中——单会话内有效,一旦跨会话或稍有措辞差异就失效。

Semantix 补上的正是这一段:

```
两个会话,任务语义相同但字节不同:
  "跑一下 Go 测试"   vs   "确保 Go 测试全过"
        └──────────── 普通前缀缓存视为无关,重新计费 ────────────┘

Semantix:检索出同一个规范切片 → 按固定顺序原样注入前缀区
        └──── 语义命中 → 转化为 GLM 前缀缓存的真实命中 ────┘
```

三级协同:

```
L3 · 验证后结果复用   只读任务带指纹验证,直接复用,不调用 GLM
L2 · 语义切片注入     语义相似 → 前缀字节稳定 → 喂养 L1
L1 · GLM 隐式前缀缓存  相同前缀字节的被动复用(智谱侧)
```

**语义层喂养字节层**——不改编程助手、不依赖智谱出新接口,就把 GLM 的命中折扣从会话内延伸到跨会话。

---

## 4. 实测

我们在 **GLM 兼容端点**上做了对照实测(完整报告:[`docs/reports/glm-spike-week.md`](./reports/glm-spike-week.md)):

- **严格前缀已证实**:无"相似 / 非前缀"命中,首 token 差异即零命中——前缀卫生的价值由此坐实;
- **跨会话前缀缓存二次命中 96–99%**(命中报告天花板约 97.6%,尾部不足块不计入);
- **Anthropic Messages 兼容端点**真实工作,`cache_control` 可透传,usage 命中字段可对账;
- 端到端验证:同一 GLM 会话的第二次相似请求,`cached_tokens` 覆盖约 **99%** 输入(实测 5760 / 5797)。

> 诚实标注:上述数字为**云端 / 网关托管端点**实测;官方 `bigmodel.cn` / `api.z.ai` 直连的同款脚本已就绪,拿到 key 即可复测。缓存机制(严格前缀 + 命中折扣)是 GLM 的固有属性,Semantix 的前缀稳定化对官方端点同样适用。

---

## 5. 快速上手(GLM)

```bash
# 1) 下载 v0.7.0 发布包(四平台),解包
tar -xzf semantix-agent-v0.7.0-<platform>.tar.gz
cd semantix-agent-v0.7.0-<platform>

# 2) 用默认 GLM 配置(已指向 bigmodel.cn)
cp semantix-agent.example.toml ~/.semantix/semantix-agent.toml
export GLM_API_KEY=<你的智谱 key>          # 海外:改用 ZAI_API_KEY 并把 default_model 设为 zai

# 3) 开跑:CLI 交互 或 本地 Web GUI
./semantix-agent                            # TUI
./semantix-agent web                        # 浏览器 GUI

# 4) 看跨会话复用带来的命中与省钱
./semantix usage
```

海外 Z.AI:把 provider 的 `base_url` 换成 `https://api.z.ai/api/paas/v4`、`api_key_env = "ZAI_API_KEY"` 即可。

---

## 6. 支持的 GLM 型号

| 类别 | 型号 |
|---|---|
| 主力(OpenAI 兼容) | `glm-5.2` · `glm-5.1` · `glm-5` · `glm-5-turbo` |
| 视觉 | `glm-5v-turbo` |
| 4.x 系 | `glm-4.7` · `glm-4.7-flash` · `glm-4.7-flashx` · `glm-4.6` · `glm-4.5` · `glm-4.5-air` · `glm-4.5-flash` |
| Anthropic Messages 兼容 | `glm-5.2[1m]` · `glm-5.2` · `glm-5.1` · `glm-5` · `glm-4.7` · `glm-4.5-air` |

---

## 7. 延伸阅读

- 项目总览:[`README.md`](../README.md) / [`README.zh-CN.md`](../README.zh-CN.md)
- GLM 缓存实测:[`docs/reports/glm-spike-week.md`](./reports/glm-spike-week.md)
- 架构与安全:[`docs/总体架构-流程树.md`](./总体架构-流程树.md) / [`docs/Security-安全设计.md`](./Security-安全设计.md)

---

*Semantix 与 GLM 的关系是互补:GLM 提供强模型与命中折扣,Semantix 把这份折扣延伸到跨会话、并保证复用质量。欢迎智谱生态的开发者接入试用与反馈。*
