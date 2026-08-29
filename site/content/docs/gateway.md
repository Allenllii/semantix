# 部署 OpenAI 兼容 Gateway

Semantix Gateway 位于支持 OpenAI 兼容请求的客户端与上游模型之间，负责请求净化、检索/注入、L3 判定、SSE 转发和用量记录。

## 适用条件

- 客户端允许设置自定义 base URL。
- 你能够安全管理网关密钥和上游模型密钥。
- 你接受“优化失败时继续请求上游”的 fail-open 行为。

## Docker Compose 起步

```bash
cp deploy/semantix-gateway.toml.example deploy/semantix-gateway.toml
export SEMANTIX_GATEWAY_KEY="<gateway-key>"
export DEEPSEEK_API_KEY="<upstream-key>"
docker compose -f deploy/docker-compose.yml up -d --build
```

不要把示例占位符替换成真实密钥后提交到 Git。配置中的 `${VAR}` 在启动时从环境变量展开，缺失凭据会使启动失败。

## 健康检查

```bash
docker compose -f deploy/docker-compose.yml ps
curl http://127.0.0.1:3000/
```

具体端口和拓扑以 compose 文件为准。New API 管理面板与内部 Gateway 是两个组件，不要把面板端口误当成 Gateway 监听地址。

## 判断缓存命中

- 非流式响应：检查 `x-semantix-cache: hit | miss`。
- 流式响应：中间代理可能剥离自定义响应头，以 Gateway usage 日志为准。

```bash
tail -n 5 .semantix/gateway-usage.jsonl
semantix usage --db .semantix/gateway-usage.jsonl
```

## 当前边界

示例配置明确标注了尚未接线或需要额外配置的能力。例如 Gateway retrieval 段当前以实际实现注释为准，不应因为 CLI 支持 hybrid 就推断 Gateway 中同样全部生效。

## 对应仓库来源

- `gateway/`
- `cmd/semantix-gateway/`
- `deploy/docker-compose.yml`
- `deploy/semantix-gateway.toml.example`
- `docs/specs/newapi-gateway-design.md`
