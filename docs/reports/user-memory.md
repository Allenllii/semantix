# 用户级记忆（办案场景）适配方案

> 需求：记住用户偏好、办案流程、总结办案经验，跨会话自动复用。
> 结论：**semantix kernel 核心机制已覆盖**，做办案领域适配即可落地。

## 能力映射（已实测验证）

| 需求 | semantix 能力 | 验证结果 |
|---|---|---|
| 记住用户偏好 | `extract --scope user`：偏好声明切片入库（user 级库 `~/.semantix/user.db`，跨项目共享） | ✅ "我办案喜欢先用模板" 检索 hit（score 0.96） |
| 办案流程 | ToolPattern/Prompt 切片：流程步骤入库；`inject --scope user` 下次办案自动带出 | ✅ "处理交通事故案件" 注入块含流程切片 |
| 总结办案经验 | 经验沉淀切片 + `judge` 验证（复用前 LLM 确认相关，防过时经验误用） | ✅ 两级验证链（指纹闸 + judge） |
| 跨会话复用 | `inject`（系统提示注入复用块）+ `semantix_lookup` 工具（检索） | ✅ 已接入 harness（H1 fork 挂载） |

## 办案场景落地路径

```
用户会话（Semantix fork，HarnessSink 自动旁路）
  → semantix extract --scope user --fingerprint <案卷/法条文件>
  → user.db（~/.semantix/user.db，跨项目）
  → 下次办案：inject --scope user → 偏好 + 相关流程 + 经验自动注入系统提示
  → grey 区候选走 judge（LLM 确认"该经验适用本案"）→ 防过时/不适用经验
  → waste++ 观测（经验复用被拒次数 → 调 extractor 粒度/阈值）
```

## 办案领域适配项（待做）

1. **领域 rubric**：judge prompt 六维表扩展法律维度——法条引用有效性、程序合规（时效/管辖）、当事人信息脱敏
2. **经验切片提取器**：会话末尾的"总结/经验"段落 → 高价值切片（当前是 turn 级 P/T/R 通用提取）
3. **偏好文件指纹**：`--fingerprint` 采集案卷模板/常用文书路径，变更即失效（防复用旧模板）
4. **脱敏基线**：user.db 0600 + 注入块标记区域（已具备）；建议 judge 拒绝含敏感个人信息切片（rubric 扩展）

## 下一步

先用真实办案会话试跑：`semantix extract --input <会话.jsonl> --scope user --project <案件类别>`，
再 `semantix verify --session <目录> --scope user --judge-protocol <协议> --judge-base-url ... --judge-model ...` 评估命中率。
