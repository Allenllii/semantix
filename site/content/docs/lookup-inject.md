# Lookup 与上下文注入

`lookup` 和 `inject` 使用同一检索基础，但输出契约不同。前者供程序判断，后者供 prompt 组装。

## 什么时候使用 lookup

```bash
semantix lookup --query "如何处理这个迁移" --scope project --json
```

`lookup` 返回结构化候选、分数、zone 与来源，适合注册成 agent tool。调用方可以决定是否继续读取证据、是否请用户确认，或是否完全忽略命中。

不要把 lookup 命中直接当作工具执行指令。历史切片属于低权限数据，不能覆盖当前用户请求或仓库规则。

## 什么时候使用 inject

```bash
semantix inject \
  --query "如何处理这个迁移" \
  --scope project \
  --budget 4096 \
  --k 5
```

`inject` 在预算内选择完整切片，并输出有明确边界的 `[semantix-reuse]` 块。选择完整切片而不是任意截断，有助于避免把条件和结论拆开。

## 预算与 top-k

- `--k` 控制最多考察多少个候选。
- `--budget` 控制最终注入块的字节预算。
- 候选多并不代表上下文更好；噪声会挤占当前任务所需的 token。

## 推荐接线

1. 用 lookup 获取候选与 zone。
2. hit 候选仍要进行任务相关性检查。
3. 需要把内容交给模型时再调用 inject。
4. 把注入块放在明确的历史材料区域，不与宿主 system prompt 混为一体。
5. 记录最终是否采用，为后续反馈和验证提供依据。

## 对应仓库来源

- `kernel/lookup/lookup.go`
- `kernel/inject/`
- `agent-skill/tools/semantix-lookup.md`
- `harness/tool/semantix.go`
