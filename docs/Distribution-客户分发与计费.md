# 客户分发：代码保护 + 免费额度计费

> 回答两个产品问题：
> ① CLI 能不能发给客户使用、同时让对方看不穿里面的实现？
> ② 怎么做"前 1000 万 token 免费，用完引导去我们 DS 平台充值"的自动化流程？

---

## 1. 现状评估：为什么旧发布版"能被穿透"

Go 编译产物是原生机器码，源码本身从不随包分发；`scripts/release/build.sh`
也已经带了 `-trimpath -ldflags "-s -w"`（去调试信息、去符号表、去绝对路径）。
**但这不够。** Go 运行时自带的 pclntab（函数名表）不受 `-s -w` 影响，实测对
v0.3.1 同参数构建的 `semantix` 二进制：

```console
$ go version -m semantix
    mod  semantix  v0.0.0-...-8c6cde7d88c2      # 精确 git revision
$ strings semantix | grep -c 'semantix/'
218                                             # 全部包路径 + 源文件名
$ strings semantix | grep kernel/judge
semantix/kernel/judge.(*LLMJudge).callAnthropic # 每个方法名都在
semantix/kernel/evolve.(*ewmaEngine).maybeAdjustLocked
...
```

即：客户拿 `strings`/redress/Ghidra 就能还原完整的包结构、算法命名、
内部提示词字符串——**架构等于裸奔**，只是拿不到逐行源码。

## 2. 保护版构建：`scripts/release/build-protected.sh`

面向客户分发一律用保护版脚本（内部/开源发布继续用原有 `build.sh`）：

```bash
# 全平台矩阵（darwin/linux/windows × amd64/arm64）
GO=/path/to/go1.26.5/bin/go scripts/release/build-protected.sh --version v0.3.2

# 只出 linux-amd64，附带网关
scripts/release/build-protected.sh --version v0.3.2 \
  --platforms linux-amd64 --binaries semantix,semantix-agent,semantix-gateway
```

做了什么：

