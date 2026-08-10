# L3 复用验证 Rubric（代码场景）

> 对应 Issue #8（两级验证：指纹快速否决 + 异步 LLM judge 最终确认）。
> 论文基础：Krites（arXiv:2602.13165）二元判定函数 J(q, s, a)。
> 实现：`kernel/judge` 包（RuleGate + Judge 接口）。

## 两级验证链

```
灰色地带候选（zone.Grey，来自 Issue #7 三段分类）
  ├─ ① RuleGate（零成本、确定性、先过）
  │     · zone=hit        → Confirm（直接复用，不花 judge 钱）
  │     · zone=miss       → Reject
  │     · zone=grey       → NeedJudge（有 Judge）或 Reject（无 Judge，保守）
  ├─ ② Judge（异步 LLM，J(q,s,a)）
  │     · 模型批准 → Confirm（提升入缓存，下次直接命中）
  │     · 模型否决 → Reject（waste++，降信号源）
  └─ 执行位置：P4 预取器等待期（模型流式输出时 off-path 验证）
```

## 代码场景检查维度（judge prompt 指南）

| 维度 | 检查内容 | 判定 |
|---|---|---|
| **API 签名** | 缓存的答案引用的函数/方法签名是否仍匹配 | 签名变更 → 否决 |
| **文件路径** | 答案涉及的路径/模块是否存在于当前工作树 | 缺失 → 否决 |
| **依赖版本** | go.mod/package.json 中相关依赖版本是否一致 | 版本漂移 → 否决 |
| **语言/框架一致** | 答案语言/框架与问题上下文一致（`pkg/foo.go` 的修复不复用给 `pkg/bar.go`） | 不一致 → 否决 |
| **新鲜度** | 代码快照、构建产物、清单文件变更（规则层已覆盖，judge 可省） | 变更 → 否决 |
| **个性化** | 项目配置、用户偏好差异（由 scope 分层：project/user 双库） | 库分层处理 |

## 当前实现边界（诚实声明）

- `kernel/judge` 已落地：RuleGate 三段路由 + Judge 接口 + NoopJudge（无模型时 grey 保守 Reject）。
- **依赖指纹**（mtime/SHA）尚未接入切片元数据——规则层当前只用 zone 信号；指纹闸是下阶段（切片 Meta 扩展 `DepFingerprints`）的工作。
- LLM judge 实现（调用 provider 的 Confirm 逻辑 + 上述 rubric prompt）待模型后端接入（H2 后）。

## 验收（Issue #8 checklist）

- [x] 两级接口（RuleGate + Judge）落地，失败保守 Reject
- [x] 无 judge 时灰色地带保守处理，可观测（reason 输出）
- [ ] 指纹闸接入切片 Meta（依赖文件 mtime/SHA）
- [ ] LLM judge 实现 + rubric prompt 入库
- [ ] 验证收益统计（waste++ 观测）
