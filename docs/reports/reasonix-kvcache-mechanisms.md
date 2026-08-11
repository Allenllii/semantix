# Reasonix 单会话 KV Cache 机制研究（2026-08-11）

来源：DeepSeek-Reasonix main-v2 快照（本机 `/Users/song/.reasonix/global-workspace/DeepSeek-Reasonix`）源码级调查，全部结论带 file:line 证据。

## 一句话结论

Reasonix 的缓存优化**不是魔法，是一套"前缀字节稳定"工程纪律**（24 条机制、6 组）：让模型服务商可见的请求前缀（system prompt + 工具 schema + 历史消息）在轮次间**逐字节不变**，动态内容一律放消息尾部，压缩被设计成"罕见的缓存重置点"。

## 机制 6 组

### ① 静态在前、动态在后（前缀可复用）
- system prompt 在 boot 一次性组装且不再变：base prompt → 风格 → 策略 → workspace → token 模式 → 环境段 → 持久记忆 → skills 索引（`internal/boot/boot.go:547-640`）
- 测试断言 "Base must come first so it stays a valid cache prefix when memory changes"（`boot_test.go:137-139`）
- boot 级字节稳定性守卫：两次 Build 必须逐字节相同，否则失败（`prompt_stability_test.go:18-59`）
- 会话中新增内容（记忆更新/任务完成/hook/召回）全部注入**当前用户消息尾部**，绝不改 system prompt（`internal/control/input.go:174-214`）
- 环境探测结果 24h 持久化快照，防 timeout/PATH 抖动重写前缀（注释：10x miss 价）（`internal/environment/snapshot.go:16-26`）

### ② 字节稳定序列化（请求体逐字节一致）
- 工具 schema 排序归一化 + SHA-256 指纹，ToolsHash 跨轮稳定（`internal/agent/cache_shape.go:51-64`）
- 请求字段 omitempty 纪律：ResponseFormat/name/prefix 用指针+omitempty，普通请求保持与历史完全相同的字节（`internal/provider/openai/openai.go:1217-1240`）
- 剥离 CreatedAt（UI 元数据）置 0，墙钟差异不失效缓存；freezeProviderRequest 深拷贝，流重试 replay 相同 payload（`internal/agent/sampling_request.go:233-244,292-334`）

### ③ append-only 重发全部历史（命中来源）
- 每轮请求字节 = 上一轮完整请求 + 新尾部："On request i the cached prefix should be the ENTIRE request i-1"（`cachehit_e2e_test.go:169-176`）
- 不做内容去重——历史完整重发正是前缀命中来源
- 跨进程 resume 永不改写历史/工具结果/transcript，只记录 warm/cold/unknown 缓存状态（`internal/control/controller.go:3449-3482`）
- 普通保存 append-only 事件日志；SaveRewrite 仅压缩等明确场景（`internal/agent/save.go:192-215`）

### ④ 压缩 = 罕见的缓存重置点（保护命中率）
- 软阈值 0.5 只通知、0.6 snip 陈旧工具结果、0.8 压缩、0.9 强制（`internal/agent/compact.go:23-43`）
- canonical transcript（事实源）与模型可见投影分离；缓存状态只参与成本与观测，不触发历史改写（`docs/research/cache-aware-compaction-design.md:18-24`）
- 压缩折叠顺序固定：system → 早期轮次逐字提升 → 单条滚动摘要 → 保留消息 → recent tail（`docs/SPEC.md:290-299`）
- 窗口过小 stuck guard：停止重写同一前缀让缓存恢复，尾部命中率 ≥85%（`cachehit_e2e_test.go:228-272`）

### ⑤ Provider 差异
- Anthropic 原生端点：`cache_control:{type:"ephemeral"}` 断点 ≤2 个（system 尾 + 最后消息尾），省略 ttl 字段保字节稳定（`internal/provider/anthropic/anthropic.go:414-435,801-807`）
- **DeepSeek：刻意不发 cache_control**，依赖自动前缀缓存；测试断言 wire 上无任何 cache_control（`anthropic_test.go:678-684`）
- OpenAI 兼容端点：无显式标记，靠 prompt_cache_hit_tokens 计费 + 字节稳定
- vendor-aware 缓存 TTL：DeepSeek 24h / DashScope 5m / Anthropic 5m（`internal/config/cache_policy.go:21-45`）

### ⑥ CI 守卫（把命中率当产品行为）
- cachehit_e2e 测试：mock 前缀缓存（按字节共同前缀推导），断言率爬过 90%（`cachehit_e2e_test.go:196`）
- CompareShape 归因：对比上/本轮 system+tools+内容，输出 miss 原因（`cache_shape.go:74-99`）
- 缓存敏感路径强制 Cache-impact/Cache-guard 标注；发布级守卫阈值默认 90%（`CONTRIBUTING.md:99-119`）

## 命中率数字口径（重要——防止对外说过头）

| 数字 | 出处 | 口径 |
|---|---|---|
| **90%+** | docs/index.html:7 | 官方营销：长会话稳态；输入成本降至 ~1/5 |
| **~90%** | docs/SPEC.md:884-885 | 27 个 delegate 子运行实测均价（~90% 命中下 ¥0.017/子） |
| **71%** | benchmarks/README.md:195 | e2e 49 次独立冷会话实测（含首请求必 miss） |
| **≥90%** | cachehit_e2e_test.go:196 | mock 前缀缓存测试断言 |
| **≥85%** | cachehit_e2e_test.go:268 | 窗口过小时 stuck guard 保护后下限 |
| **99%+** | **全仓库无出处**（仅 CSS/SVG 色值与延迟分位数） | 不可引用 |

## 对我们的含义

1. **单会话优化 = fork 继承**：24 条机制全部在 fork（Gnosil/DeepSeek-Reasonix）里现成——"把确定的做了"的最短路径就是**用 fork 跑单会话**，90%+ 基线直接继承（守卫测试同步继承）。
2. **我们的注入块设计已被这套体系覆盖**：固定位置（system prompt 尾/消息尾）+ 字节稳定（ID 规范序、原文存储）≡ 机制①④ 的要求；实证：同查询两次 inject 89 字节完全一致。
3. **我们只赚跨会话**：单会话 90%+ 是 fork 继承的（省单价），跨会话命中（省数量）才是我们的增量——实验性推进，M0-Gate 门槛不变（真实数据 ≥70%）。
4. **99%+ 不可用于对外材料**：官方口径 90%+、冷会话实测 71%；若客户问"99%"，如实引用 90%+（长会话稳态）+ 71%（独立任务实测）两个口径。

## 待办（fork 侧验证，非阻塞）
- 挂载（U7/U8）后跑 fork 的 cachehit_e2e + prompt_stability 守卫测试，确认我们的注入/hook 不破坏字节稳定性
- CompareShape 归因已可观测：注入块导致的 miss 会显示在缓存诊断里（后续可做"注入不改前缀"断言）
