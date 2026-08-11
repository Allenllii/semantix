# L3 语义缓存引入「灰色地带」三段阈值（hit / grey / miss）

## 背景与动机

当前 L3 语义结果复用（`docs/Agent-Infra-架构设计.md` §4.3）使用**单一相似度阈值** τ' 判定命中：

> 意图指纹 + 输入指纹相似 ≥ τ'，且**可验证** → 命中

Krites（*Asynchronous Verified Semantic Caching for Tiered LLM Architectures*, Apple, arXiv:2602.13165）及其引用的 vCache 工作（arXiv:2502.03771）证明了这个设计的根本缺陷：

- **正确命中和错误命中的相似度分布大面积重叠**——embedding 几何距离在中间地带无法区分"换说法"（语义等价）与"换意图"（不相关）；
- 单一阈值只能二选一：阈值高 → 安全复用机会被浪费；阈值低 → 错误命中（Negative Hit）风险上升。这正是 GPTCache（NLP-OSS 2023）0.7 阈值权衡问题的延续。

## 现状问题

1. `τ'` 一刀切：灰色地带请求要么被拒（损失复用收益）、要么被放行（风险错命中）；
2. 阈值是静态的，不区分"明显等价 / 模糊 / 明显不等价"，把判定责任全部压在一个数字上；
3. 没有可观测性：三段流量占比未知，阈值失配无法被发现。

## 方案：两阈值三区域

```
相似度轴:  0 ─────── τ_low ──────── τ_high ─────── 1
区域:      [ 明确 miss ] [ 灰色地带 ] [ 明确 hit ]
```

| 区域 | 条件 | 行为 |
|---|---|---|
| **明确 hit** | 相似度 ≥ τ_high | 直接 L3 命中（仅限规则可验证项，见 Issue 2 两级验证） |
| **灰色地带** | τ_low ≤ 相似度 < τ_high | **不直接判**，入队异步验证（LLM judge，见 Issue 2），验证通过后提升 |
| **明确 miss** | 相似度 < τ_low | 直接 miss → 调后端 |

设计要点：

1. **τ_low 是"验证预算旋钮"**：调高 → 灰色地带变小、judge 调用变少、可恢复命中变少；调低 → 覆盖广、judge 成本上升。与 Krites §3.4 的成本模型一致；
2. **判定责任分级**：明确的交给规则（阈值），模糊的才交给昂贵的模型（judge）——关键路径零变化；
3. **三段流量占比必须可观测**（新增指标，见验收标准）——多一个阈值就多一个状态机，失配要能被发现；
4. 与 P4 预取器联动：灰色地带验证任务挂在预取等待期执行（见 Issue 2）。

## 验收标准

- [x] L3 命中/错误率不劣于当前单阈值基线（相同评估集）；【`semantix eval` oracle 对比，PASS：error 20%→0%，2026-08-11】
- [x] 新增观测指标：**灰色地带流量占比**（默认目标 ≤ 30%，超限告警）；【verify/eval --grey-target + WARN + --strict exit 3】
- [x] 明确 hit / grey / miss 三段流量分布可查询（P0 观测层仪表）；【verify/eval 汇总行 + lookup/search zone 标注】
- [x] τ_high、τ_low 可配置，且参数变更走注入集冻结期规则（§6.2），不影响已注入字节稳定性；【--tau-* 四参数 + validate；注入规范序 ID 升序保证字节稳定】
- [x] 在 semantix 评估集上对比单阈值与三段策略的命中率/错误率（oracle 评估法，见 Issue 2 参考）。【testdata/eval-greyzone.tsv 15 条 4 等价类，详见 docs/reports/issue-07-acceptance.md】

## 参考

- Krites：§3.1 Grey-zone trigger and task scheduling（arXiv:2602.13165）
- vCache：arXiv:2502.03771（相似度分布重叠的实证）
- GPTCache：NLP-OSS 2023（单阈值 0.7 权衡）
- `docs/Agent-Infra-架构设计.md`：§4.3（L3 现状）、§6.2（冻结期）、§11（验证指标）
