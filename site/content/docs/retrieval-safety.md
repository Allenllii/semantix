# 检索、分区与复用安全

相关性回答“像不像”，安全复用回答“在当前状态下能不能直接用”。Semantix 把这两个判断分开。

## 混合检索

BM25 擅长精确术语，向量检索擅长语义改写。混合检索通过 weighted 或 RRF 融合两路排名，以减少单一路径的盲区。

融合后仍只是候选排序。高分不证明文件状态、依赖版本或用户意图没有改变。

## 三分区

`kernel/zone` 把候选分为 hit、grey、miss：

- hit：相对和绝对证据都达到较高门槛。
- grey：存在相关性，但证据不足以自动复用。
- miss：不进入复用路径。

三分区的价值是把不确定性显式保留下来，而不是强迫每个候选得到 yes/no。

## L3 的附加门

1. 依赖指纹检查文件状态是否仍一致。
2. 规则门拒绝明显不安全候选。
3. 配置了 judge 时，只对需要判断的候选请求模型。
4. 批准后的结果才进入 promote 流程。

## Fail-open 的含义

Semantix 的 fail-open 是“优化层失败时恢复正常执行”，不是“安全检查失败时仍使用缓存”。读取失败、候选不确定或 judge 不可用时，coding agent 应继续原任务，只是失去这次加速。

## 调参原则

降低阈值通常会提高表面命中率，也可能提高错误复用。应在带人工相关性标签的数据上校准，再观察 false positive、grey traffic 和最终任务结果。

## 对应仓库来源

- `kernel/fuse/`、`kernel/zone/`
- `kernel/fingerprint/`、`kernel/judge/`、`kernel/promote/`
- `docs/specs/issue-261-l3-freshness.md`
- `docs/specs/issue-262-l3-negative-observability.md`
