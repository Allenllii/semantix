# GLM-P0-1：harness 前缀污染审计（Issue #289）

> 日期：2026-08-21 · 判级：P0 批次（spec `semantix-glm-optimization.md` §4.1-1，Spec-Required 已合 main）
> 背景：云厂商 GLM 栈实测严格前缀且「全有或全无」（腾讯云单空格差异即清零，
> `glm-spike-week.md` §3）——前缀内任何逐 turn 变化的字节都把 L1 命中率打到 0。
> 审计范围：自有 harness 的请求组装路径（`harness/agent` → `harness/provider`）。

## 结论一览

**四项 checklist 全部达标**：三项在既有代码中已有实现与测试锁定（DeepSeek 时代
cache-first 工程纪律的遗产），一项（工具数组的 wire 层字节稳定）本次补强 e2e 断言。
未发现逐 turn 变异字节进入请求前缀的漏洞。

| 审计项 | 结论 | 证据（代码 / 测试） |
|---|---|---|
| 时间戳/UUID/trace_id 移出前缀 | ✅ 达标 | 消息 `CreatedAt` 在请求组装时归零（`sampling_request.go` requestMessages 循环）；system prompt 无日期/时间嵌入（全文搜索无命中）；`UnixNano`/writerID 类仅用于内部标识（branch/fleet/task 落盘），不进请求体 |
| 工具数组序列化顺序稳定 | ✅ 达标 + 补强 | `Registry.Schemas()` 按名字典序输出（`tool.go` sort.Strings），与注册时序无关；既有测试 `TestRegistrySchemasSorted` / `StableAndCanonical` / `CanonicalizesEquivalentOrdering` 锁住性质；**本次新增** `TestCacheHitPrefixStable` 的 wire 层断言：连续请求的 `tools` 数组原始 JSON 逐字节相等 |
| MCP server/tool 列表顺序稳定 | ✅ 达标（见 tradeoff 1） | MCP 工具与内置工具同走 `Registry`，同一字典序导出；连接时序只影响集合出现时机，不影响既有元素顺序 |
| 系统提示逐 turn 字节一致回归测试 | ✅ 已有 | `TestCacheHitPrefixStable`：mock provider 逐字节对比连续请求的共同前缀，断言「请求 i 的缓存前缀 == 完整的请求 i-1」；配套 `TestCacheHitClimbsWithoutCompaction`（14 turn 命中率爬升曲线）与 `TestCacheHitSurvivesTooSmallWindow` |

## 前缀卫生的既有系统性设计（审计中确认）

审计发现该 harness 的 cache-stable prefix 纪律早已工程化，关键机制记录如下
（均有源码注释明示意图）：

1. **环境探测快照持久化**（`boot.go` envSection）：环境块进 provider 缓存前缀，
   探测结果落 `config.CacheDir()` 跨重启复用——瞬时探测抖动（超时、PATH 漂移）
   不重写前缀、不冷启动缓存；
2. **角色/执行策略走 per-turn transient 块**（`boot.go`）：planning/verification/review
   强度不注入 system prompt，走每 turn 的 `<execution-policy>` user 块——
   cache-stable 前缀跨角色共享；
3. **持久记忆一次性折叠**（`boot.go` memory.Compose）：REASONIX.md/AGENTS.md 层级
   在 boot 时折进前缀，mid-session 变化走 transient 注入、下会话才进前缀;
4. **L2 注入块 turn 内锁定**（`sampling_request.go`）：`[semantix-reuse]` 块每 turn
   组装一次、byte-stable，插入位置固定为「第一条 system 消息之后、历史之前」
   （`prependSystemBlock`），turn 内多 round 保持前缀命中；
5. **诊断层**（`cache_shape.go` PrefixShape/CacheDiagnostics）：system/tools 双 hash
   逐 turn 对比，miss 可归因（system 变 / tools 变 / compact/snip 重写）——
   #292 usage 面板的解释性数据源已就位。

## 设计内 tradeoff（记录，不修）

1. **MCP 动态注册的集合渐变**：MCP server 连接完成时其工具加入 Registry，
   下一请求的 tools 数组集合变化（字典序中间插入）→ 一次前缀断点。这是动态
   注册特性的固有代价，`schemaRev` 与 `syncResourceCatalog` 已追踪；断点为
   一次性（新集合随后稳定）。缓解属产品决策（如「MCP 全就绪再开 turn」），
   不在本 issue 范围。
2. **跨 turn 注入块变化 = system 段末尾断点**：L2 注入内容随检索结果跨 turn 变化时，
   前缀命中止于 system prompt 末尾。这是 L1×L2 的结构性张力，spec §4.6-7 已列
   CacheBlend 类非前缀复用为观察项；turn 内锁定纪律（上表 4）已把代价压到每 turn 一次。

## 低风险开放项

- **请求参数对缓存 key 的敏感性未实测**：`temperature`/`max_tokens`/`reasoning_effort`
  等参数字段不属 prompt 前缀，主流 provider 的隐式缓存 key 不含它们；但 GLM 云厂商栈
  未实测验证。若后续遥测（#292）出现「前缀 hash 未变但命中塌陷」的样本，
  优先排查此项（标定脚本 `glm_spike.py` 可加参数扰动变体）。

## 验收对照（issue #289）

- [x] 时间戳/UUID/trace_id：审计确认不进前缀（见上表）
- [x] 工具数组序稳定：既有排序 + 本次 wire 层字节断言
- [x] MCP 列表顺序稳定：同一排序机制覆盖
- [x] 系统提示逐 turn 一致回归测试：既有 `TestCacheHitPrefixStable` + 本次 tools 断言补强
- [x] `go vet ./... && go test ./... -race` 全绿（PR CI）