| 层 | 手段 | 挡住什么 |
|---|---|---|
| 标识符 | [garble](https://github.com/burrowers/garble) 重写本模块+全部依赖的包路径/函数名为随机哈希（`-seed=random` 加盐） | `strings`/反编译器恢复架构与命名 |
| 字符串 | garble `-literals` 加密字面量 | 提示词、启发式规则、端点明文外泄 |
| 元数据 | garble `-tiny` + `-trimpath -buildvcs=false -ldflags "-s -w"` | `go version -m`、git revision、构建路径 |
| 验证 | 构建后自动泄露扫描：任何 `semantix/...` 包路径、哨兵内部符号（`ewmaEngine`/`LLMJudge`/...）、模块元数据可恢复即构建失败 | 混淆"静默失效" |
| 冒烟 | 宿主平台二进制实跑 `version`，注入版本号必须出现 | 混淆打断功能 |

产物：`dist-protected/semantix-<version>-<platform>-protected.tar.gz` + `SHA256SUMS.txt`。

**工具链注意**：garble 需要装在模块缓存之外的真实 Go 工具链（GOTOOLCHAIN
自动下载到 GOMODCACHE 的不行，脚本会预检并报错指引）。从 https://go.dev/dl/
装 go.mod 要求的版本，`GO=/path/bin/go` 传入即可；garble 缺失时脚本会自动
`go install` 固定版本（`GARBLE_VERSION` 可覆盖，默认 v0.17.0）。

**边界（必须诚实）**：原生二进制不存在绝对不可逆——混淆把逆向成本从
"跑一条 strings"抬高到"专业逆向工程"，但决心足够的人仍可分析机器码。因此：

* 任何密钥、价格逻辑、额度逻辑**不进客户端二进制**；
* 授权/额度的强制点放服务端（见下节），客户端只做展示；
* 保留 `-seed` 值（或用固定 seed）才能用 `garble reverse` 还原客户上报的
  panic 栈——发版时把 seed 记录到内部密管。

## 3. 免费额度：前 1000 万 token 免费 → 402 引导充值 → 到账自动解锁

强制点在 `semantix-gateway`（部署在**我们这一侧**，客户的 agent 以
`base_url` 指向它）。客户端无论怎么改、怎么绕，额度都在服务端计——这也是
上一节"客户端不做强制"的落地。

### 3.1 配置（`deploy/semantix-gateway.toml.example`）

```toml
[billing]
enabled = true
free_tokens = 10000000                    # 默认即 1000 万，0 = 用默认
recharge_url = "${SEMANTIX_RECHARGE_URL}" # DS 平台充值页
# 可选：平台钱包探测（DeepSeek GET /user/balance 兼容 schema）
balance_url = "https://your-platform.example.com/user/balance"
balance_key = "${SEMANTIX_PLATFORM_KEY}"
balance_cache_seconds = 300
```

### 3.2 流程（全自动，无人工介入）

```
客户请求 /v1/chat/completions
  │
  ├─ 免费额度未用完 ──────────────► 正常转发；响应头带
  │                                x-semantix-quota-{limit,used,remaining,mode}
  │
  ├─ 用完 + 平台钱包有余额(充过值) ─► 正常转发（mode=paid，探测结果缓存 5 分钟）
  │
  └─ 用完 + 无余额 ────────────────► 402 insufficient_quota / free_tier_exhausted
                                     中英双语消息 + 充值链接
                                     x-semantix-quota-recharge-url 头
```

* 402 消息体示例（harness 的 `APIError` 会原样把它显示在客户屏幕上，
  402 不重试、立即失败，客户看到的就是充值指引）：

  > free tier exhausted: 10000123 of 10000000 free tokens used. Top up at
  > https://platform.example/topup to continue — access unlocks automatically
  > once your platform balance is active. 免费额度已用完（已使用 10000123 /
  > 10000000 tokens）。请前往 https://platform.example/topup 充值，到账后
  > 自动恢复使用。

* 客户在平台充值 → 钱包 `is_available=true` → 网关下一次探测（≤5 分钟）
  自动放行，**无需重启、无需人工解锁**。
* `GET /v1/quota`（带网关 key）返回 JSON 快照，可接到面板/CLI 展示剩余额度。
* 计量口径：只计真正转发到上游的 `TokensIn + TokensOut`（供应商准确 usage
  优先，缺失时 bytes/4 估算）；**L3 缓存命中不扣额度**——缓存替客户省的钱
  不算进免费额度，这也让"semantix 省 token"的卖点在账单上直接可见。
* 计数持久化在 `state_file`（默认 store 同目录 `quota-state.json`），进程
  重启不清零；文件损坏时启动失败而不是静默重置（防"删文件重白嫖"——同理，
  **网关与 state 文件必须在我们控制的机器上**）。

### 3.3 部署拓扑（关键）

| 组件 | 位置 | 分发形态 |
|---|---|---|
| `semantix` + `semantix-agent` | 客户机器 | **保护版**二进制（§2） |
| `semantix-gateway`（billing enabled） | 我方 / 每客户一实例 | 不分发，或仅发容器镜像 |
| DS 平台（New API，充值+付费计量） | 我方 | — |

当前网关是单 key 单租户：每个客户发一个 gateway key + 一个网关实例（或复用
New API 做多租户入口，网关作为渠道挂在其后）。免费额度按"安装/客户"计。

### 3.4 与 harness 的协同

* `semantix-agent` 状态栏本就支持 `balance_url` 钱包余额展示
  （`harness/billing`），客户随时能看到平台余额；
* 402 的 `error.code = "free_tier_exhausted"` 是机器可读字段，后续前端要做
  弹窗/自动打开浏览器充值页时按 code 分支即可，无需解析文案。

## 4. 验收记录

* `gateway` 全量测试通过（含 10 个 quota/billing 用例：持久化重启、402 报文、
  钱包解锁+探测缓存、L3 免计费、/v1/quota、配置校验、损坏状态失败启动、
  billing 关闭时零痕迹）。
* 保护版 linux-amd64 三个二进制构建通过，泄露扫描 0 命中，
  `go version -m` 无模块信息，版本注入与运行冒烟正常。
