# Spec — Issue #331 anthropic / responses 适配器 honor `Request.EffortOverride`

## 1. 目标

`provider.Request.EffortOverride`（`harness/provider/provider.go:225`）是文档化的逐请求推理深度通道，但目前只有 OpenAI 家族适配器读取它（`harness/provider/openai/effort.go:79-85`）。anthropic 与 responses 适配器在构造时冻结 effort，静默丢弃该字段。本 spec 将参考实现移植到这两个适配器，并让各自的自动输出预算随 effective effort 重新推导。扩展协议（extension-hosted providers）的 `effortOverride` 字段属插件边界 wire 变更，**明确不在本 issue 范围**（另开 issue）。

## 2. 词汇表（构造时计算，非逐请求）

两个适配器各新增 `requestEfforts []string` 字段，在 `New()` 中填充；空值 = 忽略 override。

- **anthropic**（`harness/provider/anthropic/anthropic.go`）：
  - DeepSeek 路径：`low|high|max`（匹配 `:473-475` 的 wire 白名单）；`thinking=="disabled"` 或 `effort=="disabled"` 时为 nil（该路径不发射 `output_config`）。
  - 原生 adaptive 路径（`thinking=="adaptive"`）：`low|medium|high|xhigh|max`（完整产品阶梯，与现有 raw 转发一致）。
  - `thinking` 为 `enabled|disabled` 或空：nil（该路径从不发射 `output_config`）。
- **responses**（`harness/provider/responses/responses.go`）：
  - `vendor=="deepseek"`：模型含 `flash` 时 `low|high|max`，否则 `high|max`（与 openai 适配器的 DeepSeek 官方词汇一致）。
  - 其他 vendor：`low|medium|high`（OpenAI Responses 官方 reasoning effort 阶梯）。

## 3. 逐请求 gate

新增 `func (c *client) requestEffort(req provider.Request) string`，镜像 `harness/provider/openai/effort.go:79-85`：词汇表内的小写化 override 胜出，其余情况原样返回 `c.effort`。**只 gate override，绝不 gate 构造的 effort**——现有的 `medium`/`xhigh` raw 转发行为保持不变。

## 4. anthropic wire 替换点

`buildRequest` 内用 `effective := c.requestEffort(req)` 替换以下位置的 `c.effort`（仅限这些点）：

- `:467`（DeepSeek thinking toggle 判断）
- `:472`（`normalizeDeepSeekAnthropicEffort(c.model, effective)`，归一化保持）
- `:482-483`（adaptive 路径 raw 转发）
- `:487-488`（enabled/disabled 路径 thinking toggle）

**不得替换**：`:188-190` `deepSeekThinkingEnabled()`（能力谓词，位于 buildRequest 之外）、`:225-237` `MissingToolCallReasoningWarningIdentity()`（稳定诊断身份，需保持 per-client 稳定）。

## 5. anthropic 自动输出预算

新增 `autoMaxOutput bool` 字段（`defaultMaxTokens` 旁），在 `New()` 的 `if maxOutputTokens <= 0` 块（`:112-130`）内置 true，经 composite literal（`:135-155`）接线。`buildRequest` 的 `:442-448` 按 openai `:782-793` 模式重新推导：

- `autoMaxOutput && deepseek`：`provider.AutoOutputBudget(reasoningOn, effective)`，其中 `reasoningOn` 由 `c.thinking != "disabled"` 且 effective 非 disabled/off/none 决定；
- `autoMaxOutput` 且 thinking 为 `adaptive|enabled`（原生路径）：`provider.AutoOutputBudget(true, effective)`；
- 其余情况回落到 `c.defaultMaxTokens`。

配置的（非 auto）预算不受 override 影响。

## 6. responses wire 替换点

- `:303`：effort 归一化改用 `effective := c.requestEffort(req)`；
- `:319`：`responsesAutoOutputBudget(c.vendor, effective)`（仅当 budget 为 auto 且 vendor 为 deepseek/mimo 时）。

## 7. 测试计划

1. 新文件 `harness/provider/anthropic/effort_override_test.go` 与 `harness/provider/responses/effort_override_test.go`，镜像 `openai/effort_override_test.go` 的五个契约：
   - 词汇表内 override 生效（anthropic：adaptive + effort low + override max → `output_config.effort=="max"`）；
   - 词汇表外 override（DeepSeek 路径 "medium"）被忽略、回落到构造的 `c.effort`，绝不 raw 转发；
   - 空 `EffortOverride` 字节级一致：`json.Marshal` 请求体与改动前 golden 比对；
   - 无词汇表（enabled/disabled thinking、无深度端点）时 override 不发明 wire 字段。
2. 扩展两个 `output_budget_test.go`：auto budget 随 effective 重新推导的 case；配置（非 auto）预算不受 override 影响的 case。
3. 现有 guard 测试扩展 override 参数：非 DeepSeek `enabled`/`disabled` 不发射 `output_config`（`anthropic_test.go:643-644`）；DeepSeek 空 Thinking 仍发射 `output_config.effort`（`anthropic_test.go:765-767`）。

## 8. 兼容性

空 `EffortOverride` 时 wire body 字节级不变；不新增配置、CLI flag、依赖；无 i18n 文案变更（`messages_*.go` 与 `catalog_parity_test.go` 不动）。扩展协议（`harness/extension/protocol/dto_provider.go`、`harness/sdk/go/types_generated.go`）不在本 spec 范围。

## 9. 验收命令

```sh
go build ./...
go vet ./...
go test ./harness/provider/... -race
```
