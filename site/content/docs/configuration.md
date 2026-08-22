# 配置与作用域

Semantix 可以零配置运行；当路径、检索方式或成本参数需要在团队内稳定复现时，再创建 `semantix.toml`。

## 从示例开始

```bash
cp semantix.example.toml semantix.toml
```

最常用的配置如下：

```toml
[project]
name = "my-project"

[store]
db = ".semantix/project.db"
scope = "project"
max_slices = 5000

[retrieval]
retriever = "hybrid"
limit = 5

[inject]
budget = 4096
top_k = 5
```

## 优先级

运行时按“内置默认值 → 配置文件 → 环境变量 → CLI flag”逐层覆盖。临时实验用 CLI flag；团队默认值放 TOML；凭据只放环境变量。

配置文件不存在时命令继续使用默认值。配置文件存在但语法或字段类型非法时，CLI 返回用法错误，避免静默采用错误参数。

## 三种作用域

| 作用域 | 适合存放 | 注意事项 |
|---|---|---|
| `session` | 单次会话临时信息 | 不应期待跨会话命中 |
| `project` | 仓库架构、命令、局部工作流 | 默认选择，避免跨项目污染 |
| `user` | 跨项目偏好与稳定流程 | 写入前应先做敏感信息治理 |

提取和检索必须使用相容作用域。最常见的“查不到”原因，是写入时使用 `project`，读取时却使用 `user`。

## 哪些字段会改变数据兼容性

- `store.db` 决定切片库文件。
- `retrieval.vector_dim` 会改变 hash embedding 维度；已有向量与新维度不一致时需要重新提取。
- `store.max_slices` 会在维护流程中触发上限淘汰与归档。
- `cost.*` 只影响估算，不会改变模型供应商的实际账单。

## 凭据边界

Judge、上游模型或网关密钥只使用环境变量。不要把真实 key 放入 TOML、命令行历史、会话 JSONL 或 Git。

## 对应仓库来源

- `semantix.example.toml`
- `kernel/config/config.go`
- `cmd/semantix/cfgload.go`
- `cmd/semantix/config_wiring_test.go`
