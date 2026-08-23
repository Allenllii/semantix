# Spec v1 — L2 注入预算口径：单遍构造根治 + 死格式串清理（Issue #283）

> 判级：Spec-Exempt（小修，issue 原判）。本 spec 记录缺陷核查结论与
> 修法。基线：当前 main（cc3bda6，含 #278 注入侧净化改造）。

## 1. 缺陷核查（真源审计，2026-08-23）

issue 描述的「预算用未转义长度」在**当前 main 已不存在**——#278 的
注入侧改造（`kernel/inject/inject.go` 第一遍净化+转义后判定）顺带修复：

| issue 描述（旧代码） | 当前 main 实际 |
|---|---|
| 第一遍按**原文**写 buffer、用原文长度判定预算 | 第一遍已 `escapeMarker` 后按转义长度判定（inject.go:149-157） |
| 第二遍转义重建 → 实际字节可超 Budget | 第二遍格式（`--- slice %s ---\n`）**短于**第一遍判定格式（`--- slice %s (score=%.2f) ---\n`，约 -12 字节/切片），且判定含 `blockClose+64` 余量 → 数学上最终块 < Budget |

**已实测**：含 20 个 `[/semantix-reuse]` 字面量的切片（单/多切片，
块 ~3.7KB）注入后 `len(block) < 4096` ✓。

## 2. 仍存在的缺陷与隐患

1. **死格式串**：第一遍的 `(score=%.2f)` 从不出现在输出（buffer 被
   `Reset` 丢弃）——issue 指出的误导读者代码；
2. **两遍构造的隐性耦合**：预算判定口径（第一遍格式）与实际落盘格式
   （第二遍）不一致，靠「第二遍恰好更短」维持上界——若未来第二遍
   格式变长（如恢复 score 输出），超预算 bug 会**复活**且无测试拦截；
3. **无回归测试**：`len(block) <= Budget` 的强上界无任何测试锁定
   （issue 验收要求的测试不存在）。

## 3. 修法（单遍构造，issue 建议 2 原样）

```go
// 收集阶段（不写 buffer）：过滤 → 净化 → 转义，用**实际落盘格式**
// 计算每个候选的大小并累计做预算判定；top slice 强制保留语义不变。
type injectCandidate struct {
    sl      *slice.Slice
    content string // 净化 + 转义后，与落盘字节一致
}
var cands []injectCandidate
size := len(blockOpen)
for _, h := range hits {
    // ...现有过滤（MinScore/Zones/空净化）不变...
    item := len(fmt.Sprintf("--- slice %s ---\n%s\n", h.Slice.ID, content))
    if size+item+len(blockClose)+64 > budget && len(cands) > 0 {
        dropped++
        continue
    }
    size += item
    cands = append(cands, ...)
}
// 排序（canonical ID 序，字节稳定规则不变）后单遍写。
```

- **输出与现状逐字节一致**：落盘格式串相同、canonical 排序相同、
  净化/转义逻辑不动（注入集字节稳定性规则不受影响）；
- **预算强上界**：最终块 = `size ≤ budget - len(blockClose) - 64 < budget`
  ——判定口径 == 落盘口径，结构性消除超预算（含未来格式变长的
  复活路径）；
- **保留决策变化范围**：判定口径从「第一遍格式+64」变为「实际格式
  +64」，仅在接近预算边界（块 ≥ ~4000 字节）的会话可能多保留 1 个
  切片——正常会话（块远小于预算）逐字节不变（L1 前缀稳定）；
  `(score=%.2f)` 死格式串删除；
- top slice 例外（首个候选不检查预算，K>=1 时总保留）语义保留，
  文档化：单切片内容超过 Budget 时块可超预算（现状即如此，非本次
  引入）。

## 4. 验收标准

- [ ] 回归测试：多切片含 marker 字面量 → `len(block) <= Budget`
      （现实现下即绿，作为锁定）；
- [ ] 边界测试：构造接近 Budget 的候选集，断言最终块 ≤ Budget 且
      top slice 保留语义不变；
- [ ] 现有注入测试全绿（canonical 输出、转义、#278 净化、#259
      per-type 等——输出字节不变）；`go vet`、`git diff --check`。

## 5. 参考

- Issue #283（缺陷描述与建议修法）；
- `kernel/inject/inject.go`（两遍结构 → 单遍）。
