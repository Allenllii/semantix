# 提取与检索

`extract` 负责把会话转换成可复用切片，`search` 负责按当前查询排序这些切片。两者共享同一切片库，但承担不同责任。

## 输入格式

会话文件是 JSONL：一行一个 JSON 对象。典型字段包括 `role`、`content` 和 `tool_calls`。先用少量脱敏会话验证格式，再批量导入。

```bash
semantix extract \
  --input session.jsonl \
  --db .semantix/project.db \
  --scope project \
  --project demo
```

提取器会按会话内容生成不同类型的语义切片。它不是把整份 transcript 原样塞进数据库；上下文提取和压缩均有独立测试约束。

## 三种检索模式

```bash
semantix search --query "失败测试的修复步骤" --retriever bm25
semantix search --query "失败测试的修复步骤" --retriever vector
semantix search --query "失败测试的修复步骤" --retriever hybrid --fusion rrf
```

- `bm25`：依赖词面匹配，容易解释，适合命令、文件名和错误文本。
- `vector`：使用 embedding 相似度，适合同义改写。
- `hybrid`：组合两路结果；支持 weighted 或 RRF 融合。

默认值以当前配置和 `semantix search --help` 为准，不要只依赖旧文章里的参数截图。

## 读取结果

结果除了分数，还会带来源和 zone。zone 用于表达“当前证据有多强”，不是内容权限：

- `hit`：检索证据较强，仍需检查任务上下文。
- `grey`：存在相关性，但应验证后再用。
- `miss`：不应注入或复用。

自动化集成建议使用 `--json`，避免解析面向人的颜色和排版输出。

## 排查无结果

1. 确认 extract 与 search 指向同一个 `--db`。
2. 确认作用域一致。
3. 用会话中的原始术语先测 BM25。
4. 用 `doctor` 检查数据库和配置。
5. 不要为了制造 hit 盲目降低阈值；先检查输入质量。

## 对应仓库来源

- `kernel/ingest/`、`kernel/slice/`
- `kernel/bm25/`、`kernel/embed/`、`kernel/fuse/`
- `cmd/semantix/extract.go`、`search.go`
