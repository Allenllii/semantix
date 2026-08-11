# Semantix 客户交付材料（一页纸 + 附件清单）

> 状态：v0.2.0 可交付（2026-08-11）。面向客户的接入说明与收益口径；所有数字均有出处（见文末）。

## 一句话

**Semantix 是一个架在 agent harness 之上的"跨会话记忆内核"**：记住你的 agent 每次办案/开发/运维怎么做，下次遇到同类任务直接把"上次的做法"注入给模型——省掉重复的思考、检索和执行成本。

## 架构（两层，叠加）

```
┌──────────────────────────────────────────────┐
│ 单会话前缀缓存（模型服务商基础设施，自动生效）     │
│  └ 我们的注入块放固定位置 + 字节稳定 → 被它吸收    │
│     增量成本 ≈ 0                               │
├──────────────────────────────────────────────┤
│ 跨会话记忆内核（Semantix，我们的核心）            │
│  会话 → 切片提取（偏好/流程/经验）→ user.db      │
│  新任务 → 检索命中 → 注入 → 模型直接复用          │
└──────────────────────────────────────────────┘
```

- **单会话层**：字节稳定前缀（24 条工程纪律，继承自 Reasonix）——命中率 90%+（长会话稳态）
- **跨会话层**：我们的增量——命中率以真实数据验证为准（目标 ≥70%）

## 接入方式（三选一，全部为 skill 包 / 文档）

| 模式 | 适用 | 工作量 |
|---|---|---|
| A. agent skill 自助接入 | 任何框架（Reasonix/Claude Code/自定义） | 安装脚本 + 配置（≤1 小时） |
| B. LangChain 中间件 | 已有 LangChain 应用 | 两个挂点（改写+提取），示例代码现成 |
| C. harness fork | 深度集成（Reasonix 用户） | 事件级旁路 + 注入 hook（已实现） |

## 收益口径（对外只能引这些）

| 数字 | 口径 | 出处 |
|---|---|---|
| **90%+** 单会话缓存命中 | 长会话稳态（官方口径） | reasonix-kvcache-mechanisms.md |
| **71%** 单会话缓存命中 | 独立任务冷会话实测（49 次） | 同上 |
| **79.8%** 成本节省 | 跨会话复用演示实测（合成数据） | m0-cost-comparison.md |
| **≥70%** 跨会话命中目标 | 真实数据验证门槛（未达先调提取粒度） | m0-gate.md |
| **24h TTL** 前缀缓存 | DeepSeek 厂商缓存策略（记忆本体持久存储，无 TTL） | reasonix-kvcache-mechanisms.md:39 |

**禁用口径**："99%+"——全仓库无出处，不对客户引用。

## 风险与限制（诚实声明）

1. 跨会话收益依赖真实数据验证（M1 后进行；未验证前以演示数据为准）
2. 冷会话（全新任务无历史）没有收益——收益随使用时长累积
3. 敏感数据（办案内容）默认本地存储（0600），不经过我们的服务器

## 附件清单

- `agent-skill/`（SKILL.md + install.sh + selftest.sh + 工具定义 + 配置模板）
- `docs/reports/cache-taxonomy.md`（两种缓存对照）
- `docs/reports/reasonix-kvcache-mechanisms.md`（单会话机制，24 条纪律）
- `docs/reports/langchain-middleware.md`（模式 B 示例）
- `docs/reports/user-memory.md`（领域适配：偏好/流程/经验）
- `docs/reports/harness-refactor-blueprint.md`（模式 C 战略）

## 路线图

- M1：embed 语义化（压缩+进化）、真实数据验证（跨会话命中率）
- M2：LLM judge 两级验证（已实现接口，待接后端）
- 里程碑：命中率 ≥70% 后发布 v0.3（跨会话正式能力）

## 验证状态（2026-08-11）

- fork 守卫测试全 PASS：`TestBuildComposesByteStableSystemPrompt` + `TestCacheHitPrefixStable/ClimbsWithoutCompaction/SurvivesTooSmallWindow`（挂载后单会话基线完好）
- 10 包 race 测试全绿；`semantix` CLI v0.2.0 已发布（6 平台二进制）
