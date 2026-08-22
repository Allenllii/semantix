# 验证、用量与健康检查

Semantix 把“能运行”“能命中”“值得复用”分成不同检查。不要用单次成功搜索代替完整验收。

## verify：离线回放

```bash
semantix verify --session ./sessions --project demo --scope project
```

`verify` 把部分会话作为历史库，按时间回放剩余会话，并报告命中和 zone 分布。需要把门禁用于 CI 时，再启用 `--strict`。

```bash
semantix verify --session ./sessions --strict --calibrate
```

`--calibrate` 输出分桶分布；提供人工标签后才能进一步估计 precision。没有标签时，不应把相似度分数解释成正确率。

## usage：估算成本

```bash
semantix usage --db .semantix/usage.jsonl
```

usage 根据日志中的 token/cache 事件和配置价目计算基线、实际成本与估算节省。价目是输入参数；报告不会自动核对供应商账单。

## dashboard：一屏状态

```bash
semantix dashboard
```

dashboard 汇总切片库和用量日志，适合人工快速检查，不应作为机器稳定接口。自动化请使用各命令的 `--json` 输出。

## doctor：运行前诊断

```bash
semantix doctor
semantix doctor --json
```

doctor 检查配置、切片库以及已配置的外部能力。某些网络检查只有在配置端点后才执行；未配置不等于远端已通过。

## 一套可执行验收顺序

1. `doctor` 无关键失败。
2. 最小 extract/search/inject 闭环成立。
3. `verify` 使用真实、脱敏的多会话数据。
4. 人工抽查 hit 与 grey 候选。
5. 对照供应商账单核验 usage 估算。

## 对应仓库来源

- `cmd/semantix/verify.go`、`usage.go`、`dashboard.go`、`doctor.go`
- `docs/reports/verify-rubric.md`
- `docs/reports/m0-gate.md`
