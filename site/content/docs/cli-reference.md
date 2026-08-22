# CLI 命令索引

运行 `semantix help` 查看当前版本命令树，运行 `semantix <command> --help` 查看完整 flags。下面按任务分类，不复制容易过期的全量参数表。

## 建库与复用

| 命令 | 主要用途 |
|---|---|
| `extract` | 从会话 JSONL 生成切片并写入库 |
| `search` | 使用 BM25、向量或混合策略检索 |
| `lookup` | 为 agent tool 返回结构化候选 |
| `inject` | 在预算内生成 L2 复用块 |

## 评估与观测

| 命令 | 主要用途 |
|---|---|
| `verify` | 按时间回放会话并评估命中 |
| `eval` | 比较检索阈值策略 |
| `eval-judge` | 在人工 oracle 样本上评估 judge |
| `calibrate` | 汇总 judge 与运行时负向信号 |
| `usage` | 汇总 token、cache 和估算成本 |
| `dashboard` | 输出面向人的状态面板 |

## 安装与维护

| 命令 | 主要用途 |
|---|---|
| `install` | 安装或卸载 agent skill |
| `doctor` | 检查本地配置与依赖状态 |
| `gc` | 重算价值并归档超限切片 |
| `version` | 输出构建版本 |

## JSON 输出契约

支持 `--json` 的命令使用统一信封：

```json
{"ok":true,"command":"search","data":{},"error":null,"version":"..."}
```

自动化应检查 `ok` 和进程退出码，不应只判断 stdout 是否非空。

## 退出码

| 退出码 | 含义 |
|---|---|
| `0` | 成功 |
| `1` | 运行错误，例如 IO 或检索失败 |
| `2` | 用法错误，例如未知 flag 或非法配置 |
| `3` | 门禁未达标，例如 strict 验证失败 |

## 对应仓库来源

- `cmd/semantix/main.go`
- `cmd/semantix/contract.go`
- `cmd/semantix/envelope.go`
- `docs/reports/cli-v2-architecture.md`
