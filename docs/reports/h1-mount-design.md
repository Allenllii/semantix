# H1 挂载设计（fork: Gnosil/DeepSeek-Reasonix @ main-v2）

> 目标：kernel 与 harness 的首次闭环——harness 运行中的事件旁路进 kernel 切片库，
> 运行时可调用 kernel 的检索/注入能力。kernel 保持独立 module，fork 通过
> **子进程协议**（`semantix` CLI）调用 kernel，零代码耦合。

## 挂载点（已侦察确认）

| 功能 | fork 位置 | 方式 |
|---|---|---|
| U7 事件旁路 | `internal/event.Sink` 接口 | 新增 `internal/semantix/sink.go`：`HarnessSink` 实现 Sink，turn 级组装 kernel 兼容 JSONL（`{role,content,tool_calls}`）写 `.semantix/sessions/<session>.jsonl` |
| U8a L2 注入 | `internal/agent/agent.go:2641 systemPrompt()` | config 开关 `semantix.inject` 开启时，systemPrompt 末尾调 `semantix inject --query <首条用户消息>` 插入 `[semantix-reuse]` 块 |
| U8b lookup 工具 | `internal/tool/tool.go:250 RegisterBuiltin` | 新增 `internal/tool/semantix.go`：`semantix_lookup` 工具（schema: query/limit），`semantix lookup --json` 子进程调用 |
| 配置 | `reasonix.example.toml` | `[semantix] enabled/inject/db/binary` 段 |

## 事件映射（fork Sink → kernel 会话 JSONL）

```
TurnStarted  → 打开新 turn，重置 buffer
Text/Reasoning → 追加 assistant content
ToolDispatch → 记录 tool_calls[{id,name,arguments}]
ToolResult   → 追加 tool 行 {type:"tool", tool_call_id, name, content: output}
TurnDone     → flush：user 行（来自 session 首条）+ assistant 行（content+tool_calls）+ tool 行
```

输出格式 = `semantix extract --input` 原生支持的会话 JSONL（每行一个 JSON 对象）。

## 调用协议

- `semantix` 二进制路径：config `semantix.binary`（默认 `semantix`，PATH 查找）
- lookup 工具：`semantix lookup --query <q> --limit N --db <db> --json` → stdout JSON
- inject：`semantix inject --query <q> --budget N --db <db>` → stdout 注入块文本
- 失败策略：子进程失败/超时（3s）→ 静默降级（无注入块 / lookup 返回空）——kernel 故障不影响 harness 主流程

## 实施顺序

1. `internal/semantix/sink.go`（HarnessSink）+ `internal/semantix/inject.go`（子进程封装）
2. `internal/tool/semantix.go`（semantix_lookup 工具注册）
3. `internal/config` 加 `[semantix]` 段 + `cmd/reasonix` main 装配 HarnessSink
4. `systemPrompt()` 注入 hook（config 开关）
5. fork 内 `go build ./...` + `go test ./internal/semantix/...` + 冒烟（fake turn → JSONL 断言）
6. 推送 fork（网络恢复后）+ Issue #24 同步

## 风险

- 子进程调用 kernel 有 ~50ms 启动开销（Go 二进制）——注入/检索都在 turn 边界，可接受
- fork 后续升级（upstream main-v2）可能改 Sink 接口——HarnessSink 只依赖稳定子集（TurnStarted/Text/Reasoning/ToolDispatch/ToolResult/TurnDone）
- 本环境网络限制：github.com 直连不稳 → 推送/CI 用 gh api / goproxy.cn 旁路
