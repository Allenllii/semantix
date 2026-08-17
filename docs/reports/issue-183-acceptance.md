# Issue #183 验收报告 — GW3: 网关部署产物三件套 + /healthz 上游探活

> 状态：验收通过（2026-08-17）。对应 Issue：`#183 GW3: 网关部署产物三件套 + /healthz 上游探活`（Spec-Exempt，落实已批准 spec §5 文字方案 + §3.2 healthz 既有条目；`[server] health_timeout_seconds` 为 issue 批准的轻微契约扩展）。
> 设计真源：`docs/specs/newapi-gateway-design.md`（§0.3 / §3.2 / §3.8 / §3.9 / §5）；实现规格 = issue #183 上的 spec post（评论 `#issuecomment-5312589588`，用户审后批准）。
> 验收方式：实现 + 单测/e2e + 独立 subagent 审查（review）+ 缺陷修复后复验。

## 1. 验收对象

| 项 | 值 |
|---|---|
| 范围 | `deploy/`（3 文件新增）+ `.dockerignore`（新增）+ `gateway/server.go`（`handleHealth` 探活）+ `gateway/gateway.go`（`healthProbe` 注入 + `probeUpstreams`）+ `gateway/config.go`（`health_timeout_seconds`）+ `gateway/config_test.go` + `gateway/e2e_test.go`（TestE2EHealth*）+ `docs/specs/newapi-gateway-design.md`（§0 回写）+ `docs/QUICKSTART.md`（网关部署节） |
| 未提交 | 工作区未提交（待用户确认后提交） |

## 2. Issue checklist 逐条核对

| # | checklist（issue #183 原文） | 状态 | 证据 |
|---|---|---|---|
| 1 | `deploy/semantix-gateway.toml.example`：全部小节 + `${VAR}` 引用示范（上游 key 只走环境变量） | ✅ | `deploy/semantix-gateway.toml.example`（server/store/retrieval/cache/ingest/upstreams 全小节；`${SEMANTIX_GATEWAY_KEY}`/`${DEEPSEEK_API_KEY}` 示范；dead 键如实标注 GW6）；`TestDeployConfigExampleLoads` 门禁（env 注入后 `Load` 解析通过） |
| 2 | `deploy/Dockerfile`：多阶段构建 `cmd/semantix-gateway`，非 root 运行 | ✅ | 多阶段（golang:1.26-alpine → alpine:3.20 + ca-certificates），`USER 65532:65532`，`/data` 于 USER 前 chown（legacy builder 兼容） |
| 3 | `deploy/docker-compose.yml`：New API + semantix-gateway 两服务，网络与 healthcheck 接好 | ✅ | 两服务 + 内部网络（gateway `expose` 不发布端口）+ 双 healthcheck（gateway 引用 `/healthz`）+ named volume 持久化（非 root 可写） |
| 4 | `/healthz` 增加上游可达性探测（可配超时；探测失败返回 503 + 原因），compose healthcheck 引用 | ✅ | `probeUpstreams`（GET `{base_url}/models`，2xx=健康）；`[server] health_timeout_seconds`（默认 3，0=禁用）；失败 503 + OpenAI envelope（`upstream_error` + 上游名 + 原因，不泄漏 URL/Key）；`TestE2EHealthProbe*` 5 用例 |
| 5 | `docs/specs/newapi-gateway-design.md` §0 状态行回写（部署产物已存在） | ✅ | §0.1 healthz 行 + §0.3 两行（healthz 探活已实现 / deploy/ 已存在）回写 |
| 6 | README 或 docs 补一节「网关部署」指引（三条命令能起来） | ✅ | `docs/QUICKSTART.md`「网关部署（Docker）」节（三条命令 + 验证 + New API 渠道配置） |

## 3. 端到端实测（httptest 假上游，gateway 包）

| 场景 | 断言 | 结果 |
|---|---|---|
| 探活成功 | `/healthz` 200 + `{"status":"ok"}`；上游收到 1 次 GET /models | ✅ |
| 上游不可达 | 503 + envelope `upstream_error` + `unreachable`；响应不泄漏上游 URL | ✅ |
| 多上游其一失败 | 503，body 点名失败上游 | ✅ |
| 探活超时 | `health_timeout_seconds=1` + 慢 probe → 503 + `deadline exceeded`，1s 内返回 | ✅ |
| 探活禁用（0） | 200 + ok，上游零流量 | ✅ |
| toml.example 门禁 | env 注入后 `Load` 解析成功，关键字段（key/超时/路径/vendor）匹配契约 | ✅ |
| 既有回归 | gateway 全量 + 全仓 18 包 `-count=1` 全绿；`go vet` 无警告 | ✅ |

## 4. 独立 subagent 审查与修复

审查结论：核心逻辑正确，无阻断；2 应修 + 2 nit，应修已全部处理。

| 发现 | 分级 | 处置 |
|---|---|---|
| `probeUpstreams` 用 `%w` 透传 `*url.Error`，完整 BaseURL 会进 503 响应（与"不泄漏 URL"承诺矛盾） | 应修 | 改 `errors.As` 取底层 cause 只留原因；测试补 `!strings.Contains(out, deadURL)` 断言防回归 |
| Dockerfile `USER 65532` 在 `WORKDIR /data` 之前：legacy builder 下 `/data` 为 root:root，named volume 填充后 65532 不可写 → crash loop | 应修 | USER 前加 `mkdir -p /data && chown 65532:65532 /data` |
| `timeout > 0 && g.healthProbe != nil` 分支中 nil probe 静默回 200 | nit | 保留（`New()` 已保证非 nil，防御性检查无实际危害） |
| `TestE2EHealthProbeDisabled` 未真正断言零上游流量 | nit | 改用活上游 + `callCount()==0` 断言 |

复验：修复后 `go vet ./gateway/...` + 全仓 `go test ./... -count=1`（18 包）全绿。

## 5. 验证命令

```bash
go build ./...                    # ✅
go vet ./gateway/...              # ✅ 无警告
go test ./gateway/... -count=1    # ✅ 全部通过（含 6 个新 GW3 用例）
go test ./... -count=1            # ✅ 18 包全绿
git diff --check                  # ✅
go test ./gateway/... -race       # ⚠️ 本机无 CGO（gcc 不存在）不可运行，CI 承担
docker compose -f deploy/docker-compose.yml config   # ⚠️ 本机无 docker compose 插件未验证（文件为静态 YAML，语法经 review 核对）
```

## 6. 未闭合风险与后续边界

| 项 | 说明 |
|---|---|
| compose/Dockerfile 未实机验证 | 本机无 Docker daemon；文件经静态 review + toml 门禁测试验证，实机 `docker compose up` 留待 GW4 真实全链路验收时执行（正是本条产物的用途） |
| 镜像基础偏离 spec §5 文字方案 | alpine（CA 证书 + healthcheck 工具需要）而非 scratch/distroless，取舍已写入 `deploy/Dockerfile` 注释并经 spec post 审阅 |
| `-race` 未在本机验证 | 无 CGO 环境限制（项目既有记录），CI 承担 |
| 上游超时配置化未落地 | §3.8 的 connect 10s / 首字节 60s 仍是设计意图，不在本条范围；探活超时已独立实现 |
| dead 配置键（retriever/judge_api_key） | GW6 #186 处理，example 已如实标注 |
| GW4 #184 | M0 门（真实全链路 + 部署产物）依赖本条 |
